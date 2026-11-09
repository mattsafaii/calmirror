package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/mattsafaii/calmirror/internal/hey"
	"github.com/mattsafaii/calmirror/internal/notify"
)

// heyWatchEvents are the hey watch change kinds that warrant a mirror pass:
// calendar recordings coming and going, plus the catch-up signal. Mail changes
// are deliberately excluded so email traffic never triggers syncs.
const heyWatchEvents = "recording_added,recording_updated,recording_deleted,calendar_resync"

// Backoff bounds for restarting a failed `hey watch`. HEY rate-limits sign-in,
// so a watch that fails (network, server) must slow down rather than retry at
// once: an immediate restart loop is what locks the account out.
const (
	watchBackoffMin = time.Minute
	watchBackoffMax = 30 * time.Minute
	// A run that stayed up this long was healthy; its failure starts the
	// backoff over.
	watchHealthyRun = 10 * time.Minute
	// How much of hey's recent output is kept to tell why it exited.
	watchTailBytes = 16 << 10
)

// signInStopMessage is what the user sees when watch stops on a sign-in
// failure. watch exits cleanly so launchd does not relaunch it (the agent only
// restarts on a crash); it tries again at the next login, or after `calmirror
// install`.
const signInStopMessage = "HEY sign-in failed, so CalMirror stopped watching HEY. Run `hey auth login`, then `calmirror install` to start watching again."

func cmdWatch(args []string) int {
	fset := flag.NewFlagSet("watch", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: calmirror watch\n\n")
		fmt.Fprintf(fset.Output(), "Follow HEY calendar changes via the hey CLI and run `calmirror sync`\n")
		fmt.Fprintf(fset.Output(), "whenever a recording is added, updated, or deleted — near-instant mirrors\n")
		fmt.Fprintf(fset.Output(), "on top of the scheduled `install` job, which stays in place as the fallback.\n\n")
		fmt.Fprintf(fset.Output(), "Network and server failures are retried with backoff (%s to %s).\n", watchBackoffMin, watchBackoffMax)
		fmt.Fprintf(fset.Output(), "A HEY sign-in failure stops the watch and posts a notification instead:\n")
		fmt.Fprintf(fset.Output(), "retrying a refused sign-in only extends HEY's rate limit.\n")
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}

	heyBin, err := hey.Find()
	if err != nil {
		// Permanent until hey is installed: exit cleanly so launchd does not
		// relaunch the agent over and over.
		fmt.Fprintf(os.Stderr, "calmirror: %v; not watching.\n", err)
		return 0
	}
	bin, err := os.Executable()
	if err != nil {
		return fail("locate executable: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := &watcher{
		run:      func(ctx context.Context, out io.Writer) error { return runHeyWatch(ctx, heyBin, bin, out) },
		notifier: notify.Osascript{},
		log:      os.Stderr,
		sleep:    sleepCtx,
	}
	fmt.Printf("Watching HEY calendars (%s); syncing as events change. Ctrl-C to stop.\n", heyWatchEvents)
	return w.loop(ctx)
}

// watcher runs `hey watch` until a sign-in failure or cancellation, retrying
// other failures with backoff. Its pieces are fields so the loop can be tested
// without running hey or sleeping.
type watcher struct {
	run      func(ctx context.Context, out io.Writer) error
	notifier interface{ Notify(title, body string) }
	log      io.Writer
	sleep    func(ctx context.Context, d time.Duration) bool // false if ctx ended first
	now      func() time.Time
}

// loop returns the process exit code. It returns 0 on cancellation and on a
// sign-in failure, so launchd (KeepAlive on unsuccessful exit only) leaves the
// agent stopped rather than relaunching it into HEY's rate limit.
func (w *watcher) loop(ctx context.Context) int {
	now := w.now
	if now == nil {
		now = time.Now
	}
	backoff := watchBackoffMin
	for {
		tail := &tailBuffer{max: watchTailBytes}
		started := now()
		err := w.run(ctx, tail)
		if ctx.Err() != nil {
			return 0
		}

		exitCode := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if hey.SignInFailure(exitCode, tail.String()) {
			fmt.Fprintf(w.log, "calmirror: hey watch exited (status %d): %s\n", exitCode, signInStopMessage)
			if w.notifier != nil {
				w.notifier.Notify("CalMirror: HEY sign-in needed", signInStopMessage)
			}
			return 0
		}

		if now().Sub(started) >= watchHealthyRun {
			backoff = watchBackoffMin
		}
		reason := "exited"
		if err != nil {
			reason = err.Error()
		}
		fmt.Fprintf(w.log, "calmirror: hey watch %s; retrying in %s\n", reason, backoff)
		if !w.sleep(ctx, backoff) {
			return 0
		}
		backoff = min(backoff*2, watchBackoffMax)
	}
}

// runHeyWatch runs one `hey watch` until it exits or ctx is cancelled, copying
// its output to the terminal/log and to tail. hey runs `calmirror sync` itself
// on each calendar change.
func runHeyWatch(ctx context.Context, heyBin, bin string, tail io.Writer) error {
	cmd := exec.CommandContext(ctx, heyBin, "watch",
		"--events", heyWatchEvents,
		"--run-sync", fmt.Sprintf("%q sync", bin))
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, tail)
	cmd.Stderr = io.MultiWriter(os.Stderr, tail)
	cmd.Cancel = func() error { // forward SIGINT/SIGTERM to hey instead of SIGKILL
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	return cmd.Run()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// tailBuffer keeps the last max bytes written to it. It is safe for the
// concurrent writes exec makes when stdout and stderr share it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
