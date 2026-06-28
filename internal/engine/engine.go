// Package engine is CalMirror's mirror core: for each feed it fetches the ICS
// source, diffs it against local state by ICS UID, and creates, updates, or
// deletes events in the feed's dedicated destination calendar (iCloud CalDAV or
// Google Calendar). A failure on one feed is isolated so it cannot corrupt
// another feed's sync.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/feed"
	"github.com/mattsafaii/calmirror/internal/store"
)

// Destination is the abstraction over a calendar backend (iCloud CalDAV or
// Google Calendar). The engine drives it generically: ensure the dedicated
// mirror calendar exists, then create/update/delete events. Each implementation
// owns its own serialization (ICS for CalDAV, the Calendar API event model for
// Google), object naming, and credential handling.
//
// calRef is an opaque calendar handle returned by EnsureCalendar (a CalDAV
// calendar path, a Google calendar id). ref is an opaque per-event handle
// returned by CreateEvent (a CalDAV object href, a Google event id) and stored
// as the EventLink href. etag is an optional version token; an empty value is
// not an error.
type Destination interface {
	EnsureCalendar(ctx context.Context, displayName string) (calRef string, err error)
	CreateEvent(ctx context.Context, calRef string, ev feed.Event, tz []*ics.VTimezone) (ref, etag string, err error)
	UpdateEvent(ctx context.Context, calRef, ref string, ev feed.Event, tz []*ics.VTimezone) (etag string, err error)
	DeleteEvent(ctx context.Context, calRef, ref string) error
}

// DestinationFor returns the Destination a feed should sync to. The CLI wires
// this up from config + Keychain; tests supply a fake. An error here is treated
// as a per-feed failure, isolating (for example) a broken Google credential
// from a healthy iCloud feed.
type DestinationFor func(ctx context.Context, f config.Feed) (Destination, error)

// Fetcher retrieves and parses a feed. *feed.Fetcher satisfies it.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (*feed.Parsed, error)
}

// Notifier surfaces a feed failure to the user. *notify.Osascript satisfies it.
type Notifier interface {
	Notify(title, body string)
}

// Syncer runs sync passes. Now defaults to time.Now if unset; Notifier is
// optional (nil disables notifications).
type Syncer struct {
	Store       *store.Store
	Destination DestinationFor
	Fetcher     Fetcher
	Notifier    Notifier
	Now         func() time.Time
}

