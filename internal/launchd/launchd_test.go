package launchd

import (
	"strings"
	"testing"
	"time"
)

func TestSyncPlist(t *testing.T) {
	p := renderPlist(Label, []string{"/usr/local/bin/calmirror", "sync"}, int((15 * time.Minute).Seconds()), false, "/tmp/calmirror/sync.log")

	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/usr/local/bin/calmirror</string>",
		"<string>sync</string>",
		"<key>StartInterval</key>\n\t<integer>900</integer>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/tmp/calmirror/sync.log</string>",
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
	p := renderPlist(WatchLabel, []string{"/usr/local/bin/calmirror", "watch"}, 0, true, "/tmp/calmirror/watch.log")

	for _, want := range []string{
		"<string>" + WatchLabel + "</string>",
		"<string>/usr/local/bin/calmirror</string>",
		"<string>watch</string>",
		"<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>",
		"<key>ThrottleInterval</key>\n\t<integer>300</integer>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/tmp/calmirror/watch.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("watch plist missing %q\n---\n%s", want, p)
		}
	}
	if strings.Contains(p, "StartInterval") {
		t.Errorf("watch plist should not StartInterval (KeepAlive only):\n%s", p)
	}
}
