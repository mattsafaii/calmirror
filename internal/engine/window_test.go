package engine

import (
	"testing"
	"time"

	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/feed"
)

func TestWindow(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	start, end := window(now, config.SyncWindow{PastDays: 30})
	if want := now.AddDate(0, 0, -30); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if !end.IsZero() {
		t.Errorf("end = %v, want zero (unbounded future)", end)
	}

	_, end = window(now, config.SyncWindow{PastDays: 30, FutureDays: 90})
	if want := now.AddDate(0, 0, 90); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestFilterWindow(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	start, end := window(now, config.SyncWindow{PastDays: 30, FutureDays: 90})

	at := func(days int) time.Time { return now.AddDate(0, 0, days) }
	events := []feed.Event{
		{UID: "old", Start: at(-45), End: at(-45)},                    // before window -> drop
		{UID: "recent-past", Start: at(-10), End: at(-10)},            // in window -> keep
		{UID: "future-in", Start: at(45), End: at(45)},                // in window -> keep
		{UID: "far-future", Start: at(200), End: at(200)},             // past end -> drop
		{UID: "recurring-old", Start: at(-365), RRule: "FREQ=WEEKLY"}, // recurring -> keep
		{UID: "no-start"}, // unknown time -> keep
	}

	got := map[string]bool{}
	for _, e := range filterWindow(events, start, end) {
		got[e.UID] = true
	}
	keep := []string{"recent-past", "future-in", "recurring-old", "no-start"}
	drop := []string{"old", "far-future"}
	for _, uid := range keep {
		if !got[uid] {
			t.Errorf("expected %q kept", uid)
		}
	}
	for _, uid := range drop {
		if got[uid] {
			t.Errorf("expected %q dropped", uid)
		}
	}
}
