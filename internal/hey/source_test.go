package hey

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mattsafaii/calmirror/internal/feed"
)

// weekRunner serves `hey event week <date>` from a map of date -> data JSON,
// recording the dates it was asked for.
type weekRunner struct {
	weeks map[string]string
	asked []string
	fail  map[string]bool
}

func (w *weekRunner) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if len(args) < 3 || args[0] != "event" || args[1] != "week" {
		return nil, nil, errors.New("unexpected command: " + strings.Join(args, " "))
	}
	date := args[2]
	w.asked = append(w.asked, date)
	if w.fail[date] {
		return shellExit(`{"ok":false,"error":"token refresh failed (status 429): rate_limit_exceeded","code":"api"}`, 7)
	}
	data, ok := w.weeks[date]
	if !ok {
		data = "[]"
	}
	return []byte(`{"ok":true,"data":` + data + `}`), nil, nil
}

// shellExit produces a real *exec.ExitError with the given stdout and status.
func shellExit(stdout string, code int) ([]byte, []byte, error) {
	cmd := exec.Command("/bin/sh", "-c", `printf '%s' "$0"; exit `+itoa(code), stdout)
	out, err := cmd.Output()
	return out, nil, err
}

func itoa(n int) string { return string(rune('0' + n)) }

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

const (
	standupMon = `{"id":500,"parent_id":500,"occurrence_id":"500_2026-11-09","type":"Calendar::Event","title":"Standup",
		"starts_at":"2026-11-09T17:00:00Z","ends_at":"2026-11-09T17:15:00Z","starts_at_time_zone":"America/Los_Angeles",
		"join_link":{"url":"https://zoom.us/j/1"},"url":"https://example.com/agenda","calendar":{"id":9,"name":"Work"}}`
	standupNext = `{"id":500,"parent_id":500,"occurrence_id":"500_2026-11-16","type":"Calendar::Event","title":"Standup",
		"starts_at":"2026-11-16T17:00:00Z","ends_at":"2026-11-16T17:15:00Z","starts_at_time_zone":"America/Los_Angeles",
		"join_link":{"url":"https://zoom.us/j/1"},"calendar":{"id":9,"name":"Work"}}`
	birthday = `{"id":700,"type":"Calendar::Event","title":"Birthday","all_day":true,
		"starts_at":"2026-11-12T00:00:00Z","ends_at":"2026-11-13T00:00:00Z","calendar":{"id":9,"name":"Work"}}`
	personal = `{"id":800,"type":"Calendar::Event","title":"Dentist",
		"starts_at":"2026-11-10T16:00:00Z","ends_at":"2026-11-10T17:00:00Z","calendar":{"id":4,"name":"Personal"}}`
)