// FeedResult summarizes one feed's sync pass.
type FeedResult struct {
	Feed      string
	Created   int
	Updated   int
	Deleted   int
	Unchanged int
	Err       error
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Sync mirrors every feed. The returned error is non-nil only for a setup
// failure that prevents the pass from running at all; per-feed failures
// (including a destination that won't connect) are reported in the
// corresponding FeedResult.Err and recorded in the store, leaving other feeds
// untouched.
func (s *Syncer) Sync(ctx context.Context, feeds []config.Feed) ([]FeedResult, error) {
	if s.Destination == nil {
		return nil, errors.New("engine: no destination factory configured")
	}

	results := make([]FeedResult, 0, len(feeds))
	for _, f := range feeds {
		// Capture prior state so we can notify only on failure onset (a
		// previously-healthy feed that just broke), not on every scheduled run.
		prior, _, _ := s.Store.GetFeed(f.Name)

		res := s.syncFeed(ctx, f)
		now := s.now()
		if res.Err != nil {
			// Best-effort error recording; don't mask the sync error.
			_ = s.Store.RecordSyncError(f.Name, res.Err.Error(), now)
			if s.Notifier != nil && prior.LastError == "" {
				s.Notifier.Notify("CalMirror: "+f.Name+" sync failed", res.Err.Error())
			}
		} else {
			_ = s.Store.RecordSyncSuccess(f.Name, now)
		}
		results = append(results, res)
	}
	return results, nil
}

func (s *Syncer) syncFeed(ctx context.Context, f config.Feed) FeedResult {
	res := FeedResult{Feed: f.Name}

	// Build the feed's destination. A failure here (bad credentials, revoked
	// OAuth) isolates to this feed, leaving other feeds untouched.
	dest, err := s.Destination(ctx, f)
	if err != nil {
		res.Err = err
		return res
	}

	calRef, err := dest.EnsureCalendar(ctx, f.DestinationCalendar)
	if err != nil {
		res.Err = err
		return res
	}
	if err := s.Store.SetDestinationCalendar(f.Name, calRef); err != nil {
		res.Err = err
		return res
	}

	// Fetch before any destructive action: if the source 404s or errors, we
	// return here and the existing mirror is left intact.
	parsed, err := s.Fetcher.Fetch(ctx, f.SourceURL)
	if err != nil {
		res.Err = err
		return res
	}

	start, end := window(s.now(), f.SyncWindow)
	events := filterWindow(parsed.Events, start, end)

	links, err := s.Store.LinksByUID(f.Name)
	if err != nil {
		res.Err = err
		return res
	}

	now := s.now()
	seen := make(map[string]bool, len(events))
	var errs []error

	for _, ev := range events {
		if ev.UID == "" {
			continue // an event with no UID can't be diffed; skip it
		}
		seen[ev.UID] = true
		hash := contentHash(ev)

		link, exists := links[ev.UID]
		switch {
		case !exists:
			ref, etag, err := dest.CreateEvent(ctx, calRef, ev, parsed.Timezones)
			if err != nil {
				errs = append(errs, fmt.Errorf("create %q: %w", ev.UID, err))
				continue
			}
			_ = s.Store.UpsertLink(store.EventLink{
				FeedName: f.Name, UID: ev.UID, Href: ref,
				ETag: etag, ContentHash: hash, LastSeenAt: now,
			})
			res.Created++
		case link.ContentHash != hash:
			etag, err := dest.UpdateEvent(ctx, calRef, link.Href, ev, parsed.Timezones)
			if err != nil {
				errs = append(errs, fmt.Errorf("update %q: %w", ev.UID, err))
				continue
			}
			link.ETag = etag
			link.ContentHash = hash
			link.LastSeenAt = now
			_ = s.Store.UpsertLink(link)
			res.Updated++
		default:
			link.LastSeenAt = now
			_ = s.Store.UpsertLink(link)
			res.Unchanged++
		}
	}

	// Delete mirror events whose source UID is no longer present in the window.
	for uid, link := range links {
		if seen[uid] {
			continue
		}
		if err := dest.DeleteEvent(ctx, calRef, link.Href); err != nil {
			errs = append(errs, fmt.Errorf("delete %q: %w", uid, err))
			continue
		}
		_ = s.Store.DeleteLink(f.Name, uid)
		res.Deleted++
	}

	if len(errs) > 0 {
		res.Err = errors.Join(errs...)
	}
	return res
}

// window returns the [start, end] time bounds for a feed. A FutureDays of 0
// leaves the future unbounded (end is the zero time).
func window(now time.Time, w config.SyncWindow) (start, end time.Time) {
	start = now.AddDate(0, 0, -w.PastDays)
	if w.FutureDays > 0 {
		end = now.AddDate(0, 0, w.FutureDays)
	}
	return start, end
}

// filterWindow keeps events that fall within [start, end]. Recurring events are
// always kept — their RRULE is expanded by the calendar client, so an old
// DTSTART must not exclude them. Events with no usable start time are kept.
func filterWindow(events []feed.Event, start, end time.Time) []feed.Event {
	out := make([]feed.Event, 0, len(events))
	for _, e := range events {
		if e.RRule != "" || e.Start.IsZero() {
			out = append(out, e)
			continue
		}
		// Determine the event's effective end for the lower-bound test.
		evEnd := e.End
		if evEnd.IsZero() {
			evEnd = e.Start
		}
		if evEnd.Before(start) {
			continue
		}
		if !end.IsZero() && e.Start.After(end) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// contentHash is a deterministic fingerprint of the event fields CalMirror
// mirrors. It is computed from normalized fields rather than re-serialized ICS
// because property-parameter order from the parser is not stable, which would
// otherwise cause spurious updates every sync.
func contentHash(e feed.Event) string {
	var b strings.Builder
	writeField := func(s string) { b.WriteString(s); b.WriteByte(0x1f) }
	writeField(e.UID)
	writeField(e.Summary)
	writeField(e.Description)
	writeField(e.Location)
	writeField(e.URL)
	writeField(e.Status)
	writeField(e.RecurrenceID)
	writeField(e.RRule)
	writeField(strings.Join(e.RDates, ","))
	writeField(strings.Join(e.ExDate, ","))
	writeField(strings.Join(e.ExRule, ","))
	writeField(fmt.Sprintf("%t", e.AllDay))
	writeField(fmt.Sprintf("%d", e.Sequence))
	writeField(e.Start.UTC().Format(time.RFC3339))
	writeField(e.End.UTC().Format(time.RFC3339))
	writeField(e.LastModified.UTC().Format(time.RFC3339))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
