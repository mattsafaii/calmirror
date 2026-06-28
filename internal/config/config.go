// Package config defines the on-disk CalMirror configuration: the destination
// accounts (iCloud, Google) and the set of feeds to mirror. Secrets (the iCloud
// app-specific password, the Google OAuth refresh token and client secret)
// never live here — they belong in the macOS Keychain.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSyncWindowPastDays is how far back in time events are mirrored when a
// feed does not specify its own window.
const DefaultSyncWindowPastDays = 30

// Destination kinds. A feed routes to exactly one.
const (
	KindICloud = "icloud"
	KindGoogle = "google"
)

// Config is the complete CalMirror configuration as stored on disk. The iCloud
// and Google blocks are each optional; a block is required only when at least
// one feed targets that kind.
type Config struct {
	ICloud ICloud `json:"icloud"`
	Google Google `json:"google"`
	Feeds  []Feed `json:"feeds"`
}

// ICloud identifies the destination iCloud account. The matching app-specific
// password is stored in the macOS Keychain, keyed by Username — never here.
type ICloud struct {
	// Username is the Apple ID / iCloud email used for CalDAV auth.
	Username string `json:"username"`
}

// Google identifies the destination Google account and OAuth client. The OAuth
// refresh token and client secret are stored in the macOS Keychain, keyed by
// Account — never here. ClientID is an identifier (a desktop OAuth client id),
// not a secret, so it lives in config alongside the account.
type Google struct {
	Account  string `json:"account,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// Feed describes one mirror: an ICS source fetched into a dedicated calendar on
// the chosen destination. Name is the stable identifier used as the local-state
// key and in CLI output. Kind selects the destination backend (icloud/google);
// an empty Kind defaults to icloud for Phase 1 backward compatibility. Mutable
// per-run state (last-sync time, last error, event counts) lives in the SQLite
// store, not in config.
type Feed struct {
	Name                string     `json:"name"`
	Kind                string     `json:"kind"`
	SourceURL           string     `json:"source_url"`
	DestinationCalendar string     `json:"destination_calendar"`
	SyncWindow          SyncWindow `json:"sync_window"`
}

// SyncWindow bounds which events are mirrored, relative to now. PastDays looks
// backward; FutureDays of 0 means no future bound (mirror everything ahead).
type SyncWindow struct {
	PastDays   int `json:"past_days"`
	FutureDays int `json:"future_days"`
}

// Path returns the default config file location:
// $HOME/Library/Application Support/calmirror/config.json on macOS.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "calmirror", "config.json"), nil
}

// StateDBPath returns the default SQLite state database location, alongside the
// config file.
func StateDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "calmirror", "state.db"), nil
}

// Load reads and validates the config at the default Path. A missing file
// returns an error wrapping fs.ErrNotExist, which callers can test with
// errors.Is to distinguish "not set up yet" from a real failure.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads and validates the config at an explicit path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config to the default Path, creating the parent directory and
// validating before any bytes are written. The file is written 0600 since it
// names the iCloud account.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return c.SaveTo(path)
}

// SaveTo writes the config to an explicit path with the same guarantees as Save.
func (c *Config) SaveTo(path string) error {
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// Feed returns the feed with the given name, or false if none matches.
func (c *Config) FeedByName(name string) (Feed, bool) {
	for _, f := range c.Feeds {
		if f.Name == name {
			return f, true
		}
	}
	return Feed{}, false
}

func (c *Config) applyDefaults() {
	for i := range c.Feeds {
		if c.Feeds[i].SyncWindow.PastDays == 0 {
			c.Feeds[i].SyncWindow.PastDays = DefaultSyncWindowPastDays
		}
		// An empty kind means a Phase 1 config that predates Google support;
		// those feeds are iCloud.
		if strings.TrimSpace(c.Feeds[i].Kind) == "" {
			c.Feeds[i].Kind = KindICloud
		}
	}
}

// Validate checks structural invariants: the required fields on each feed,
// unique feed names, a valid destination kind per feed, and the destination
// account for each kind actually in use.
func (c *Config) Validate() error {
	seen := make(map[string]bool, len(c.Feeds))
	var usesICloud, usesGoogle bool
	for i, f := range c.Feeds {
		switch {
		case strings.TrimSpace(f.Name) == "":
			return fmt.Errorf("feeds[%d]: name is required", i)
		case strings.TrimSpace(f.SourceURL) == "":
			return fmt.Errorf("feed %q: source_url is required", f.Name)
		case strings.TrimSpace(f.DestinationCalendar) == "":
			return fmt.Errorf("feed %q: destination_calendar is required", f.Name)
		case f.SyncWindow.PastDays < 0:
			return fmt.Errorf("feed %q: sync_window.past_days cannot be negative", f.Name)
		case f.SyncWindow.FutureDays < 0:
			return fmt.Errorf("feed %q: sync_window.future_days cannot be negative", f.Name)
		}
		switch f.Kind {
		case KindICloud:
			usesICloud = true
		case KindGoogle:
			usesGoogle = true
		default:
			return fmt.Errorf("feed %q: unknown destination kind %q (want %q or %q)",
				f.Name, f.Kind, KindICloud, KindGoogle)
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate feed name %q", f.Name)
		}
		seen[f.Name] = true
	}

	// A destination account is required only for the kinds actually in use, so
	// a Google-only setup needs no iCloud account and vice versa.
	if usesICloud && strings.TrimSpace(c.ICloud.Username) == "" {
		return errors.New("icloud.username is required for iCloud feeds")
	}
	if usesGoogle {
		if strings.TrimSpace(c.Google.Account) == "" {
			return errors.New("google.account is required for Google feeds")
		}
		if strings.TrimSpace(c.Google.ClientID) == "" {
			return errors.New("google.client_id is required for Google feeds")
		}
	}
	return nil
}
