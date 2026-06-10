// Package cli routes the calmirror command-line surface to its subcommands.
package cli

import (
	"fmt"
	"os"
)

const usage = `calmirror mirrors ICS calendar feeds into iCloud via CalDAV.

Usage:
  calmirror <command> [arguments]

Commands:
  setup       Add a feed and store the iCloud app-specific password
  sync        Run one sync pass across all configured feeds
  status      Show per-feed last-sync time, event counts, and last error
  install     Register the launchd login-item that runs sync on a schedule
  uninstall   Remove the launchd login-item
`

// Run dispatches args[0] to the matching subcommand and returns a process exit
// code. args is os.Args without the program name.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "setup":
		return cmdSetup(rest)
	case "sync":
		return cmdSync(rest)
	case "status":
		return cmdStatus(rest)
	case "install":
		return cmdInstall(rest)
	case "uninstall":
		return cmdUninstall(rest)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "calmirror: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// fail prints a message to stderr and returns exit code 1.
func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "calmirror: "+format+"\n", args...)
	return 1
}
