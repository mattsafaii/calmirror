package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mattsafaii/heyday/internal/config"
	"github.com/mattsafaii/heyday/internal/hey"
	"github.com/mattsafaii/heyday/internal/launchd"
)

func cmdInstall(args []string) int {
	fset := flag.NewFlagSet("install", flag.ContinueOnError)
	interval := fset.Int("interval", 15, "minutes between scheduled syncs")
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: heyday install [--interval <minutes>]\n\n")
		fmt.Fprintf(fset.Output(), "Registers the launchd login-items that run `heyday sync` on a schedule,\n")
		fmt.Fprintf(fset.Output(), "plus — when the hey CLI is installed — a watch agent that syncs near-instantly\n")
		fmt.Fprintf(fset.Output(), "as HEY calendars change.\n\n")
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *interval < 1 {
		return fail("--interval must be at least 1 minute")
	}

	bin, err := os.Executable()
	if err != nil {
		return fail("locate executable: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}

	logPath, err := logFilePath()
	if err != nil {
		return fail("%v", err)
	}

	removeLegacyJobs()
	if err := launchd.Install(bin, time.Duration(*interval)*time.Minute, logPath); err != nil {
		return fail("%v", err)
	}
	plistPath, _ := launchd.PlistPath(launchd.Label)
	fmt.Printf("Installed launchd job %s (every %dm).\n", launchd.Label, *interval)
	fmt.Printf("  plist: %s\n  log:   %s\n", plistPath, logPath)

	watchLog := filepath.Join(filepath.Dir(logPath), "watch.log")
	if _, err := hey.Find(); err != nil {
		fmt.Println("hey CLI not found; skipping the watch agent (scheduled syncs still run).")
		fmt.Println("Install it from github.com/basecamp/hey-cli, then re-run `heyday install`.")
		return 0
	}
	if err := launchd.InstallWatch(bin, watchLog); err != nil {
		return fail("install watch agent: %v", err)
	}
	watchPlist, _ := launchd.PlistPath(launchd.WatchLabel)
	fmt.Printf("Installed launchd job %s (HEY calendar changes trigger sync).\n", launchd.WatchLabel)
	fmt.Printf("  plist: %s\n  log:   %s\n", watchPlist, watchLog)
	return 0
}

func cmdUninstall(args []string) int {
	removeLegacyJobs()
	if err := launchd.Uninstall(); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("Removed launchd job %s.\n", launchd.Label)
	if err := launchd.UninstallWatch(); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("Removed launchd job %s.\n", launchd.WatchLabel)
	return 0
}

// removeLegacyJobs removes CalMirror's old launchd jobs, so the old binary
// stops running alongside Heyday. A failure is only a warning: it should not
// block installing or removing Heyday's own jobs.
func removeLegacyJobs() {
	removed, err := launchd.RemoveLegacy()
	for _, label := range removed {
		fmt.Printf("Removed old CalMirror launchd job %s.\n", label)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
}

// logFilePath returns the path the scheduled sync logs to, alongside the config.
func logFilePath() (string, error) {
	dbPath, err := config.StateDBPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync.log"), nil
}
