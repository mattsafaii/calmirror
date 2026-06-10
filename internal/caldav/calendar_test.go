package caldav

import (
	"regexp"
	"strings"
	"testing"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUID(t *testing.T) {
	a, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID: %v", err)
	}
	if !uuidRE.MatchString(a) {
		t.Fatalf("not a v4 UUID: %q", a)
	}
	b, _ := newUUID()
	if a == b {
		t.Fatalf("UUIDs not unique: %q", a)
	}
}

func TestMkcalendarBodyEscapes(t *testing.T) {
	body, err := mkcalendarBody(`HEY <&> "synced"`)
	if err != nil {
		t.Fatalf("mkcalendarBody: %v", err)
	}
	s := string(body)
	if strings.Contains(s, "<&>") {
		t.Errorf("display name not XML-escaped: %s", s)
	}
	if !strings.Contains(s, `name="VEVENT"`) {
		t.Errorf("missing VEVENT component restriction: %s", s)
	}
}

func TestResolveURL(t *testing.T) {
	c := &Client{endpoint: ICloudEndpoint}
	cases := map[string]string{
		"/123/calendars/":                         "https://caldav.icloud.com/123/calendars/",
		"https://p01-caldav.icloud.com/123/cals/": "https://p01-caldav.icloud.com/123/cals/",
	}
	for ref, want := range cases {
		u, err := c.resolveURL(ref)
		if err != nil {
			t.Fatalf("resolveURL(%q): %v", ref, err)
		}
		if u.String() != want {
			t.Errorf("resolveURL(%q) = %q, want %q", ref, u.String(), want)
		}
	}
}
