package caldav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	ics "github.com/arran4/golang-ical"
	"github.com/mattsafaii/calmirror/internal/feed"
)

// Destination adapts the iCloud CalDAV Client to the engine's Destination
// interface. It owns CalDAV-specific concerns the engine should not know about:
// lazily discovering the calendar-home-set, naming event objects, and rendering
// events to ICS. The calRef it returns from EnsureCalendar is a CalDAV calendar
// path; the per-event ref is the object's href.
type Destination struct {
	client *Client

	mu   sync.Mutex
	home string // calendar-home-set path, discovered once and cached
}

// NewDestination wraps a CalDAV client as an engine destination.
func NewDestination(client *Client) *Destination {
	return &Destination{client: client}
}

// calendarHome discovers and caches the account's calendar-home-set. The first
// call is the credential check: bad credentials surface here as an auth error.
func (d *Destination) calendarHome(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.home != "" {
		return d.home, nil
	}
	disc, err := d.client.Discover(ctx)
	if err != nil {
		return "", err
	}
	d.home = disc.CalendarHome
	return d.home, nil
}

// EnsureCalendar finds or creates the dedicated mirror calendar by display name
// and returns its CalDAV path.
func (d *Destination) EnsureCalendar(ctx context.Context, displayName string) (string, error) {
	home, err := d.calendarHome(ctx)
	if err != nil {
		return "", err
	}
	return d.client.EnsureCalendar(ctx, home, displayName)
}

// CreateEvent writes a new calendar object and returns its href and ETag. The
// href is derived deterministically from the event UID so re-runs are stable.
func (d *Destination) CreateEvent(ctx context.Context, calRef string, ev feed.Event, tz []*ics.VTimezone) (string, string, error) {
	href := strings.TrimRight(calRef, "/") + "/" + objectName(ev.UID) + ".ics"
	put, err := d.client.CreateObject(ctx, href, feed.RenderICS(ev, tz))
	if err != nil {
		return "", "", err
	}
	return href, put.ETag, nil
}

// UpdateEvent overwrites the object at ref and returns the new ETag.
func (d *Destination) UpdateEvent(ctx context.Context, calRef, ref string, ev feed.Event, tz []*ics.VTimezone) (string, error) {
	put, err := d.client.UpdateObject(ctx, ref, feed.RenderICS(ev, tz))
	if err != nil {
		return "", err
	}
	return put.ETag, nil
}

// DeleteEvent removes the object at ref.
func (d *Destination) DeleteEvent(ctx context.Context, calRef, ref string) error {
	return d.client.DeleteObject(ctx, ref)
}

// objectName derives a stable, path-safe filename stem from an ICS UID.
func objectName(uid string) string {
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:16])
}
