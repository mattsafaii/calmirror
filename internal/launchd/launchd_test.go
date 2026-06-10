package launchd

import (
	"strings"
	"testing"
	"time"
)

func TestPlistContent(t *testing.T) {
	p := plistContent("/usr/local/bin/calmirror", 15*time.Minute, "/tmp/calmirror/sync.log")

	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/usr/local/bin/calmirror</string>",
		"<string>sync</string>",
		"<key>StartInterval</key>\n\t<integer>900</integer>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/tmp/calmirror/sync.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q\n---\n%s", want, p)
		}
	}
}

func TestPlistContentMinInterval(t *testing.T) {
	p := plistContent("/bin/x", 0, "/tmp/x.log")
	if !strings.Contains(p, "<integer>1</integer>") {
		t.Errorf("zero interval not clamped to 1s:\n%s", p)
	}
}
