package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/mattsafaii/calmirror/internal/feed"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
)

// calendarMarker tags the dedicated calendars CalMirror creates so EnsureCalendar
// only ever adopts its own calendars — never a user's hand-made calendar that
// happens to share the display name. It also reads as a warning in the Google UI.
const calendarMarker = "Managed by CalMirror — events here are mirrored from a source feed and will be overwritten."

// Destination implements engine.Destination against Google Calendar. The calRef
// it returns from EnsureCalendar is a Google calendar id; the per-event ref is a
// Google event id.
type Destination struct {
	svc *calendar.Service
}

// NewDestination wraps an authenticated Calendar service as an engine destination.
func NewDestination(svc *calendar.Service) *Destination {
	return &Destination{svc: svc}
}

// EnsureCalendar finds the dedicated, CalMirror-owned mirror calendar named
// displayName, creating it if absent, and returns its id. It matches only
// calendars CalMirror itself created (owner access + our marker description), so
// it never writes into a user's existing calendar of the same name.
func (d *Destination) EnsureCalendar(ctx context.Context, displayName string) (string, error) {
	call := d.svc.CalendarList.List().ShowHidden(true)
	for {
		page, err := call.Context(ctx).Do()
		if err != nil {
			return "", fmt.Errorf("google: list calendars: %w", err)
		}
		for _, item := range page.Items {
			if item.Summary == displayName && item.AccessRole == "owner" && item.Description == calendarMarker {
				return item.Id, nil
			}
		}
		if page.NextPageToken == "" {
			break
		}
		call = call.PageToken(page.NextPageToken)
	}

	created, err := d.svc.Calendars.Insert(&calendar.Calendar{
		Summary:     displayName,
		Description: calendarMarker,
	}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("google: create calendar %q: %w", displayName, err)
	}
	return created.Id, nil
}

// CreateEvent inserts a native event and returns its id and ETag.
func (d *Destination) CreateEvent(ctx context.Context, calRef string, ev feed.Event, tz []*ics.VTimezone) (string, string, error) {
	created, err := d.svc.Events.Insert(calRef, toGoogleEvent(ev)).Context(ctx).Do()
	if err != nil {
		return "", "", err
	}
	return created.Id, created.Etag, nil
}

// UpdateEvent overwrites the event at ref and returns the new ETag.
func (d *Destination) UpdateEvent(ctx context.Context, calRef, ref string, ev feed.Event, tz []*ics.VTimezone) (string, error) {
	updated, err := d.svc.Events.Update(calRef, ref, toGoogleEvent(ev)).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return updated.Etag, nil
}

// DeleteEvent removes the event at ref. An already-gone event (404/410) is
// treated as success, so deletion is idempotent.
func (d *Destination) DeleteEvent(ctx context.Context, calRef, ref string) error {
	err := d.svc.Events.Delete(calRef, ref).Context(ctx).Do()
	if err == nil {
		return nil
	}
	var ae *googleapi.Error
	if errors.As(err, &ae) && (ae.Code == http.StatusNotFound || ae.Code == http.StatusGone) {
		return nil
	}
	return err
}

// toGoogleEvent maps a normalized source event to the Google Calendar event
// model, carrying conferencing URL, location, notes, and recurrence so meeting
// tools (Granola, Zoom) can attach.
func toGoogleEvent(ev feed.Event) *calendar.Event {
	ge := &calendar.Event{
		Summary:     ev.Summary,
		Location:    ev.Location,
		Description: ev.Description,
		Status:      mapStatus(ev.Status),
		Recurrence:  recurrence(ev),
	}

	// Conferencing fidelity is the crux: third-party meeting URLs (HEY/Zoom)
	// can't be set as native Google conferenceData, so carry the URL where
	// Granola will find it — the location field and a dedicated description
	// line. Both are scanned by meeting tools.
	if mu := meetingURL(ev); mu != "" {
		if strings.TrimSpace(ge.Location) == "" {
			ge.Location = mu
		}
		line := "Join: " + mu
		if !strings.Contains(ge.Description, mu) {
			if strings.TrimSpace(ge.Description) == "" {
				ge.Description = line
			} else {
				ge.Description = line + "\n\n" + ge.Description
			}
		}
	}

	if ev.AllDay {
		end := ev.End
		if end.IsZero() {
			end = ev.Start.AddDate(0, 0, 1)
		}
		ge.Start = &calendar.EventDateTime{Date: ev.Start.Format("2006-01-02")}
		ge.End = &calendar.EventDateTime{Date: end.Format("2006-01-02")}
	} else {
		end := ev.End
		if end.IsZero() {
			end = ev.Start.Add(time.Hour) // Google requires an end; default 1h
		}
		ge.Start = dateTime(ev.Start)
		ge.End = dateTime(end)
	}
	return ge
}

// dateTime renders a timed event boundary. The RFC3339 value already carries
// the UTC offset; the IANA TimeZone name (when known) is what Google needs to
// expand recurrence correctly across DST.
func dateTime(t time.Time) *calendar.EventDateTime {
	edt := &calendar.EventDateTime{DateTime: t.Format(time.RFC3339)}
	if name := t.Location().String(); name != "" && name != "Local" {
		edt.TimeZone = name
	}
	return edt
}

// mapStatus maps an ICS STATUS to a Google event status. An unknown or empty
// status maps to "confirmed", Google's default.
func mapStatus(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TENTATIVE":
		return "tentative"
	case "CANCELLED":
		return "cancelled"
	default:
		return "confirmed"
	}
}

// recurrence builds Google's recurrence lines from the source rules. EXRULE is
// deprecated and unsupported by Google, so it is dropped (a known edge).
func recurrence(ev feed.Event) []string {
	var r []string
	if ev.RRule != "" {
		r = append(r, "RRULE:"+ev.RRule)
	}
	for _, rd := range ev.RDates {
		r = append(r, "RDATE:"+rd)
	}
	for _, ex := range ev.ExDate {
		r = append(r, "EXDATE:"+ex)
	}
	return r
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

// meetingURL extracts the best conferencing/meeting URL for an event, checking
// the URL and CONFERENCE properties first, then common conferencing extensions,
// then any URL embedded in the location or description.
func meetingURL(ev feed.Event) string {
	if isURL(ev.URL) {
		return ev.URL
	}
	if ev.Raw != nil {
		for _, name := range []string{"CONFERENCE", "X-GOOGLE-CONFERENCE", "X-MICROSOFT-SKYPETEAMSMEETINGURL"} {
			if p := ev.Raw.GetProperty(ics.ComponentProperty(name)); p != nil && isURL(p.Value) {
				return p.Value
			}
		}
	}
	if u := urlRe.FindString(ev.Location); u != "" {
		return u
	}
	if u := urlRe.FindString(ev.Description); u != "" {
		return u
	}
	return ""
}

// isURL reports whether s is a non-empty http(s) URL.
func isURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
