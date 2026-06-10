package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mattsafaii/calmirror/internal/caldav"
	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/feed"
	"github.com/mattsafaii/calmirror/internal/store"
)

// fakeCalDAV is an in-memory CalDAV stand-in.
type fakeCalDAV struct {
	objects        map[string]string // path -> ics body
	creates        int
	updates        int
	deletes        int
	discoverErr    error
	createErrPaths map[string]error
}

func newFakeCalDAV() *fakeCalDAV {
	return &fakeCalDAV{objects: map[string]string{}, createErrPaths: map[string]error{}}
}

func (f *fakeCalDAV) Discover(ctx context.Context) (caldav.Discovery, error) {
	if f.discoverErr != nil {
		return caldav.Discovery{}, f.discoverErr
	}
	return caldav.Discovery{Principal: "/p/", CalendarHome: "/p/calendars/"}, nil
}

func (f *fakeCalDAV) EnsureCalendar(ctx context.Context, home, name string) (string, error) {
	return home + "mirror/", nil
}

func (f *fakeCalDAV) CreateObject(ctx context.Context, path, ics string) (caldav.PutResult, error) {
	if err := f.createErrPaths[path]; err != nil {
		return caldav.PutResult{}, err
	}
	f.objects[path] = ics
	f.creates++
	return caldav.PutResult{ETag: "etag-create"}, nil
}

func (f *fakeCalDAV) UpdateObject(ctx context.Context, path, ics string) (caldav.PutResult, error) {
	f.objects[path] = ics
	f.updates++
	return caldav.PutResult{ETag: "etag-update"}, nil
}

func (f *fakeCalDAV) DeleteObject(ctx context.Context, path string) error {
	delete(f.objects, path)
	f.deletes++
	return nil
}

// fakeFetcher returns a preset Parsed (or error) each call.
type fakeFetcher struct {
	parsed *feed.Parsed
	err    error
}

func (f *fakeFetcher) Fetch(ctx context.Context, url string) (*feed.Parsed, error) {
	return f.parsed, f.err
}

func mustParse(t *testing.T, body string) *feed.Parsed {
	t.Helper()
	p, err := feed.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return p
}

func cal(events ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n"
}

func vevent(uid, summary string) string {
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nSUMMARY:" + summary +
		"\r\nDTSTART:20260615T170000Z\r\nDTEND:20260615T180000Z\r\nEND:VEVENT\r\n"
}

func newSyncer(t *testing.T, cd CalDAV, ff Fetcher) (*Syncer, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &Syncer{
		Store:   st,
		CalDAV:  cd,
		Fetcher: ff,
		Now:     func() time.Time { return time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) },
	}, st
}

var testFeeds = []config.Feed{{
	Name:                "hey",
	SourceURL:           "https://hey/feed.ics",
	DestinationCalendar: "HEY (synced)",
	SyncWindow:          config.SyncWindow{PastDays: 30},
}}

func TestSyncCreateUpdateDeleteUnchanged(t *testing.T) {
	cd := newFakeCalDAV()
	ff := &fakeFetcher{}
	s, st := newSyncer(t, cd, ff)
	ctx := context.Background()

	// Pass 1: two new events -> both created.
	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha"), vevent("b@x", "Beta")))
	res, err := s.Sync(ctx, testFeeds)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res[0].Created != 2 || res[0].Updated != 0 || res[0].Deleted != 0 {
		t.Fatalf("pass1 = %+v, want 2 created", res[0])
	}
	if len(cd.objects) != 2 {
		t.Fatalf("pass1 objects = %d, want 2", len(cd.objects))
	}

	// Pass 2: identical source -> all unchanged.
	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha"), vevent("b@x", "Beta")))
	res, _ = s.Sync(ctx, testFeeds)
	if res[0].Unchanged != 2 || res[0].Created != 0 || res[0].Updated != 0 {
		t.Fatalf("pass2 = %+v, want 2 unchanged", res[0])
	}

	// Pass 3: edit Alpha -> 1 updated, 1 unchanged.
	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha EDITED"), vevent("b@x", "Beta")))
	res, _ = s.Sync(ctx, testFeeds)
	if res[0].Updated != 1 || res[0].Unchanged != 1 {
		t.Fatalf("pass3 = %+v, want 1 updated 1 unchanged", res[0])
	}

	// Pass 4: drop Beta from source -> 1 deleted.
	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha EDITED")))
	res, _ = s.Sync(ctx, testFeeds)
	if res[0].Deleted != 1 {
		t.Fatalf("pass4 = %+v, want 1 deleted", res[0])
	}
	if len(cd.objects) != 1 {
		t.Fatalf("pass4 objects = %d, want 1", len(cd.objects))
	}
	if n, _ := st.CountLinks("hey"); n != 1 {
		t.Fatalf("pass4 links = %d, want 1", n)
	}
}