func TestFetchExpandsWeeksAndFilters(t *testing.T) {
	w := &weekRunner{weeks: map[string]string{
		// Week reads overlap, so the same occurrence can come back twice.
		"2026-11-09": "[" + standupMon + "," + birthday + "," + personal + "]",
		"2026-11-16": "[" + standupMon + "," + standupNext + "]",
	}}
	src := &Source{Client: &Client{Run: w.run}}

	parsed, err := src.Fetch(context.Background(), 9, day("2026-11-09"), day("2026-11-20"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := strings.Join(w.asked, ","); got != "2026-11-09,2026-11-16,2026-11-20" {
		t.Errorf("weeks read = %s", got)
	}

	byUID := map[string]feed.Event{}
	for _, ev := range parsed.Events {
		byUID[ev.UID] = ev
	}
	if len(byUID) != 3 {
		t.Fatalf("events = %d (%v), want 2 standups + birthday; the other calendar is filtered out", len(byUID), keys(byUID))
	}

	mon := byUID["hey-500_2026-11-09@calmirror"]
	if mon.Summary != "Standup" || mon.URL != "https://zoom.us/j/1" {
		t.Errorf("standup = %+v; want the join link as URL", mon)
	}
	if !strings.Contains(mon.Description, "Link: https://example.com/agenda") {
		t.Errorf("event link should be kept in the notes when the join link takes URL: %q", mon.Description)
	}
	if mon.Start.Location().String() != "America/Los_Angeles" || mon.Start.Hour() != 9 {
		t.Errorf("start = %v, want 09:00 in America/Los_Angeles", mon.Start)
	}
	if mon.Raw == nil {
		t.Fatal("Raw VEVENT missing; the CalDAV destination writes from it")
	}

	bday := byUID["hey-700@calmirror"]
	if !bday.AllDay || !bday.Start.Equal(day("2026-11-12")) || !bday.End.Equal(day("2026-11-14")) {
		t.Errorf("all-day = %v..%v, want 2026-11-12..2026-11-14 (HEY's inclusive end made exclusive)", bday.Start, bday.End)
	}
	ics := feed.RenderICS(bday, nil)
	if !strings.Contains(ics, "DTSTART;VALUE=DATE:20261112") || !strings.Contains(ics, "DTEND;VALUE=DATE:20261114") {
		t.Errorf("all-day ICS dates wrong:\n%s", ics)
	}
}

func TestFetchAllCalendars(t *testing.T) {
	w := &weekRunner{weeks: map[string]string{"2026-11-09": "[" + birthday + "," + personal + "]"}}
	parsed, err := (&Source{Client: &Client{Run: w.run}}).Fetch(context.Background(), 0, day("2026-11-09"), day("2026-11-12"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(parsed.Events) != 2 {
		t.Fatalf("events = %d, want both calendars", len(parsed.Events))
	}
}

// One failed week must fail the whole fetch: a partial read would look like
// deleted events and remove mirror events that still exist.
func TestFetchFailsWholeOnAnyWeekError(t *testing.T) {
	w := &weekRunner{
		weeks: map[string]string{"2026-11-09": "[" + birthday + "]"},
		fail:  map[string]bool{"2026-11-16": true},
	}
	_, err := (&Source{Client: &Client{Run: w.run}}).Fetch(context.Background(), 0, day("2026-11-09"), day("2026-11-20"))
	var he *Error
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *hey.Error", err)
	}
	if he.ExitCode != 7 || !he.Auth() || !he.RateLimited() {
		t.Errorf("error = %+v; want exit 7 read as a sign-in / rate-limit failure", he)
	}
	if !strings.Contains(he.Error(), "token refresh failed") {
		t.Errorf("message lost: %v", he)
	}
}

func TestFetchRefusesEventWithoutCalendar(t *testing.T) {
	w := &weekRunner{weeks: map[string]string{"2026-11-09": `[{"id":1,"title":"x","starts_at":"2026-11-09T10:00:00Z","ends_at":"2026-11-09T11:00:00Z"}]`}}
	if _, err := (&Source{Client: &Client{Run: w.run}}).Fetch(context.Background(), 9, day("2026-11-09"), day("2026-11-10")); err == nil {
		t.Fatal("expected an error for an event with no calendar")
	}
}

func TestFetchNeedsBoundedWindow(t *testing.T) {
	w := &weekRunner{}
	if _, err := (&Source{Client: &Client{Run: w.run}}).Fetch(context.Background(), 0, day("2026-11-09"), time.Time{}); err == nil {
		t.Fatal("expected an error for an unbounded window")
	}
	if len(w.asked) != 0 {
		t.Fatalf("read HEY anyway: %v", w.asked)
	}
}

func TestUIDStableAcrossRealizedOccurrence(t *testing.T) {
	virtual := Recording{ID: 500, ParentID: 500, OccurrenceID: "500_2026-11-09"}
	realized := Recording{ID: 9001, ParentID: 500, OccurrenceID: "500_2026-11-09"}
	if UID(virtual) != UID(realized) {
		t.Fatalf("UID changed when HEY wrote the day out: %s vs %s", UID(virtual), UID(realized))
	}
}

func TestCalendars(t *testing.T) {
	c := &Client{Run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		if strings.Join(args, " ") != "calendar list --json" {
			t.Errorf("args = %v", args)
		}
		return []byte(`{"ok":true,"data":[{"id":9,"name":"Work","kind":"owned","owned":true}]}`), nil, nil
	}}
	cals, err := c.Calendars(context.Background())
	if err != nil {
		t.Fatalf("Calendars: %v", err)
	}
	if len(cals) != 1 || cals[0].ID != 9 || cals[0].Name != "Work" {
		t.Fatalf("calendars = %+v", cals)
	}
}

func keys(m map[string]feed.Event) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
