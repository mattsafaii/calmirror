// Package store is CalMirror's local state: the mapping from a source ICS UID
// to the CalDAV event it was mirrored to, plus per-feed sync state. The config
// file owns feed *definitions* (source URL, destination calendar, sync window);
// this store owns mutable runtime state. Secrets never live here — they belong
// in the macOS Keychain.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// schema is applied idempotently on Open. Feeds are keyed by their config name.
const schema = `
CREATE TABLE IF NOT EXISTS feeds (
    name                       TEXT PRIMARY KEY,
    destination_calendar_href  TEXT NOT NULL DEFAULT '',
    last_sync_at               INTEGER NOT NULL DEFAULT 0,
    last_error                 TEXT NOT NULL DEFAULT '',
    last_error_at              INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS event_links (
    feed_name     TEXT NOT NULL,
    uid           TEXT NOT NULL,
    href          TEXT NOT NULL,
    etag          TEXT NOT NULL DEFAULT '',
    content_hash  TEXT NOT NULL DEFAULT '',
    last_seen_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_name, uid),
    FOREIGN KEY (feed_name) REFERENCES feeds(name) ON DELETE CASCADE
);
`

// FeedState is the mutable per-feed state tracked across syncs. A zero
// LastSyncAt means the feed has never synced; an empty LastError means the last
// sync succeeded.
type FeedState struct {
	Name                    string
	DestinationCalendarHref string
	LastSyncAt              time.Time
	LastError               string
	LastErrorAt             time.Time
}

// EventLink maps one source event (by ICS UID) to the CalDAV event it was
// mirrored to, with the data needed for change detection.
type EventLink struct {
	FeedName    string
	UID         string
	Href        string
	ETag        string
	ContentHash string
	LastSeenAt  time.Time
}

// Store is a handle to the local SQLite state database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection keeps an in-memory DB coherent and is plenty for a
	// single-process CLI; the pragmas make file-backed use robust.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %q: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// EnsureFeed creates a feed row if absent, leaving any existing state intact.
func (s *Store) EnsureFeed(name string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO feeds (name) VALUES (?)`, name)
	return err
}

// GetFeed returns the stored state for a feed. The bool is false if the feed
// has no row yet.
func (s *Store) GetFeed(name string) (FeedState, bool, error) {
	var (
		fs                  FeedState
		lastSync, lastErrAt int64
	)
	fs.Name = name
	err := s.db.QueryRow(
		`SELECT destination_calendar_href, last_sync_at, last_error, last_error_at
		   FROM feeds WHERE name = ?`, name,
	).Scan(&fs.DestinationCalendarHref, &lastSync, &fs.LastError, &lastErrAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FeedState{}, false, nil
	}
	if err != nil {
		return FeedState{}, false, err
	}
	fs.LastSyncAt = fromUnix(lastSync)
	fs.LastErrorAt = fromUnix(lastErrAt)
	return fs, true, nil
}

// SetDestinationCalendar records the resolved CalDAV calendar href for a feed,
// creating the feed row if needed.
func (s *Store) SetDestinationCalendar(name, href string) error {
	if err := s.EnsureFeed(name); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`UPDATE feeds SET destination_calendar_href = ? WHERE name = ?`, href, name)
	return err
}

// RecordSyncSuccess marks a successful sync at time at, clearing any prior error.
func (s *Store) RecordSyncSuccess(name string, at time.Time) error {
	if err := s.EnsureFeed(name); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`UPDATE feeds SET last_sync_at = ?, last_error = '', last_error_at = 0
		   WHERE name = ?`, at.Unix(), name)
	return err
}

// RecordSyncError records a persistent feed failure for surfacing in status,
// without clearing the last successful sync time.
func (s *Store) RecordSyncError(name, msg string, at time.Time) error {
	if err := s.EnsureFeed(name); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`UPDATE feeds SET last_error = ?, last_error_at = ? WHERE name = ?`,
		msg, at.Unix(), name)
	return err
}

// LinksByUID returns all event links for a feed, keyed by ICS UID.
func (s *Store) LinksByUID(feedName string) (map[string]EventLink, error) {
	rows, err := s.db.Query(
		`SELECT uid, href, etag, content_hash, last_seen_at
		   FROM event_links WHERE feed_name = ?`, feedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := make(map[string]EventLink)
	for rows.Next() {
		var (
			l        EventLink
			lastSeen int64
		)
		l.FeedName = feedName
		if err := rows.Scan(&l.UID, &l.Href, &l.ETag, &l.ContentHash, &lastSeen); err != nil {
			return nil, err
		}
		l.LastSeenAt = fromUnix(lastSeen)
		links[l.UID] = l
	}
	return links, rows.Err()
}

// UpsertLink inserts or replaces the link for (feed, UID).
func (s *Store) UpsertLink(l EventLink) error {
	_, err := s.db.Exec(
		`INSERT INTO event_links (feed_name, uid, href, etag, content_hash, last_seen_at)
		      VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(feed_name, uid) DO UPDATE SET
		      href = excluded.href,
		      etag = excluded.etag,
		      content_hash = excluded.content_hash,
		      last_seen_at = excluded.last_seen_at`,
		l.FeedName, l.UID, l.Href, l.ETag, l.ContentHash, l.LastSeenAt.Unix())
	return err
}

// DeleteLink removes the link for (feed, UID).
func (s *Store) DeleteLink(feedName, uid string) error {
	_, err := s.db.Exec(
		`DELETE FROM event_links WHERE feed_name = ? AND uid = ?`, feedName, uid)
	return err
}

// CountLinks returns how many events are currently mirrored for a feed.
func (s *Store) CountLinks(feedName string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM event_links WHERE feed_name = ?`, feedName).Scan(&n)
	return n, err
}

// fromUnix converts a stored epoch-seconds value to a time, mapping 0 to the
// zero time so callers can test with IsZero.
func fromUnix(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
