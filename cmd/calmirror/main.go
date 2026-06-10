// Command calmirror mirrors ICS calendar feeds into iCloud via CalDAV as
// full-fidelity native events. See CLAUDE.md for the Phase 1 scope.
package main

import (
	"os"

	"github.com/mattsafaii/calmirror/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
