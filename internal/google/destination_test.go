package google

import (
	"strings"
	"testing"

	"github.com/mattsafaii/calmirror/internal/feed"
)

func parseOne(t *testing.T, vevent string) feed.Event {
	t.Helper()
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\n" + vevent + "END:VCALENDAR\r\n"
	p, err := feed.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(p.Events))
	}
	return p.Events[0]
}

func TestToGoogleEventTimedWithConferencing(t *testing.T) {
	ev := parseOne(t, "BEGIN:VEVENT\r\nUID:a@x\r\nSUMMARY:Standup\r\n"+
		"DESCRIPTION:Daily sync\r\nURL:https://meet.example.com/abc\r\n"+
		"DTSTART:20260615T170000Z\r\nDTEND:20260615T173000Z\r\nEND:VEVENT\r\n")

	ge := toGoogleEvent(ev)
	if ge.Summary != "Standup" {
		t.Errorf("summary = %q", ge.Summary)
	}
	if ge.Start.DateTime == "" || ge.End.DateTime == "" {
		t.Errorf("timed event must use DateTime, got %+v / %+v", ge.Start, ge.End)
	}
	if ge.Start.Date != "" {
		t.Errorf("timed event must not set all-day Date")
	}
	if ge.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", ge.Status)
	}
	// Conferencing URL must land where Granola looks: location + description.
	if ge.Location != "https://meet.example.com/abc" {
		t.Errorf("location = %q, want meeting URL", ge.Location)
	}
	if !strings.Contains(ge.Description, "https://meet.example.com/abc") {
		t.Errorf("description missing meeting URL: %q", ge.Description)
	}
	if !strings.Contains(ge.Description, "Daily sync") {
		t.Errorf("description lost original notes: %q", ge.Description)
	}
}

func TestToGoogleEventAllDay(t *testing.T) {
	ev := parseOne(t, "BEGIN:VEVENT\r\nUID:b@x\r\nSUMMARY:Holiday\r\n"+
		"DTSTART;VALUE=DATE:20260704\r\nDTEND;VALUE=DATE:20260705\r\nEND:VEVENT\r\n")
	ge := toGoogleEvent(ev)
	if ge.Start.Date != "2026-07-04" || ge.End.Date != "2026-07-05" {
		t.Errorf("all-day dates = %q/%q", ge.Start.Date, ge.End.Date)
	}
	if ge.Start.DateTime != "" {
		t.Errorf("all-day event must not set DateTime")
	}
}

func TestToGoogleEventRecurrence(t *testing.T) {
	ev := parseOne(t, "BEGIN:VEVENT\r\nUID:c@x\r\nSUMMARY:Weekly\r\n"+
		"DTSTART:20260615T170000Z\r\nDTEND:20260615T173000Z\r\n"+
		"RRULE:FREQ=WEEKLY;BYDAY=MO\r\nEXDATE:20260622T170000Z\r\nEND:VEVENT\r\n")
	ge := toGoogleEvent(ev)
	want := map[string]bool{"RRULE:FREQ=WEEKLY;BYDAY=MO": true, "EXDATE:20260622T170000Z": true}
	if len(ge.Recurrence) != len(want) {
		t.Fatalf("recurrence = %v", ge.Recurrence)
	}
	for _, r := range ge.Recurrence {
		if !want[r] {
			t.Errorf("unexpected recurrence line %q", r)
		}
	}
}

func TestMeetingURLFromLocation(t *testing.T) {
	ev := parseOne(t, "BEGIN:VEVENT\r\nUID:d@x\r\nSUMMARY:Call\r\n"+
		"LOCATION:Join at https://zoom.us/j/123 now\r\n"+
		"DTSTART:20260615T170000Z\r\nDTEND:20260615T173000Z\r\nEND:VEVENT\r\n")
	if got := meetingURL(ev); got != "https://zoom.us/j/123" {
		t.Errorf("meetingURL = %q", got)
	}
}

func TestMapStatus(t *testing.T) {
	cases := map[string]string{"": "confirmed", "CONFIRMED": "confirmed", "tentative": "tentative", "CANCELLED": "cancelled", "weird": "confirmed"}
	for in, want := range cases {
		if got := mapStatus(in); got != want {
			t.Errorf("mapStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
