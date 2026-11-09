package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattsafaii/heyday/internal/config"
	"github.com/mattsafaii/heyday/internal/google"
	"github.com/mattsafaii/heyday/internal/hey"
)

// googleSecretEnv lets the Google OAuth client secret be supplied
// non-interactively instead of via the stdin prompt.
const googleSecretEnv = "HEYDAY_GOOGLE_CLIENT_SECRET"

// Defaults for the common case: one HEY feed into one Heyday-owned calendar.
const (
	defaultFeedName = "hey"
	defaultCalendar = "HEY (Heyday)"
)

func cmdSetup(args []string) int {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	var (
		heyCalendar    = fset.String("hey-calendar", "", "HEY calendar id from `hey calendar list`, or \"all\"")
		googleClientID = fset.String("google-client-id", "", "Google OAuth desktop client id (needed to connect Google the first time)")
		name           = fset.String("feed", defaultFeedName, "short feed name (stable identifier)")
		calendar       = fset.String("calendar", defaultCalendar, "Google calendar to mirror into (created and owned by Heyday)")
		pastDays       = fset.Int("past-days", config.DefaultSyncWindowPastDays, "mirror events from this many days in the past")
		futureDays     = fset.Int("future-days", 0, fmt.Sprintf("mirror events up to this many days ahead (0 = %d for HEY, unbounded for ICS)", config.DefaultHEYFutureDays))
		source         = fset.String("source", config.SourceHEY, "where events come from: hey (default) or ics")
		url            = fset.String("url", "", "ICS feed URL (only with --source ics)")
	)
	fset.Usage = func() {
		out := fset.Output()
		fmt.Fprintf(out, "Usage:\n")
		fmt.Fprintf(out, "  heyday setup --google-client-id <id> --hey-calendar <id|all>\n")
		fmt.Fprintf(out, "  heyday setup --source ics --url <ics-url> --feed <name> --calendar <name>   (optional)\n\n")
		fmt.Fprintf(out, "Adds or updates a feed mirrored into a Heyday-owned Google calendar.\n\n")
		fmt.Fprintf(out, "HEY:    read through the hey CLI, which must be installed and signed in\n")
		fmt.Fprintf(out, "        (`hey auth login`). Run without --hey-calendar to list your HEY\n")
		fmt.Fprintf(out, "        calendars. Repeating events are mirrored as individual occurrences,\n")
		fmt.Fprintf(out, "        %d days ahead by default.\n", config.DefaultHEYFutureDays)
		fmt.Fprintf(out, "Google: the first run needs --google-client-id; the client secret is read from\n")
		fmt.Fprintf(out, "        $%s if set, else prompted, and a browser consent stores the\n", googleSecretEnv)
		fmt.Fprintf(out, "        refresh token in the Keychain. Later runs reuse the connected account.\n")
		fmt.Fprintf(out, "ICS:    optional, for non-HEY feeds such as Basecamp's schedule.\n\n")
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}

	*source = strings.ToLower(strings.TrimSpace(*source))
	if *source != config.SourceHEY && *source != config.SourceICS {
		fset.Usage()
		return fail("--source must be %q or %q", config.SourceHEY, config.SourceICS)
	}

	required := map[string]string{"feed": *name, "calendar": *calendar}
	if *source == config.SourceICS {
		required["url"] = *url
		if strings.TrimSpace(*heyCalendar) != "" {
			return fail("--hey-calendar does not apply to --source ics")
		}
	} else if strings.TrimSpace(*url) != "" {
		return fail("--url only applies to --source ics; HEY calendars are read through the hey CLI")
	}
	missing := []string{}
	for flagName, val := range required {
		if strings.TrimSpace(val) == "" {
			missing = append(missing, "--"+flagName)
		}
	}
	if len(missing) > 0 {
		fset.Usage()
		return fail("missing required flags: %s", strings.Join(missing, ", "))
	}

	var heyCalendarID int64
	if *source == config.SourceHEY {
		id, rc := resolveHEYCalendar(*heyCalendar)
		if rc != 0 {
			return rc
		}
		heyCalendarID = id
	}

	// Load existing config or start fresh.
	cfg, err := config.Load()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fail("load config: %v", err)
		}
		cfg = &config.Config{}
	}

	if rc := setupGoogle(cfg, *googleClientID); rc != 0 {
		return rc
	}

	// Add or replace the feed by name.
	upsertFeed(cfg, config.Feed{
		Name:                *name,
		Source:              *source,
		SourceURL:           *url,
		HEYCalendarID:       heyCalendarID,
		DestinationCalendar: *calendar,
		SyncWindow:          config.SyncWindow{PastDays: *pastDays, FutureDays: *futureDays},
	})

	if err := cfg.Save(); err != nil {
		return fail("save config: %v", err)
	}
	path, _ := config.Path()
	fmt.Printf("Saved feed %q (%s) -> Google calendar %q (config: %s)\n", *name, *source, *calendar, path)
	fmt.Println("Run `heyday sync` to mirror it now, then `heyday install` to keep it in sync.")
	return 0
}

