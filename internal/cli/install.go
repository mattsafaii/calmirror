package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/launchd"
)

func cmdInstall(args []string) int {
	fset := flag.NewFlagSet("install", flag.ContinueOnError)
	interval := fset.Int("interval", 15, "minutes between scheduled syncs")
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: calmirror install [--interval <minutes>]\n\n")
		fmt.Fprintf(fset.Output(), "Registers a launchd login-item that runs `calmirror sync` at login and on a schedule.\n\n")
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

	if err := launchd.Install(bin, time.Duration(*interval)*time.Minute, logPath); err != nil {
		return fail("%v", err)
	}

	plistPath, _ := launchd.PlistPath()
	fmt.Printf("Installed launchd job %s (every %dm).\n", launchd.Label, *interval)
	fmt.Printf("  plist: %s\n  log:   %s\n", plistPath, logPath)
	return 0
}

func cmdUninstall(args []string) int {
	if err := launchd.Uninstall(); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("Removed launchd job %s.\n", launchd.Label)
	return 0
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
