// Package google is CalMirror's Google Calendar destination: the installed-app
// OAuth flow that connects an account, the Keychain-backed credential storage,
// and the Destination implementation that mirrors events into a dedicated,
// CalMirror-owned Google calendar.
//
// Secrets never touch config or SQLite. The OAuth refresh token and the client
// secret live in the macOS Keychain; only the account email and the (public)
// desktop client id live in config.
package google

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/mattsafaii/calmirror/internal/keychain"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Keychain account prefixes namespace Google secrets so they never collide with
// the iCloud app-specific password (which is keyed by the bare iCloud username).
const (
	keyRefreshPrefix = "google-refresh:"
	keySecretPrefix  = "google-secret:"
)

// oauthConfig builds the OAuth2 config for the Calendar scope. The calendar
// scope is required to create the dedicated mirror calendar and write events.
func oauthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     googleoauth.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{calendar.CalendarScope},
	}
}

// Connect runs the installed-app loopback OAuth flow: it spins up a localhost
// callback server, opens the consent page in the browser, and exchanges the
// returned code for a token. It returns the connected account email and the
// refresh token. AccessTypeOffline + prompt=consent ensure Google issues a
// refresh token even on re-consent.
func Connect(ctx context.Context, clientID, clientSecret string) (account, refreshToken string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("google: open loopback listener: %w", err)
	}
	defer ln.Close()

	redirectURL := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	cfg := oauthConfig(clientID, clientSecret, redirectURL)

	state, err := randomState()
	if err != nil {
		return "", "", err
	}

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "Authorization failed: "+e, http.StatusBadRequest)
			resultCh <- result{err: fmt.Errorf("google: consent denied: %s", e)}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			resultCh <- result{err: fmt.Errorf("google: oauth state mismatch")}
			return
		}
		fmt.Fprintln(w, "CalMirror is connected. You can close this tab and return to the terminal.")
		resultCh <- result{code: q.Get("code")}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Println("Opening your browser to authorize CalMirror's Google access.")
	fmt.Println("If it does not open, visit this URL:")
	fmt.Println("  " + authURL)
	_ = openBrowser(authURL)

	var code string
	select {
	case res := <-resultCh:
		if res.err != nil {
			return "", "", res.err
		}
		code = res.code
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-time.After(5 * time.Minute):
		return "", "", fmt.Errorf("google: timed out waiting for authorization")
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", "", fmt.Errorf("google: exchange code: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", "", fmt.Errorf("google: no refresh token returned; revoke prior access and retry")
	}

	// Resolve the connected account's email via its primary calendar id.
	svc, err := calendar.NewService(ctx, option.WithTokenSource(cfg.TokenSource(ctx, tok)))
	if err != nil {
		return "", "", fmt.Errorf("google: build calendar service: %w", err)
	}
	primary, err := svc.CalendarList.Get("primary").Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("google: identify account: %w", err)
	}
	return primary.Id, tok.RefreshToken, nil
}

// StoreCredentials persists the client secret and refresh token for account in
// the Keychain.
func StoreCredentials(account, clientSecret, refreshToken string) error {
	if err := keychain.Set(keySecretPrefix+account, clientSecret); err != nil {
		return err
	}
	return keychain.Set(keyRefreshPrefix+account, refreshToken)
}

// DeleteCredentials removes the stored secret and refresh token for account.
// Missing items are ignored.
func DeleteCredentials(account string) error {
	for _, key := range []string{keySecretPrefix + account, keyRefreshPrefix + account} {
		if err := keychain.Delete(key); err != nil && err != keychain.ErrNotFound {
			return err
		}
	}
	return nil
}

// NewService builds an authenticated Calendar service for account from the
// stored client secret and refresh token. An expired or revoked token surfaces
// on the first API call as an authentication error, isolated to this feed.
func NewService(ctx context.Context, account, clientID string) (*calendar.Service, error) {
	secret, err := keychain.Get(keySecretPrefix + account)
	if err != nil {
		return nil, fmt.Errorf("google: read client secret for %q: %w", account, err)
	}
	refresh, err := keychain.Get(keyRefreshPrefix + account)
	if err != nil {
		return nil, fmt.Errorf("google: read refresh token for %q: %w", account, err)
	}
	cfg := oauthConfig(clientID, secret, "")
	// An empty access token with a refresh token makes the reusing TokenSource
	// mint a fresh access token on first use.
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh})
	svc, err := calendar.NewService(ctx, option.WithTokenSource(src))
	if err != nil {
		return nil, fmt.Errorf("google: build calendar service: %w", err)
	}
	return svc, nil
}

// randomState returns a random URL-safe OAuth state token.
func randomState() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// openBrowser opens url in the default browser on macOS.
func openBrowser(url string) error {
	return exec.Command("open", url).Start()
}
