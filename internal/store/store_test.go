package store

import (
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFeedStateLifecycle(t *testing.T) {
	s := openTest(t)

	if _, ok, err := s.GetFeed("hey"); err != nil || ok {
		t.Fatalf("GetFeed before create: ok=%v err=%v", ok, err)
	}

	if err := s.SetDestinationCalendar("hey", "/cal/hey-synced/"); err != nil {
		t.Fatalf("SetDestinationCalendar: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	if err := s.RecordSyncError("hey", "404 from feed", now); err != nil {
		t.Fatalf("RecordSyncError: %v", err)
	}

	fs, ok, err := s.GetFeed("hey")
	if err != nil || !ok {
		t.Fatalf("GetFeed: ok=%v err=%v", ok, err)
	}
	if fs.DestinationCalendarHref != "/cal/hey-synced/" {
		t.Errorf("href = %q", fs.DestinationCalendarHref)
	}
	if fs.LastError != "404 from feed" || fs.LastErrorAt.IsZero() {
		t.Errorf("error not recorded: %+v", fs)
	}
	if !fs.LastSyncAt.IsZero() {
		t.Errorf("LastSyncAt should be zero before any success, got %v", fs.LastSyncAt)
	}

	if err := s.RecordSyncSuccess("hey", now.Add(time.Hour)); err != nil {
		t.Fatalf("RecordSyncSuccess: %v", err)
	}
	fs, _, _ = s.GetFeed("hey")
	if fs.LastError != "" || !fs.LastErrorAt.IsZero() {
		t.Errorf("success did not clear error: %+v", fs)
	}
	if fs.LastSyncAt.IsZero() {
		t.Errorf("LastSyncAt not set after success")
	}
}

func TestEventLinks(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureFeed("hey"); err != nil {
		t.Fatalf("EnsureFeed: %v", err)
	}

	seen := time.Unix(1_700_000_000, 0)
	link := EventLink{
		FeedName:    "hey",
		UID:         "evt-1@hey.com",
		Href:        "/cal/hey/evt-1.ics",
		ETag:        `"abc"`,
		ContentHash: "hash1",
		LastSeenAt:  seen,
	}
	if err := s.UpsertLink(link); err != nil {
		t.Fatalf("UpsertLink insert: %v", err)
	}

	// Update the same UID.
	link.ContentHash = "hash2"
	link.ETag = `"def"`
	if err := s.UpsertLink(link); err != nil {
		t.Fatalf("UpsertLink update: %v", err)
	}

	links, err := s.LinksByUID("hey")
	if err != nil {
		t.Fatalf("LinksByUID: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1", len(links))
	}
	got := links["evt-1@hey.com"]
	if got.ContentHash != "hash2" || got.ETag != `"def"` {
		t.Errorf("update not applied: %+v", got)
	}
	if !got.LastSeenAt.Equal(seen) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, seen)
	}

	if n, _ := s.CountLinks("hey"); n != 1 {
		t.Errorf("CountLinks = %d, want 1", n)
	}

	if err := s.DeleteLink("hey", "evt-1@hey.com"); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if n, _ := s.CountLinks("hey"); n != 0 {
		t.Errorf("CountLinks after delete = %d, want 0", n)
	}
}
