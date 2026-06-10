package feed

import ics "github.com/arran4/golang-ical"

// RenderICS builds a standalone VCALENDAR document for one event, suitable for
// PUTting as a CalDAV calendar object. It carries the source VTIMEZONE
// components so any TZID the event references resolves, and preserves the full
// source VEVENT (conferencing URL, location, notes, recurrence, alarms) for a
// full-fidelity native event. Output uses CRLF line endings per RFC 5545.
func RenderICS(e Event, timezones []*ics.VTimezone) string {
	cal := ics.NewCalendar()
	cal.SetProductId("-//CalMirror//CalMirror 0.1//EN")
	for _, tz := range timezones {
		cal.AddVTimezone(tz)
	}
	cal.AddVEvent(e.Raw)
	return cal.Serialize(ics.WithNewLineWindows)
}
