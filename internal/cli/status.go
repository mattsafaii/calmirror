package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/mattsafaii/calmirror/internal/config"
)

func cmdStatus(args []string) int {
	cfg, err := config.Load()
	if errors.Is(err, fs.ErrNotExist) {
		return fail("not set up yet; run `calmirror setup` first")
	}
	if err != nil {
		return fail("load config: %v", err)
	}

	st, err := openStore()
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	if len(cfg.Feeds) == 0 {
		fmt.Println("No feeds configured.")
		return 0
	}

	fmt.Printf("iCloud account: %s\n\n", cfg.ICloud.Username)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "FEED\tEVENTS\tLAST SYNC\tLAST ERROR")
	for _, f := range cfg.Feeds {
		count, _ := st.CountLinks(f.Name)
		state, ok, _ := st.GetFeed(f.Name)

		lastSync := "never"
		lastErr := "-"
		if ok {
			if !state.LastSyncAt.IsZero() {
				lastSync = humanizeAge(state.LastSyncAt)
			}
			if state.LastError != "" {
				lastErr = state.LastError
			}
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", f.Name, count, lastSync, lastErr)
	}
	if err := tw.Flush(); err != nil {
		return fail("%v", err)
	}
	return 0
}

// humanizeAge renders a timestamp as a short relative age (e.g. "5m ago").
func humanizeAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
