package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/mattsafaii/calmirror/internal/caldav"
	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/keychain"
)

// passwordEnv lets the app-specific password be supplied non-interactively
// (e.g. piped in CI) instead of via the stdin prompt.
const passwordEnv = "CALMIRROR_ICLOUD_PASSWORD"

func cmdSetup(args []string) int {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	var (
		user       = fset.String("icloud-user", "", "iCloud Apple ID / email")
		name       = fset.String("feed", "", "short feed name (stable identifier)")
		url        = fset.String("url", "", "source ICS feed URL")
		calendar   = fset.String("calendar", "", "destination iCloud calendar name (created if absent)")
		pastDays   = fset.Int("past-days", config.DefaultSyncWindowPastDays, "mirror events from this many days in the past")
		futureDays = fset.Int("future-days", 0, "mirror events up to this many days ahead (0 = unbounded)")
		skipVerify = fset.Bool("skip-verify", false, "skip the iCloud connection check")
	)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: calmirror setup --icloud-user <email> --feed <name> --url <ics-url> --calendar <name>\n\n")
		fmt.Fprintf(fset.Output(), "Adds or updates a feed and stores the iCloud app-specific password in the Keychain.\n")
		fmt.Fprintf(fset.Output(), "The password is read from $%s if set, otherwise prompted on stdin.\n\n", passwordEnv)
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}

	missing := []string{}
	for flagName, val := range map[string]string{
		"icloud-user": *user, "feed": *name, "url": *url, "calendar": *calendar,
	} {
		if strings.TrimSpace(val) == "" {
			missing = append(missing, "--"+flagName)
		}
	}
	if len(missing) > 0 {
		fset.Usage()
		return fail("missing required flags: %s", strings.Join(missing, ", "))
	}

	password, err := readPassword()
	if err != nil {
		return fail("%v", err)
	}
	if password == "" {
		return fail("app-specific password is required")
	}

	// Load existing config or start fresh.
	cfg, err := config.Load()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fail("load config: %v", err)
		}
		cfg = &config.Config{}
	}
	cfg.ICloud.Username = *user

	// Add or replace the feed by name.
	feedCfg := config.Feed{
		Name:                *name,
		SourceURL:           *url,
		DestinationCalendar: *calendar,
		SyncWindow:          config.SyncWindow{PastDays: *pastDays, FutureDays: *futureDays},
	}
	replaced := false
	for i := range cfg.Feeds {
		if cfg.Feeds[i].Name == *name {
			cfg.Feeds[i] = feedCfg
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Feeds = append(cfg.Feeds, feedCfg)
	}

	if err := keychain.Set(*user, password); err != nil {
		return fail("%v", err)
	}
	if err := cfg.Save(); err != nil {
		return fail("save config: %v", err)
	}

	path, _ := config.Path()
	fmt.Printf("Saved feed %q -> calendar %q (config: %s)\n", *name, *calendar, path)

	if *skipVerify {
		return 0
	}
	if err := verifyConnection(*user, password); err != nil {
		fmt.Fprintf(os.Stderr, "warning: iCloud connection check failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "config was saved; fix credentials and re-run setup, or run `calmirror sync`.\n")
		return 1
	}
	fmt.Println("iCloud connection OK.")
	return 0
}

// readPassword returns the app-specific password from the environment, or
// prompts for it on stdin.
func readPassword() (string, error) {
	if v := os.Getenv(passwordEnv); v != "" {
		return v, nil
	}
	fmt.Print("iCloud app-specific password: ")
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// verifyConnection confirms the credentials reach iCloud and discovery works.
func verifyConnection(user, password string) error {
	client, err := caldav.New(user, password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = client.Discover(ctx)
	return err
}
