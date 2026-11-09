// Package launchd installs and removes Heyday's launchd login-items: the
// scheduled `heyday sync` job and the optional HEY watch agent, so both run
// after login with no manual plist editing.
package launchd

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Label is the launchd job label (and plist base name) of the scheduled sync.
	Label = "com.mattsafaii.heyday.sync"
	// WatchLabel is the launchd job label of the HEY watch agent.
	WatchLabel = "com.mattsafaii.heyday.watch"
)

// PlistPath returns the per-user LaunchAgents path for a Heyday job label.
func PlistPath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// Install writes the sync job's plist and (re)loads it. The job runs
// `heyday sync` at load (login) and every interval thereafter. It logs to
// logPath. binPath is the heyday executable to invoke.
func Install(binPath string, interval time.Duration, logPath string) error {
	return installPlist(Label,
		renderPlist(Label, []string{binPath, "sync"}, int(interval.Seconds()), false, logPath))
}

// InstallWatch writes the watch agent's plist and (re)loads it. The job runs
// `heyday watch` at load and launchd restarts it whenever it crashes (exits
// non-zero); a clean exit leaves it stopped. It logs
// to logPath. binPath is the heyday executable to invoke.
func InstallWatch(binPath string, logPath string) error {
	return installPlist(WatchLabel,
		renderPlist(WatchLabel, []string{binPath, "watch"}, 0, true, logPath))
}

// Uninstall unloads the sync job and removes its plist. Removing an absent job
// is not an error.
func Uninstall() error {
	return uninstallLabel(Label)
}

// UninstallWatch unloads the watch agent and removes its plist. Removing an
// absent job is not an error.
func UninstallWatch() error {
	return uninstallLabel(WatchLabel)
}

// IsInstalled reports whether the plist file exists.
func IsInstalled() (bool, error) {
	return labelInstalled(Label)
}

// IsWatchInstalled reports whether the watch agent's plist file exists.
func IsWatchInstalled() (bool, error) {
	return labelInstalled(WatchLabel)
}

func labelInstalled(label string) (bool, error) {
	plistPath, err := PlistPath(label)
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

// LegacyLabels are the launchd jobs CalMirror, Heyday's former name,
// installed. They ran the old binary, so Heyday removes them on install.
var LegacyLabels = []string{"com.calmirror.sync", "com.calmirror.watch"}

// RemoveLegacy unloads and deletes any CalMirror-era jobs, returning the
// labels whose plist it removed. A job that was never installed is skipped.
func RemoveLegacy() ([]string, error) {
	var removed []string
	for _, label := range LegacyLabels {
		installed, err := labelInstalled(label)
		if err != nil {
			return removed, err
		}
		if !installed {
			continue
		}
		if err := uninstallLabel(label); err != nil {
			return removed, fmt.Errorf("remove old job %s: %w", label, err)
		}
		removed = append(removed, label)
	}
	return removed, nil
}

// run executes a command (launchctl). It is a variable so tests can stand in
// for launchctl without touching the user's real launchd jobs.
var run = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}

func installPlist(label, content string) error {
	plistPath, err := PlistPath(label)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return err
	}

	target := domainTarget()
	// Replace any prior instance, then load and start once now.
	_ = run("launchctl", "bootout", target+"/"+label) // ignore "not loaded"
	if err := run("launchctl", "bootstrap", target, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if err := run("launchctl", "enable", target+"/"+label); err != nil {
		return fmt.Errorf("launchctl enable: %w", err)
	}
	return nil
}

func uninstallLabel(label string) error {
	plistPath, err := PlistPath(label)
	if err != nil {
		return err
	}
	_ = run("launchctl", "bootout", domainTarget()+"/"+label)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// keepAliveThrottleSecs is the minimum gap between relaunches of a KeepAlive job.
const keepAliveThrottleSecs = 300

// renderPlist renders a launchd property list running one heyday subcommand.
// startIntervalSecs adds a StartInterval key when > 0; keepAlive asks launchd
// to relaunch the job whenever it exits unsuccessfully.
func renderPlist(label string, args []string, startIntervalSecs int, keepAlive bool, logPath string) string {
	var intervalKey, keepAliveKey string
	if startIntervalSecs > 0 {
		if startIntervalSecs < 1 {
			startIntervalSecs = 1
		}
		intervalKey = fmt.Sprintf("\t<key>StartInterval</key>\n\t<integer>%d</integer>\n", startIntervalSecs)
	}
	if keepAlive {
		// Relaunch only after a crash (a non-zero exit). A job that exits 0 on
		// purpose — the watch agent stopping on a HEY sign-in failure — stays
		// stopped until the next login or reinstall. ThrottleInterval spaces
		// out the relaunches that do happen (launchd's default is 10s).
		keepAliveKey = fmt.Sprintf("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n"+
			"\t<key>ThrottleInterval</key>\n\t<integer>%d</integer>\n", keepAliveThrottleSecs)
	}

	var progArgs strings.Builder
	for _, a := range args {
		progArgs.WriteString(fmt.Sprintf("\t\t<string>%s</string>\n", html.EscapeString(a)))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>RunAtLoad</key>
	<true/>
%s%s	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`, html.EscapeString(label), progArgs.String(), intervalKey, keepAliveKey,
		html.EscapeString(logPath), html.EscapeString(logPath))
}