func TestSyncFetchErrorLeavesMirrorIntact(t *testing.T) {
	cd := newFakeCalDAV()
	ff := &fakeFetcher{}
	s, st := newSyncer(t, cd, ff)
	ctx := context.Background()

	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha"), vevent("b@x", "Beta")))
	if _, err := s.Sync(ctx, testFeeds); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	if len(cd.objects) != 2 {
		t.Fatalf("seed objects = %d, want 2", len(cd.objects))
	}

	// Now the feed 404s. Nothing should be deleted, and the error is recorded.
	ff.parsed = nil
	ff.err = &feed.HTTPError{URL: "https://hey/feed.ics", StatusCode: 404, Status: "404 Not Found"}
	res, err := s.Sync(ctx, testFeeds)
	if err != nil {
		t.Fatalf("Sync returned fatal error: %v", err)
	}
	if res[0].Err == nil {
		t.Fatalf("expected per-feed error, got nil")
	}
	if res[0].Deleted != 0 || len(cd.objects) != 2 {
		t.Fatalf("mirror was disturbed on fetch error: deleted=%d objects=%d", res[0].Deleted, len(cd.objects))
	}
	fs, _, _ := st.GetFeed("hey")
	if fs.LastError == "" {
		t.Fatalf("feed error not recorded in store")
	}
}

type fakeNotifier struct{ count int }

func (f *fakeNotifier) Notify(title, body string) { f.count++ }

func TestNotifyOnFailureOnsetOnly(t *testing.T) {
	cd := newFakeCalDAV()
	ff := &fakeFetcher{}
	s, _ := newSyncer(t, cd, ff)
	nf := &fakeNotifier{}
	s.Notifier = nf
	ctx := context.Background()

	// Healthy sync first: no notification.
	ff.parsed = mustParse(t, cal(vevent("a@x", "Alpha")))
	if _, err := s.Sync(ctx, testFeeds); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	if nf.count != 0 {
		t.Fatalf("notified on healthy sync: %d", nf.count)
	}

	// Feed breaks -> exactly one notification (onset).
	ff.parsed = nil
	ff.err = &feed.HTTPError{URL: "u", StatusCode: 404, Status: "404 Not Found"}
	if _, err := s.Sync(ctx, testFeeds); err != nil {
		t.Fatalf("failing sync: %v", err)
	}
	if nf.count != 1 {
		t.Fatalf("onset notifications = %d, want 1", nf.count)
	}

	// Still broken on the next run -> no repeat notification (no spam).
	if _, err := s.Sync(ctx, testFeeds); err != nil {
		t.Fatalf("failing sync 2: %v", err)
	}
	if nf.count != 1 {
		t.Fatalf("notifications after persistent failure = %d, want 1 (no spam)", nf.count)
	}
}

func TestSyncDiscoveryErrorIsFatal(t *testing.T) {
	cd := newFakeCalDAV()
	cd.discoverErr = errors.New("401 unauthorized")
	ff := &fakeFetcher{parsed: mustParse(t, cal(vevent("a@x", "Alpha")))}
	s, _ := newSyncer(t, cd, ff)

	if _, err := s.Sync(context.Background(), testFeeds); err == nil {
		t.Fatalf("expected fatal discovery error")
	}
}
