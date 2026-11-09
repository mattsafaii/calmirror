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

func TestValidateKinds(t *testing.T) {
	bad := map[string]*Config{
		"unknown kind": {ICloud: ICloud{Username: "u"}, Feeds: []Feed{
			{Name: "f", Kind: "outlook", SourceURL: "u", DestinationCalendar: "d"}}},
		"google feed without account": {Feeds: []Feed{
			{Name: "f", Kind: KindGoogle, SourceURL: "u", DestinationCalendar: "d"}}},
		"google feed without client id": {Google: Google{Account: "a@gmail.com"}, Feeds: []Feed{
			{Name: "f", Kind: KindGoogle, SourceURL: "u", DestinationCalendar: "d"}}},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}

	// A Google-only config needs no iCloud account; an iCloud-only config needs
	// no Google block. Both should validate.
	ok := map[string]*Config{
		"google only": {Google: Google{Account: "a@gmail.com", ClientID: "cid"}, Feeds: []Feed{
			{Name: "g", Kind: KindGoogle, SourceURL: "u", DestinationCalendar: "d"}}},
		"icloud only": {ICloud: ICloud{Username: "u"}, Feeds: []Feed{
			{Name: "i", Kind: KindICloud, SourceURL: "u", DestinationCalendar: "d"}}},
		"mixed": {ICloud: ICloud{Username: "u"}, Google: Google{Account: "a@gmail.com", ClientID: "cid"}, Feeds: []Feed{
			{Name: "i", Kind: KindICloud, SourceURL: "u", DestinationCalendar: "d"},
			{Name: "g", Kind: KindGoogle, SourceURL: "u", DestinationCalendar: "d"}}},
	}
	for name, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%s: expected valid, got %v", name, err)
		}
	}
}

func TestHEYFeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := &Config{
		Google: Google{Account: "a@gmail.com", ClientID: "cid"},
		Feeds: []Feed{{Name: "hey", Kind: KindGoogle, Source: SourceHEY, HEYCalendarID: 42,
			DestinationCalendar: "HEY (synced)"}},
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	f := got.Feeds[0]
	if f.Source != SourceHEY || f.HEYCalendarID != 42 {
		t.Fatalf("source = %q, calendar = %d", f.Source, f.HEYCalendarID)
	}
	if f.SyncWindow.FutureDays != DefaultHEYFutureDays {
		t.Fatalf("FutureDays = %d, want the bounded HEY default %d", f.SyncWindow.FutureDays, DefaultHEYFutureDays)
	}
}

func TestDefaultSourceIsICS(t *testing.T) {
	c := &Config{ICloud: ICloud{Username: "u"}, Feeds: []Feed{{Name: "f", SourceURL: "u", DestinationCalendar: "d"}}}
	c.applyDefaults()
	if c.Feeds[0].Source != SourceICS {
		t.Fatalf("Source = %q, want %q", c.Feeds[0].Source, SourceICS)
	}
	if c.Feeds[0].SyncWindow.FutureDays != 0 {
		t.Fatalf("an ICS feed's future window should stay unbounded, got %d", c.Feeds[0].SyncWindow.FutureDays)
	}
}

func TestValidateSources(t *testing.T) {
	icloud := ICloud{Username: "u"}
	bad := map[string]*Config{
		"unknown source": {ICloud: icloud, Feeds: []Feed{
			{Name: "f", Kind: KindICloud, Source: "outlook", SourceURL: "u", DestinationCalendar: "d"}}},
		"hey with url": {ICloud: icloud, Feeds: []Feed{
			{Name: "f", Kind: KindICloud, Source: SourceHEY, SourceURL: "u", DestinationCalendar: "d"}}},
		"ics with hey calendar": {ICloud: icloud, Feeds: []Feed{
			{Name: "f", Kind: KindICloud, Source: SourceICS, SourceURL: "u", HEYCalendarID: 3, DestinationCalendar: "d"}}},
		"ics without url": {ICloud: icloud, Feeds: []Feed{
			{Name: "f", Kind: KindICloud, Source: SourceICS, DestinationCalendar: "d"}}},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
	ok := &Config{ICloud: icloud, Feeds: []Feed{
		{Name: "h", Kind: KindICloud, Source: SourceHEY, DestinationCalendar: "d"}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("hey feed without a url: expected valid, got %v", err)
	}
}
