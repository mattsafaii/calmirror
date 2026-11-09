package hey

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/mattsafaii/calmirror/internal/feed"
)

// Recording is the part of a HEY calendar event (`hey event week --json`) that
// CalMirror mirrors. Field names follow the hey SDK's generated.Recording.
type Recording struct {
	ID               int64     `json:"id"`
	Type             string    `json:"type"`
	Title            string    `json:"title"`
	Description      string    `json:"description"` // notes, as plain text
	Notes            string    `json:"notes"`
	Location         string    `json:"location"`
	URL              string    `json:"url"`
	JoinLink         JoinLink  `json:"join_link"`
	AllDay           bool      `json:"all_day"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	StartsAtTimeZone string    `json:"starts_at_time_zone"`
	EndsAtTimeZone   string    `json:"ends_at_time_zone"`
	UpdatedAt        time.Time `json:"updated_at"`
	OccurrenceID     string    `json:"occurrence_id"` // set on each day of a repeating series
	ParentID         int64     `json:"parent_id"`     // the series, on an occurrence
	Calendar         struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"calendar"`
}

// JoinLink is a video/meeting join link on a HEY event.
type JoinLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Source reads HEY calendar events through the hey CLI, expanded into
// individual occurrences. HEY's CLI exposes only a preset repeat kind (no end
// date or count, custom schedules opaque), so rather than rebuild a lossy
// RRULE, CalMirror mirrors each occurrence inside the sync window as its own
// event. Single-day edits and cancellations then come through exactly.
type Source struct {
	Client *Client
}

func (s *Source) client() *Client {
	if s.Client != nil {
		return s.Client
	}
	return &Client{}
}

// Fetch returns the events of calendarID (0 = every calendar switched on in
// HEY) that overlap [start, end]. HEY's Week view is the only expanding read
// the CLI has, so it reads one week at a time; any date names its week, so
// stepping a week at a time from start covers the span. end must be set: an
// expanded read needs a bound.
//
// Every read must succeed. A partial result would look like deleted events and
// remove mirror events that still exist, so any failure fails the whole fetch.
func (s *Source) Fetch(ctx context.Context, calendarID int64, start, end time.Time) (*feed.Parsed, error) {
	if end.IsZero() || end.Before(start) {
		return nil, fmt.Errorf("hey: a bounded sync window is required (got %s to %s)", start.Format(dateLayout), end.Format(dateLayout))
	}

	byUID := make(map[string]feed.Event)
	for day := start; ; day = day.AddDate(0, 0, 7) {
		if day.After(end) {
			day = end // read end's own week too, in case stepping skipped past it
		}
		var recs []Recording
		if err := s.client().JSON(ctx, &recs, "event", "week", day.Format(dateLayout), "--all"); err != nil {
			return nil, err
		}
		for _, r := range recs {
			if !isEvent(r.Type) {
				continue
			}
			if r.Calendar.ID == 0 {
				return nil, fmt.Errorf("hey: event %d came back without a calendar; refusing to sync so nothing is deleted by mistake", r.ID)
			}
			if calendarID != 0 && r.Calendar.ID != calendarID {
				continue
			}
			ev := ToEvent(r)
			if ev.End.Before(start) || ev.Start.After(end) {
				continue
			}
			byUID[ev.UID] = ev
		}
		if !day.Before(end) {
			break
		}
	}

	events := make([]feed.Event, 0, len(byUID))
	for _, ev := range byUID {
		events = append(events, ev)
	}
	sort.Slice(events, func(i, j int) bool {
		if !events[i].Start.Equal(events[j].Start) {
			return events[i].Start.Before(events[j].Start)
		}
		return events[i].UID < events[j].UID
	})
	return &feed.Parsed{Events: events}, nil
}

const dateLayout = "2006-01-02"

// isEvent keeps calendar events and drops the other recordings HEY can put in
// a period (habits, to-dos, countdowns). The CLI already filters to events; an
// empty type is accepted for that reason.
func isEvent(t string) bool {
	switch t {
	case "", "Calendar::Event", "CalendarEvent":
		return true
	}
	return false
}

// UID is the stable mirror identity of a HEY event. An occurrence of a series
// keys on its occurrence_id ("<series>_<date>"), which stays the same whether
// HEY serves the day virtually or has written it out after a one-day edit; a
// one-off event keys on its id.
func UID(r Recording) string {
	key := r.OccurrenceID
	if key == "" {
		key = strconv.FormatInt(r.ID, 10)
	}
	return "hey-" + key + "@calmirror"
}

// ToEvent maps a HEY event to a normalized feed.Event, and builds the VEVENT
// the CalDAV destination writes from it. The meeting link goes in URL, where
// both destinations (and Granola) look first.
func ToEvent(r Recording) feed.Event {
	notes := r.Description
	if notes == "" {
		notes = r.Notes
	}
	link := r.JoinLink.URL
	if link == "" {
		link = r.URL
	} else if r.URL != "" && r.URL != link && !strings.Contains(notes, r.URL) {
		// Keep the event's own link when the join link took the URL slot.
		notes = strings.TrimSpace(notes + "\n\nLink: " + r.URL)
	}

	ev := feed.Event{
		UID:          UID(r),
		Summary:      r.Title,
		Description:  notes,
		Location:     r.Location,
		URL:          link,
		AllDay:       r.AllDay,
		Status:       "CONFIRMED",
		LastModified: r.UpdatedAt.UTC(),
	}
	if r.AllDay {
		// HEY stamps an all-day event's dates at UTC midnight with an
		// inclusive end; calendars want an exclusive end date.
		ev.Start = dateOnly(r.StartsAt)
		end := dateOnly(r.EndsAt)
		if end.Before(ev.Start) {
			end = ev.Start
		}
		ev.End = end.AddDate(0, 0, 1)
	} else {
		ev.Start = inZone(r.StartsAt, r.StartsAtTimeZone)
		ev.End = inZone(r.EndsAt, firstNonEmpty(r.EndsAtTimeZone, r.StartsAtTimeZone))
		if ev.End.Before(ev.Start) {
			ev.End = ev.Start
		}
	}
	ev.Raw = vevent(ev)
	return ev
}

// vevent renders the normalized event as an ICS VEVENT. Timed events are
// written in UTC, so no VTIMEZONE is needed.
func vevent(ev feed.Event) *ics.VEvent {
	ve := ics.NewEvent(ev.UID)
	ve.SetDtStampTime(ev.LastModified)
	if !ev.LastModified.IsZero() {
		ve.SetLastModifiedAt(ev.LastModified)
	}
	ve.SetSummary(ev.Summary)
	if ev.Description != "" {
		ve.SetDescription(ev.Description)
	}
	if ev.Location != "" {
		ve.SetLocation(ev.Location)
	}
	if ev.URL != "" {
		ve.SetURL(ev.URL)
	}
	ve.SetStatus(ics.ObjectStatusConfirmed)
	if ev.AllDay {
		ve.SetAllDayStartAt(ev.Start)
		ve.SetAllDayEndAt(ev.End)
	} else {
		ve.SetStartAt(ev.Start.UTC())
		ve.SetEndAt(ev.End.UTC())
	}
	return ve
}

func dateOnly(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// inZone expresses t in the named IANA zone when it loads, so Google gets the
// zone name it needs; otherwise t stays as HEY sent it (UTC).
func inZone(t time.Time, zone string) time.Time {
	if zone != "" {
		if loc, err := time.LoadLocation(zone); err == nil {
			return t.In(loc)
		}
	}
	return t.UTC()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
