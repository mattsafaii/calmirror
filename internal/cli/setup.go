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
	"github.com/mattsafaii/calmirror/internal/google"
	"github.com/mattsafaii/calmirror/internal/keychain"
)

// Secrets may be supplied non-interactively (e.g. piped in CI) instead of via
// the stdin prompt.
const (
	passwordEnv     = "CALMIRROR_ICLOUD_PASSWORD"
	googleSecretEnv = "CALMIRROR_GOOGLE_CLIENT_SECRET"
)

func cmdSetup(args []string) int {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	var (
		kind       = fset.String("kind", config.KindICloud, "destination kind: icloud or google")
		name       = fset.String("feed", "", "short feed name (stable identifier)")
		url        = fset.String("url", "", "source ICS feed URL")
		calendar   = fset.String("calendar", "", "destination calendar name (created if absent)")
		pastDays   = fset.Int("past-days", config.DefaultSyncWindowPastDays, "mirror events from this many days in the past")
		futureDays = fset.Int("future-days", 0, "mirror events up to this many days ahead (0 = unbounded)")
		skipVerify = fset.Bool("skip-verify", false, "skip the destination connection check (iCloud only)")

		icloudUser     = fset.String("icloud-user", "", "iCloud Apple ID / email (icloud kind)")
		googleClientID = fset.String("google-client-id", "", "Google OAuth desktop client id (google kind)")
	)
	fset.Usage = func() {
		out := fset.Output()
		fmt.Fprintf(out, "Usage:\n")
		fmt.Fprintf(out, "  calmirror setup --kind icloud --icloud-user <email> --feed <name> --url <ics-url> --calendar <name>\n")
		fmt.Fprintf(out, "  calmirror setup --kind google --google-client-id <id> --feed <name> --url <ics-url> --calendar <name>\n\n")
		fmt.Fprintf(out, "Adds or updates a feed routed to the chosen destination.\n")
		fmt.Fprintf(out, "iCloud: the app-specific password is read from $%s if set, else prompted.\n", passwordEnv)
		fmt.Fprintf(out, "Google: the client secret is read from $%s if set, else prompted; a browser\n", googleSecretEnv)
		fmt.Fprintf(out, "        consent stores the refresh token in the Keychain. Re-run without\n")
		fmt.Fprintf(out, "        --google-client-id to add more Google feeds on the connected account.\n\n")
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}

	*kind = strings.ToLower(strings.TrimSpace(*kind))
	if *kind != config.KindICloud && *kind != config.KindGoogle {
		fset.Usage()
		return fail("--kind must be %q or %q", config.KindICloud, config.KindGoogle)
	}

	// Fields required regardless of destination kind.
	missing := []string{}
	for flagName, val := range map[string]string{"feed": *name, "url": *url, "calendar": *calendar} {
		if strings.TrimSpace(val) == "" {
			missing = append(missing, "--"+flagName)
		}
	}
	if len(missing) > 0 {
		fset.Usage()
		return fail("missing required flags: %s", strings.Join(missing, ", "))
	}

	// Load existing config or start fresh.
	cfg, err := config.Load()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fail("load config: %v", err)
		}
		cfg = &config.Config{}
	}

	switch *kind {
	case config.KindICloud:
		if rc := setupICloud(cfg, *icloudUser, *skipVerify); rc != 0 {
			return rc
		}
	case config.KindGoogle:
		if rc := setupGoogle(cfg, *googleClientID); rc != 0 {
			return rc
		}
	}

	// Add or replace the feed by name.
	feedCfg := config.Feed{
		Name:                *name,
		Kind:                *kind,
		SourceURL:           *url,
		DestinationCalendar: *calendar,
		SyncWindow:          config.SyncWindow{PastDays: *pastDays, FutureDays: *futureDays},
	}
	upsertFeed(cfg, feedCfg)

	if err := cfg.Save(); err != nil {
		return fail("save config: %v", err)
	}
	path, _ := config.Path()
	fmt.Printf("Saved feed %q (%s) -> calendar %q (config: %s)\n", *name, *kind, *calendar, path)
	return 0
}

// setupICloud handles iCloud credentials: it requires the username, stores the
// app-specific password in the Keychain, and (unless skipped) verifies the
// connection. It mutates cfg.ICloud.
func setupICloud(cfg *config.Config, user string, skipVerify bool) int {
	if strings.TrimSpace(user) == "" {
		return fail("--icloud-user is required for --kind icloud")
	}
	password, err := readSecret(passwordEnv, "iCloud app-specific password: ")
	if err != nil {
		return fail("%v", err)
	}
	if password == "" {
		return fail("app-specific password is required")
	}
	if err := keychain.Set(user, password); err != nil {
		return fail("%v", err)
	}
	cfg.ICloud.Username = user

	if skipVerify {
		return 0
	}
	if err := verifyICloud(user, password); err != nil {
		fmt.Fprintf(os.Stderr, "warning: iCloud connection check failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "credentials were stored; fix them and re-run setup, or run `calmirror sync`.\n")
		return 0
	}
	fmt.Println("iCloud connection OK.")
	return 0
}

// setupGoogle handles the Google OAuth path. If clientID is given (or no account
// is connected yet) it runs the browser consent flow, stores the refresh token
// and client secret in the Keychain, and records the account + client id in
// cfg.Google. If clientID is empty and an account is already connected, it
// reuses that connection so more Google feeds can be added without re-consent.
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

// verifyICloud confirms the iCloud credentials reach the server and discovery
// works.
func verifyICloud(user, password string) error {
	client, err := caldav.New(user, password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = client.Discover(ctx)
	return err
}
