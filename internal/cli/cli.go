// Package cli routes the heyday command-line surface to its subcommands.
package cli

import (
	"fmt"
	"os"
)

const usage = `heyday mirrors your HEY calendar into Google Calendar as native events,
with the meeting link, notes and location intact, so tools that read only
Google or Outlook calendars (Granola, Zoom) see your HEY meetings.

Usage:
  heyday <command> [arguments]

Commands:
  setup       Connect Google and pick the HEY calendar to mirror (an ICS feed is optional)
  sync        Run one sync pass across all configured feeds
  status      Show each feed's source, Google calendar, last sync, event count and last error
  watch       Sync whenever HEY calendars change (requires the hey CLI)
  install     Register launchd login-items: scheduled syncs, plus the HEY watch agent
  uninstall   Remove the launchd login-items
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
	case "watch":
		return cmdWatch(rest)
	case "install":
		return cmdInstall(rest)
	case "uninstall":
		return cmdUninstall(rest)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "heyday: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// fail prints a message to stderr and returns exit code 1.
func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "heyday: "+format+"\n", args...)
	return 1
}
