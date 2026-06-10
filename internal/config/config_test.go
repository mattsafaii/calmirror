package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := &Config{
		ICloud: ICloud{Username: "matt@icloud.com"},
		Feeds: []Feed{{
			Name:                "hey",
			SourceURL:           "https://hey.com/feed.ics",
			DestinationCalendar: "HEY (synced)",
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
	if len(got.Feeds) != 1 || got.Feeds[0] != want.Feeds[0] || got.ICloud != want.ICloud {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDefaultSyncWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := &Config{
		ICloud: ICloud{Username: "u@icloud.com"},
		Feeds:  []Feed{{Name: "f", SourceURL: "https://x/y.ics", DestinationCalendar: "F"}},
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got.Feeds[0].SyncWindow.PastDays != DefaultSyncWindowPastDays {
		t.Fatalf("PastDays = %d, want default %d", got.Feeds[0].SyncWindow.PastDays, DefaultSyncWindowPastDays)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]*Config{
		"no username":   {Feeds: []Feed{{Name: "f", SourceURL: "u", DestinationCalendar: "d"}}},
		"no source url": {ICloud: ICloud{Username: "u"}, Feeds: []Feed{{Name: "f", DestinationCalendar: "d"}}},
		"no dest cal":   {ICloud: ICloud{Username: "u"}, Feeds: []Feed{{Name: "f", SourceURL: "u"}}},
		"dup names": {ICloud: ICloud{Username: "u"}, Feeds: []Feed{
			{Name: "f", SourceURL: "u", DestinationCalendar: "d"},
			{Name: "f", SourceURL: "u2", DestinationCalendar: "d2"},
		}},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}
