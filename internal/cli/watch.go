package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mattsafaii/calmirror/internal/hey"
)

// heyWatchEvents are the hey watch change kinds that warrant a mirror pass:
// calendar recordings coming and going, plus the catch-up signal. Mail changes
// are deliberately excluded so email traffic never triggers syncs.
const heyWatchEvents = "recording_added,recording_updated,recording_deleted,calendar_resync"

// Backoff bounds for restarting a failed `hey watch`. HEY rate-limits sign-in,
// so a watch that fails (expired login, rate limit, network) must slow down
// rather than retry at once: an immediate restart loop is what locks the
// account out.
const (
	watchBackoffMin = time.Minute
	watchBackoffMax = 30 * time.Minute
	// A run that stayed up this long was healthy; its failure starts the
	// backoff over.
	watchHealthyRun = 10 * time.Minute
)

func cmdWatch(args []string) int {
	fset := flag.NewFlagSet("watch", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: calmirror watch\n\n")
		fmt.Fprintf(fset.Output(), "Follow HEY calendar changes via the hey CLI and run `calmirror sync`\n")
		fmt.Fprintf(fset.Output(), "whenever a recording is added, updated, or deleted — near-instant mirrors\n")
		fmt.Fprintf(fset.Output(), "on top of the scheduled `install` job, which stays in place as the fallback.\n\n")
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}

	heyBin, err := hey.Find()
	if err != nil {
		return fail("%v", err)
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

	fmt.Printf("Watching HEY calendars (%s); syncing as events change. Ctrl-C to stop.\n", heyWatchEvents)
	backoff := watchBackoffMin
	for {
		started := time.Now()
		err := runHeyWatch(ctx, heyBin, bin)
		if ctx.Err() != nil {
			return 0
		}
		if time.Since(started) >= watchHealthyRun {
			backoff = watchBackoffMin
		}
		reason := "exited"
		if err != nil {
			reason = err.Error()
		}
		hint := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == hey.ExitAuth {
			hint = " (signed out of HEY? run `hey auth login`)"
		}
		fmt.Fprintf(os.Stderr, "calmirror: hey watch %s%s; retrying in %s\n", reason, hint, backoff)
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, watchBackoffMax)
	}
}

// runHeyWatch runs one `hey watch` until it exits or ctx is cancelled. hey
// runs `calmirror sync` itself on each calendar change.
func runHeyWatch(ctx context.Context, heyBin, bin string) error {
	cmd := exec.CommandContext(ctx, heyBin, "watch",
		"--events", heyWatchEvents,
		"--run-sync", fmt.Sprintf("%q sync", bin))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { // forward SIGINT/SIGTERM to hey instead of SIGKILL
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	return cmd.Run()
}
