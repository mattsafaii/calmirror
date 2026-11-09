package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// exitWith runs a real process that prints out and exits with code, so the
// watcher sees a genuine *exec.ExitError.
func exitWith(out string, code int) func(ctx context.Context, w io.Writer) error {
	return func(ctx context.Context, w io.Writer) error {
		cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf(`printf '%%s' "$0"; exit %d`, code), out)
		cmd.Stdout = w
		return cmd.Run()
	}
}

type countingNotifier struct{ titles []string }

func (n *countingNotifier) Notify(title, body string) { n.titles = append(n.titles, title) }

func TestWatchStopsOnSignInFailure(t *testing.T) {
	cases := map[string]func(ctx context.Context, w io.Writer) error{
		"signed out (exit 3)": exitWith(`{"ok":false,"error":"authentication required","code":"auth"}`, 3),
		"token refresh refused (exit 7)": exitWith(
			`{"ok":false,"error":"token refresh failed: token refresh failed (status 429): rate_limit_exceeded","code":"api"}`, 7),
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			runs := 0
			n := &countingNotifier{}
			w := &watcher{
				run:      func(ctx context.Context, out io.Writer) error { runs++; return run(ctx, out) },
				notifier: n,
				log:      io.Discard,
				sleep: func(ctx context.Context, d time.Duration) bool {
					t.Fatalf("slept %s; a sign-in failure must stop, not retry", d)
					return false
				},
			}
			if code := w.loop(context.Background()); code != 0 {
				t.Fatalf("exit code = %d, want 0 so launchd leaves the agent stopped", code)
			}
			if runs != 1 {
				t.Fatalf("hey watch ran %d times, want 1", runs)
			}
			if len(n.titles) != 1 {
				t.Fatalf("notifications = %d, want 1", len(n.titles))
			}
		})
	}
}

func TestWatchBacksOffOnOtherFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var slept []time.Duration
	n := &countingNotifier{}
	w := &watcher{
		run:      exitWith(`{"ok":false,"error":"network error: HEY's cable server hung up","code":"network"}`, 6),
		notifier: n,
		log:      io.Discard,
		sleep: func(ctx context.Context, d time.Duration) bool {
			slept = append(slept, d)
			if len(slept) == 7 {
				cancel()
				return false
			}
			return true
		},
	}
	if code := w.loop(ctx); code != 0 {
		t.Fatalf("exit code = %d, want 0 on cancellation", code)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	if fmt.Sprint(slept) != fmt.Sprint(want) {
		t.Fatalf("backoff = %v, want %v", slept, want)
	}
	if len(n.titles) != 0 {
		t.Fatalf("a network failure should not notify, got %v", n.titles)
	}
}

func TestWatchHealthyRunResetsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	runs := 0
	var slept []time.Duration
	w := &watcher{
		run: func(ctx context.Context, out io.Writer) error {
			runs++
			if runs == 3 {
				clock = clock.Add(watchHealthyRun) // this run stayed up a while
			}
			return exitWith("server error", 7)(ctx, out)
		},
		log: io.Discard,
		now: func() time.Time { return clock },
		sleep: func(ctx context.Context, d time.Duration) bool {
			slept = append(slept, d)
			if len(slept) == 4 {
				cancel()
				return false
			}
			return true
		},
	}
	w.loop(ctx)
	want := []time.Duration{time.Minute, 2 * time.Minute, time.Minute, 2 * time.Minute}
	if fmt.Sprint(slept) != fmt.Sprint(want) {
		t.Fatalf("backoff = %v, want %v", slept, want)
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	tb := &tailBuffer{max: 8}
	io.WriteString(tb, "0123456789")
	io.WriteString(tb, "ab")
	if got := tb.String(); got != "456789ab" {
		t.Fatalf("tail = %q", got)
	}
	if strings.Contains(tb.String(), "0123") {
		t.Fatal("kept bytes past max")
	}
}
