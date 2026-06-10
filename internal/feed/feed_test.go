package feed

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//Test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:meeting-1@hey.com\r\n" +
	"SUMMARY:Weekly Sync\r\n" +
	"DESCRIPTION:Join here\\nhttps://zoom.us/j/123\r\n" +
	"LOCATION:https://zoom.us/j/123\r\n" +
	"URL:https://zoom.us/j/123\r\n" +
	"DTSTART:20260615T170000Z\r\n" +
	"DTEND:20260615T173000Z\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO\r\n" +
	"SEQUENCE:2\r\n" +
	"STATUS:CONFIRMED\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:holiday-1@hey.com\r\n" +
	"SUMMARY:Day Off\r\n" +
	"DTSTART;VALUE=DATE:20260704\r\n" +
	"DTEND;VALUE=DATE:20260705\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParse(t *testing.T) {
	parsed, err := Parse(strings.NewReader(sampleICS))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	events := parsed.Events
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	m := events[0]
	if m.UID != "meeting-1@hey.com" {
		t.Errorf("UID = %q", m.UID)
	}
	if m.Summary != "Weekly Sync" {
		t.Errorf("Summary = %q", m.Summary)
	}
	if m.URL != "https://zoom.us/j/123" {
		t.Errorf("URL = %q", m.URL)
	}
	if m.RRule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Errorf("RRule = %q", m.RRule)
	}
	if m.Sequence != 2 {
		t.Errorf("Sequence = %d, want 2", m.Sequence)
	}
	if m.AllDay {
		t.Errorf("timed event flagged all-day")
	}
	if m.Start.IsZero() || m.End.IsZero() {
		t.Errorf("start/end not parsed: %v / %v", m.Start, m.End)
	}

	h := events[1]
	if !h.AllDay {
		t.Errorf("date-only event not flagged all-day")
	}
	if h.Start.IsZero() {
		t.Errorf("all-day start not parsed")
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	var f Fetcher
	_, err := f.Fetch(t.Context(), srv.URL)
	herr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("got %T (%v), want *HTTPError", err, err)
	}
	if herr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", herr.StatusCode)
	}
}

func TestFetchOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(sampleICS))
	}))
	defer srv.Close()

	var f Fetcher
	parsed, err := f.Fetch(t.Context(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(parsed.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(parsed.Events))
	}
}
