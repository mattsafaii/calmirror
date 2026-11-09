package launchd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncPlist(t *testing.T) {
	p := renderPlist(Label, []string{"/usr/local/bin/heyday", "sync"}, int((15 * time.Minute).Seconds()), false, "/tmp/heyday/sync.log")

	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/usr/local/bin/heyday</string>",
		"<string>sync</string>",
		"<key>StartInterval</key>\n\t<integer>900</integer>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/tmp/heyday/sync.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("sync plist missing %q\n---\n%s", want, p)
		}
	}
	if strings.Contains(p, "KeepAlive") {
		t.Errorf("sync plist should not KeepAlive:\n%s", p)
	}
}

func TestSyncPlistZeroIntervalOmitted(t *testing.T) {
	p := renderPlist(Label, []string{"/bin/x", "sync"}, 0, false, "/tmp/x.log")
	if strings.Contains(p, "StartInterval") {
		t.Errorf("zero interval should omit StartInterval entirely:\n%s", p)
	}
}

func TestWatchPlist(t *testing.T) {
	p := renderPlist(WatchLabel, []string{"/usr/local/bin/heyday", "watch"}, 0, true, "/tmp/heyday/watch.log")

	for _, want := range []string{
		"<string>" + WatchLabel + "</string>",
		"<string>/usr/local/bin/heyday</string>",
		"<string>watch</string>",
		"<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>",
		"<key>ThrottleInterval</key>\n\t<integer>300</integer>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/tmp/heyday/watch.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("watch plist missing %q\n---\n%s", want, p)
		}
	}
	if strings.Contains(p, "StartInterval") {
		t.Errorf("watch plist should not StartInterval (KeepAlive only):\n%s", p)
	}
}

// RemoveLegacy deletes only CalMirror-era plists, and unloads them through
// launchctl (stubbed here so the test never touches real launchd jobs).
func TestRemoveLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var calls []string
	orig := run
	run = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { run = orig })

	agents := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"com.calmirror.sync.plist", Label + ".plist"} {
		if err := os.WriteFile(filepath.Join(agents, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := RemoveLegacy()
	if err != nil {
		t.Fatalf("RemoveLegacy: %v", err)
	}
	if len(removed) != 1 || removed[0] != "com.calmirror.sync" {
		t.Fatalf("removed = %v, want only com.calmirror.sync", removed)
	}
	if _, err := os.Stat(filepath.Join(agents, "com.calmirror.sync.plist")); !os.IsNotExist(err) {
		t.Errorf("legacy plist still there (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(agents, Label+".plist")); err != nil {
		t.Errorf("Heyday's own plist was touched: %v", err)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "bootout") || !strings.Contains(calls[0], "com.calmirror.sync") {
		t.Errorf("launchctl calls = %v, want one bootout of com.calmirror.sync", calls)
	}
}
