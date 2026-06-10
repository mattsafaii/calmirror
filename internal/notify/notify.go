// Package notify posts macOS user notifications via osascript, so a persistent
// feed failure is visible even when CalMirror runs unattended on a schedule.
package notify

import (
	"os/exec"
	"strings"
)

// Osascript posts notifications through the macOS `osascript` tool. Its zero
// value is ready to use.
type Osascript struct{}

// Notify displays a Notification Center banner. It is best-effort: any error
// (e.g. osascript unavailable) is ignored, since a failed notification must
// never break a sync.
func (Osascript) Notify(title, body string) {
	script := "display notification \"" + escape(body) + "\" with title \"" + escape(title) + "\""
	_ = exec.Command("osascript", "-e", script).Run()
}

// escape makes a string safe to embed in an AppleScript double-quoted literal
// and trims it to a banner-friendly length.
func escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:197] + "..."
	}
	return s
}
