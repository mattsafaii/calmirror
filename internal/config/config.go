// Package config defines Heyday's on-disk configuration: the connected Google
// account and the feeds mirrored into it. Secrets (the Google OAuth refresh
// token and client secret) never live here — they belong in the macOS
// Keychain. HEY sign-in belongs to the hey CLI and is never seen by Heyday.
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

// DefaultHEYFutureDays is how far ahead a HEY feed reads when it does not set
// its own window. HEY feeds expand repeating events into occurrences, which
// needs a bounded window.
const DefaultHEYFutureDays = 60

// appDir is the directory under the user config dir that holds config and state.
const appDir = "heyday"

// Source kinds: where a feed's events come from.
const (
	// SourceHEY reads HEY calendars through the hey CLI, which owns sign-in.
	// It is the default.
	SourceHEY = "hey"
	// SourceICS fetches an ICS feed URL (Basecamp, any calendar feed). Opt-in.
	SourceICS = "ics"
)

// Config is the complete Heyday configuration as stored on disk.
type Config struct {
	Google Google `json:"google"`
	Feeds  []Feed `json:"feeds"`
}

// Google identifies the destination Google account and OAuth client. The OAuth
// refresh token and client secret are stored in the macOS Keychain, keyed by
// Account — never here. ClientID is an identifier (a desktop OAuth client id),
// not a secret, so it lives in config alongside the account.
type Google struct {
	Account  string `json:"account,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// Feed describes one mirror: a source copied into a dedicated, Heyday-owned
// Google calendar. Name is the stable identifier used as the local-state key
// and in CLI output. Source selects where events come from; an empty Source is
// hey. Mutable per-run state (last-sync time, last error, event counts) lives in
// the SQLite store, not in config.
type Feed struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	// HEYCalendarID narrows a HEY feed to one calendar (from `hey calendar
	// list`). 0 means every calendar switched on in HEY.
	HEYCalendarID int64 `json:"hey_calendar_id,omitempty"`
	// SourceURL is the feed URL of an ICS feed.
	SourceURL           string     `json:"source_url,omitempty"`
	DestinationCalendar string     `json:"destination_calendar"`
	SyncWindow          SyncWindow `json:"sync_window"`
}

// SyncWindow bounds which events are mirrored, relative to now. PastDays looks
// backward; FutureDays of 0 means no future bound (ICS feeds only — HEY feeds
// default to DefaultHEYFutureDays).
type SyncWindow struct {
	PastDays   int `json:"past_days"`
	FutureDays int `json:"future_days"`
}

// Dir returns the directory holding Heyday's config and state:
// $HOME/Library/Application Support/heyday on macOS.
func Dir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDir), nil
}

// Path returns the default config file location.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// StateDBPath returns the default SQLite state database location, alongside the
// config file.
func StateDBPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
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
// names the Google account.
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

// FeedByName returns the feed with the given name, or false if none matches.
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
		f := &c.Feeds[i]
		if f.SyncWindow.PastDays == 0 {
			f.SyncWindow.PastDays = DefaultSyncWindowPastDays
		}
		if strings.TrimSpace(f.Source) == "" {
			f.Source = SourceHEY
		}
		if f.Source == SourceHEY && f.SyncWindow.FutureDays == 0 {
			f.SyncWindow.FutureDays = DefaultHEYFutureDays
		}
	}
}

// Validate checks structural invariants: the required fields on each feed,
// unique feed names, a valid source per feed, and a connected Google account
// whenever there is a feed to write.
func (c *Config) Validate() error {
	seen := make(map[string]bool, len(c.Feeds))
	for i, f := range c.Feeds {
		switch {
		case strings.TrimSpace(f.Name) == "":
			return fmt.Errorf("feeds[%d]: name is required", i)
		case strings.TrimSpace(f.DestinationCalendar) == "":
			return fmt.Errorf("feed %q: destination_calendar is required", f.Name)
		case f.SyncWindow.PastDays < 0:
			return fmt.Errorf("feed %q: sync_window.past_days cannot be negative", f.Name)
		case f.SyncWindow.FutureDays < 0:
			return fmt.Errorf("feed %q: sync_window.future_days cannot be negative", f.Name)
		}
		switch f.Source {
		case SourceHEY, "": // "" before applyDefaults
			if strings.TrimSpace(f.SourceURL) != "" {
				return fmt.Errorf("feed %q: source_url does not apply to source %q (the hey CLI reads the calendars)", f.Name, SourceHEY)
			}
			if f.HEYCalendarID < 0 {
				return fmt.Errorf("feed %q: hey_calendar_id cannot be negative", f.Name)
			}
		case SourceICS:
			if strings.TrimSpace(f.SourceURL) == "" {
				return fmt.Errorf("feed %q: source_url is required for source %q", f.Name, SourceICS)
			}
			if f.HEYCalendarID != 0 {
				return fmt.Errorf("feed %q: hey_calendar_id only applies to source %q", f.Name, SourceHEY)
			}
		default:
			return fmt.Errorf("feed %q: unknown source %q (want %q or %q)", f.Name, f.Source, SourceHEY, SourceICS)
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate feed name %q", f.Name)
		}
		seen[f.Name] = true
	}

	if len(c.Feeds) > 0 {
		if strings.TrimSpace(c.Google.Account) == "" {
			return errors.New("google.account is required; run `heyday setup` to connect Google")
		}
		if strings.TrimSpace(c.Google.ClientID) == "" {
			return errors.New("google.client_id is required; run `heyday setup` to connect Google")
		}
	}
	return nil
}
