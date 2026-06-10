// Package launchd installs and removes CalMirror's own launchd login-item, so
// `calmirror sync` runs on a schedule after login with no manual plist editing.
package launchd

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Label is the launchd job label and the plist file's base name.
const Label = "com.calmirror.sync"

// PlistPath returns the per-user LaunchAgents path for the CalMirror job.
func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

// Install writes the launchd plist and (re)loads it. The job runs `calmirror
// sync` at load (login) and every interval thereafter. It logs stdout/stderr to
// logPath. binPath is the calmirror executable to invoke.
func Install(binPath string, interval time.Duration, logPath string) error {
	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	content := plistContent(binPath, interval, logPath)
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return err
	}

	target := domainTarget()
	// Replace any prior instance, then load and start once now.
	_ = run("launchctl", "bootout", target+"/"+Label) // ignore "not loaded"
	if err := run("launchctl", "bootstrap", target, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if err := run("launchctl", "enable", target+"/"+Label); err != nil {
		return fmt.Errorf("launchctl enable: %w", err)
	}
	return nil
}

// Uninstall unloads the job and removes its plist. Removing an absent job is
// not an error.
func Uninstall() error {
	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	_ = run("launchctl", "bootout", domainTarget()+"/"+Label)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsInstalled reports whether the plist file exists.
func IsInstalled() (bool, error) {
	plistPath, err := PlistPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(plistPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// domainTarget is the launchd GUI domain for the current user.
func domainTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}

// plistContent renders the launchd property list. interval is rounded to whole
// seconds (minimum 1).
func plistContent(binPath string, interval time.Duration, logPath string) string {
	secs := int(interval.Seconds())
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>sync</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>%d</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`, html.EscapeString(Label), html.EscapeString(binPath), secs,
		html.EscapeString(logPath), html.EscapeString(logPath))
}
