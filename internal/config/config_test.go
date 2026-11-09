package config

import (
	"path/filepath"
	"testing"
)

var google = Google{Account: "matt@gmail.com", ClientID: "cid"}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := &Config{
		Google: google,
		Feeds: []Feed{{
			Name:                "hey",
			HEYCalendarID:       42,
			DestinationCalendar: "HEY (Heyday)",
			SyncWindow:          SyncWindow{PastDays: 14, FutureDays: 90},
		}},
	}
	if err := want.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(got.Feeds) != 1 || got.Feeds[0] != want.Feeds[0] || got.Google != want.Google {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// A feed with no source is a HEY feed, with a bounded future window so
// repeating events can be expanded.
func TestDefaultsToHEY(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := &Config{Google: google, Feeds: []Feed{{Name: "hey", DestinationCalendar: "HEY"}}}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	f := got.Feeds[0]
	if f.Source != SourceHEY {
		t.Fatalf("Source = %q, want %q", f.Source, SourceHEY)
	}
	if f.SyncWindow.PastDays != DefaultSyncWindowPastDays || f.SyncWindow.FutureDays != DefaultHEYFutureDays {
		t.Fatalf("window = %+v, want %d past / %d future", f.SyncWindow, DefaultSyncWindowPastDays, DefaultHEYFutureDays)
	}
}

// An ICS feed is opt-in and keeps an unbounded future by default.
func TestICSFeedOptIn(t *testing.T) {
	c := &Config{Google: google, Feeds: []Feed{{Name: "bc", Source: SourceICS, SourceURL: "https://x/y.ics", DestinationCalendar: "BC"}}}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Feeds[0].SyncWindow.FutureDays != 0 {
		t.Fatalf("an ICS feed's future window should stay unbounded, got %d", c.Feeds[0].SyncWindow.FutureDays)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]*Config{
		"no google account": {Google: Google{ClientID: "cid"}, Feeds: []Feed{{Name: "f", DestinationCalendar: "d"}}},
		"no client id":      {Google: Google{Account: "a@gmail.com"}, Feeds: []Feed{{Name: "f", DestinationCalendar: "d"}}},
		"no dest cal":       {Google: google, Feeds: []Feed{{Name: "f"}}},
		"no name":           {Google: google, Feeds: []Feed{{DestinationCalendar: "d"}}},
		"dup names": {Google: google, Feeds: []Feed{
			{Name: "f", DestinationCalendar: "d"},
			{Name: "f", DestinationCalendar: "d2"},
		}},
		"unknown source":        {Google: google, Feeds: []Feed{{Name: "f", Source: "outlook", DestinationCalendar: "d"}}},
		"hey with url":          {Google: google, Feeds: []Feed{{Name: "f", Source: SourceHEY, SourceURL: "u", DestinationCalendar: "d"}}},
		"ics without url":       {Google: google, Feeds: []Feed{{Name: "f", Source: SourceICS, DestinationCalendar: "d"}}},
		"ics with hey calendar": {Google: google, Feeds: []Feed{{Name: "f", Source: SourceICS, SourceURL: "u", HEYCalendarID: 3, DestinationCalendar: "d"}}},
		"negative calendar":     {Google: google, Feeds: []Feed{{Name: "f", HEYCalendarID: -1, DestinationCalendar: "d"}}},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}

	ok := map[string]*Config{
		"no feeds yet":   {},
		"hey feed":       {Google: google, Feeds: []Feed{{Name: "h", DestinationCalendar: "d"}}},
		"hey + ics feed": {Google: google, Feeds: []Feed{{Name: "h", DestinationCalendar: "d"}, {Name: "i", Source: SourceICS, SourceURL: "u", DestinationCalendar: "d2"}}},
	}
	for name, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%s: expected valid, got %v", name, err)
		}
	}
}
