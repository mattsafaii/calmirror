// Package feed fetches an ICS calendar feed over HTTP and parses it into
// normalized events. It preserves the full source VEVENT alongside the
// extracted fields so the mirror engine can write full-fidelity native events
// (conferencing URL, location, notes, recurrence) into the destination.
package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	ics "github.com/arran4/golang-ical"
)

// userAgent identifies CalMirror to feed hosts.
const userAgent = "CalMirror/0.1 (+https://github.com/mattsafaii/calmirror)"

// Event is a normalized calendar event extracted from a source VEVENT. The
// extracted fields drive sync-window filtering, change detection, and status
// display; Raw retains the original component for full-fidelity serialization
// into the destination calendar.
type Event struct {
	UID          string
	Summary      string
	Description  string
	Location     string
	URL          string
	Start        time.Time
	End          time.Time
	AllDay       bool
	Status       string
	Sequence     int
	RecurrenceID string // set when this is an override of a recurrence instance
	LastModified time.Time

	// Recurrence rules, kept as raw property values for passthrough fidelity.
	RRule  string
	RDates []string
	ExDate []string
	ExRule []string

	// Raw is the source component, used to write a faithful copy downstream.
	Raw *ics.VEvent
}

// Parsed is the result of parsing an ICS feed: its events plus the source
// VTIMEZONE components, which the mirror engine carries into each destination
// object so TZID references resolve.
type Parsed struct {
	Events    []Event
	Timezones []*ics.VTimezone
}

// HTTPError reports a non-success HTTP status from a feed fetch. The mirror
// engine surfaces these (e.g. an expired-token 404) without corrupting other
// feeds.
type HTTPError struct {
	URL        string
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fetch %s: %s", e.URL, e.Status)
}

// Fetcher retrieves and parses ICS feeds. The zero value is usable and uses a
// default HTTP client with a sane timeout.
type Fetcher struct {
	// Client is the HTTP client used for fetches. If nil, a default client
	// with a 30s timeout is used.
	Client *http.Client
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Fetch retrieves the feed at url and parses it. A non-2xx response yields an
// *HTTPError.
func (f *Fetcher) Fetch(ctx context.Context, url string) (*Parsed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/calendar, */*")

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain a little so the connection can be reused, then report.
		io.CopyN(io.Discard, resp.Body, 4<<10)
		return nil, &HTTPError{URL: url, StatusCode: resp.StatusCode, Status: resp.Status}
	}

	return Parse(resp.Body)
}

// Parse reads an ICS document into normalized events and the source timezones.
func Parse(r io.Reader) (*Parsed, error) {
	cal, err := ics.ParseCalendar(r)
	if err != nil {
		return nil, fmt.Errorf("parse ICS: %w", err)
	}
	vevents := cal.Events()
	p := &Parsed{
		Events:    make([]Event, 0, len(vevents)),
		Timezones: cal.Timezones(),
	}
	for _, ve := range vevents {
		p.Events = append(p.Events, normalize(ve))
	}
	return p, nil
}

func normalize(ve *ics.VEvent) Event {
	e := Event{
		UID:          prop(ve, ics.ComponentPropertyUniqueId),
		Summary:      prop(ve, ics.ComponentPropertySummary),
		Description:  prop(ve, ics.ComponentPropertyDescription),
		Location:     prop(ve, ics.ComponentPropertyLocation),
		URL:          prop(ve, ics.ComponentPropertyUrl),
		Status:       prop(ve, ics.ComponentPropertyStatus),
		RecurrenceID: prop(ve, ics.ComponentPropertyRecurrenceId),
		RRule:        prop(ve, ics.ComponentPropertyRrule),
		RDates:       props(ve, ics.ComponentPropertyRDate),
		ExDate:       props(ve, ics.ComponentPropertyExdate),
		ExRule:       props(ve, ics.ComponentPropertyExrule),
		Raw:          ve,
	}

	if s := prop(ve, ics.ComponentPropertySequence); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			e.Sequence = n
		}
	}
	if t, err := ve.GetLastModifiedAt(); err == nil {
		e.LastModified = t
	}

	e.AllDay = isAllDay(ve)
	if e.AllDay {
		if t, err := ve.GetAllDayStartAt(); err == nil {
			e.Start = t
		}
		if t, err := ve.GetAllDayEndAt(); err == nil {
			e.End = t
		}
	} else {
		if t, err := ve.GetStartAt(); err == nil {
			e.Start = t
		}
		if t, err := ve.GetEndAt(); err == nil {
			e.End = t
		}
	}
	return e
}

// isAllDay reports whether DTSTART carries VALUE=DATE (a date without a time).
func isAllDay(ve *ics.VEvent) bool {
	p := ve.GetProperty(ics.ComponentPropertyDtStart)
	if p == nil {
		return false
	}
	for _, v := range p.ICalParameters[string(ics.ParameterValue)] {
		if v == string(ics.ValueDataTypeDate) {
			return true
		}
	}
	return false
}

// prop returns the first value of a property, or "" if absent.
func prop(ve *ics.VEvent, p ics.ComponentProperty) string {
	if got := ve.GetProperty(p); got != nil {
		return got.Value
	}
	return ""
}

// props returns all values of a (possibly repeated) property.
func props(ve *ics.VEvent, p ics.ComponentProperty) []string {
	got := ve.GetProperties(p)
	if len(got) == 0 {
		return nil
	}
	out := make([]string, 0, len(got))
	for _, g := range got {
		out = append(out, g.Value)
	}
	return out
}
