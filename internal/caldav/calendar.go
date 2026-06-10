package caldav

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// EnsureCalendar returns the path of the dedicated mirror calendar named
// displayName within the calendar-home-set, creating it if it does not yet
// exist. Re-runs reuse the same calendar, so CalMirror never touches the
// user's hand-made calendars.
func (c *Client) EnsureCalendar(ctx context.Context, calendarHome, displayName string) (string, error) {
	if path, err := c.findCalendarByName(ctx, calendarHome, displayName); err != nil {
		return "", err
	} else if path != "" {
		return path, nil
	}

	if err := c.mkcalendar(ctx, calendarHome, displayName); err != nil {
		return "", err
	}

	// Re-discover so we return the canonical path the library will use for
	// subsequent event operations against this calendar.
	path, err := c.findCalendarByName(ctx, calendarHome, displayName)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("caldav: created calendar %q not found on re-discovery", displayName)
	}
	return path, nil
}

func (c *Client) findCalendarByName(ctx context.Context, calendarHome, displayName string) (string, error) {
	cals, err := c.FindCalendars(ctx, calendarHome)
	if err != nil {
		return "", err
	}
	for _, cal := range cals {
		if cal.Name == displayName {
			return cal.Path, nil
		}
	}
	return "", nil
}

// mkcalendar issues an RFC 4791 MKCALENDAR request for a new VEVENT calendar
// under calendarHome. The collection name is a random UUID so it never
// collides with an existing resource.
func (c *Client) mkcalendar(ctx context.Context, calendarHome, displayName string) error {
	seg, err := newUUID()
	if err != nil {
		return err
	}
	u, err := c.resolveURL(calendarHome)
	if err != nil {
		return err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + seg + "/"

	body, err := mkcalendarBody(displayName)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "MKCALENDAR", u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("caldav: mkcalendar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("caldav: mkcalendar %q: %s: %s",
			displayName, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// mkcalendarBody builds the MKCALENDAR request XML, restricting the calendar to
// VEVENT components.
func mkcalendarBody(displayName string) ([]byte, error) {
	var name bytes.Buffer
	if err := xml.EscapeText(&name, []byte(displayName)); err != nil {
		return nil, err
	}
	const tmpl = `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set>
    <D:prop>
      <D:displayname>%s</D:displayname>
      <C:supported-calendar-component-set>
        <C:comp name="VEVENT"/>
      </C:supported-calendar-component-set>
    </D:prop>
  </D:set>
</C:mkcalendar>`
	return []byte(fmt.Sprintf(tmpl, name.String())), nil
}

// resolveURL turns a discovered path or absolute href into a full URL against
// the client endpoint, accommodating iCloud returning either form.
func (c *Client) resolveURL(ref string) (*url.URL, error) {
	base, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(r), nil
}

// newUUID returns a random RFC 4122 version-4 UUID string.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
