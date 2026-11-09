// Command heyday mirrors HEY calendars into Google Calendar as native events,
// so meeting tools that read only Google (Granola, Zoom) see HEY meetings.
package main

import (
	"os"

	"github.com/mattsafaii/heyday/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