// setupGoogle handles the Google OAuth path. If clientID is given (or no account
// is connected yet) it runs the browser consent flow, stores the refresh token
// and client secret in the Keychain, and records the account + client id in
// cfg.Google. If clientID is empty and an account is already connected, it
// reuses that connection so more feeds can be added without re-consent.
func setupGoogle(cfg *config.Config, clientID string) int {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		if cfg.Google.Account != "" && cfg.Google.ClientID != "" {
			fmt.Printf("Reusing connected Google account %q.\n", cfg.Google.Account)
			return 0
		}
		return fail("--google-client-id is required to connect a Google account")
	}

	secret, err := readSecret(googleSecretEnv, "Google OAuth client secret: ")
	if err != nil {
		return fail("%v", err)
	}
	if secret == "" {
		return fail("Google client secret is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	account, refreshToken, err := google.Connect(ctx, clientID, secret)
	if err != nil {
		return fail("%v", err)
	}
	if err := google.StoreCredentials(account, secret, refreshToken); err != nil {
		return fail("%v", err)
	}
	cfg.Google.Account = account
	cfg.Google.ClientID = clientID
	fmt.Printf("Connected Google account %q.\n", account)
	return 0
}

// upsertFeed adds feedCfg, replacing any existing feed of the same name.
func upsertFeed(cfg *config.Config, feedCfg config.Feed) {
	for i := range cfg.Feeds {
		if cfg.Feeds[i].Name == feedCfg.Name {
			cfg.Feeds[i] = feedCfg
			return
		}
	}
	cfg.Feeds = append(cfg.Feeds, feedCfg)
}

// readSecret returns a secret from the named environment variable, or prompts
// for it on stdin.
func readSecret(env, prompt string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return v, nil
	}
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// resolveHEYCalendar turns --hey-calendar into a calendar id (0 = every
// calendar switched on in HEY). With no value it lists the account's calendars
// so the user can pick one. A listing that fails (not signed in, rate-limited)
// is only a warning when an explicit id was given, so setup still works offline.
func resolveHEYCalendar(value string) (int64, int) {
	value = strings.TrimSpace(value)
	if _, err := hey.Find(); err != nil {
		return 0, fail("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cals, listErr := (&hey.Client{}).Calendars(ctx)

	if value == "" {
		if listErr != nil {
			return 0, fail("list HEY calendars: %v", listErr)
		}
		fmt.Fprintln(os.Stderr, "Pick a HEY calendar with --hey-calendar <id>, or --hey-calendar all:")
		for _, c := range cals {
			fmt.Fprintf(os.Stderr, "  %-10d %s (%s)\n", c.ID, c.Name, c.Kind)
		}
		return 0, 2
	}
	if strings.EqualFold(value, "all") {
		fmt.Println("Mirroring every calendar switched on in HEY.")
		return 0, 0
	}

	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fail("--hey-calendar must be a calendar id from `hey calendar list`, or \"all\"")
	}
	if listErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not check HEY calendar %d: %v\n", id, listErr)
		return id, 0
	}
	for _, c := range cals {
		if c.ID == id {
			fmt.Printf("Mirroring HEY calendar %q. It must stay switched on in HEY to be read.\n", c.Name)
			return id, 0
		}
	}
	return 0, fail("no HEY calendar with id %d; run `heyday setup` without --hey-calendar to list them", id)
}
