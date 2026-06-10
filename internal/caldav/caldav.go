// Package caldav is CalMirror's iCloud CalDAV client: it authenticates with an
// app-specific password (read from the Keychain by the caller), discovers the
// account's principal and calendar-home-set, and manages the dedicated mirror
// calendars and their events.
package caldav

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

// ICloudEndpoint is the iCloud CalDAV entry point. Discovery walks from here to
// the per-account principal and calendar-home-set.
const ICloudEndpoint = "https://caldav.icloud.com"

// Client wraps the go-webdav CalDAV client with iCloud-specific discovery and
// the helpers CalMirror needs. Construct it with New.
type Client struct {
	dav        *caldav.Client
	httpClient webdav.HTTPClient
	endpoint   string
}

// New builds a CalDAV client for the iCloud account identified by username,
// authenticating with the given app-specific password over HTTP Basic.
func New(username, password string) (*Client, error) {
	return newWithEndpoint(ICloudEndpoint, username, password)
}

func newWithEndpoint(endpoint, username, password string) (*Client, error) {
	base := &http.Client{Timeout: 60 * time.Second}
	hc := webdav.HTTPClientWithBasicAuth(base, username, password)
	dav, err := caldav.NewClient(hc, endpoint)
	if err != nil {
		return nil, fmt.Errorf("caldav: new client: %w", err)
	}
	return &Client{dav: dav, httpClient: hc, endpoint: endpoint}, nil
}

// Discovery is the result of walking from the endpoint to the account's
// calendar storage.
type Discovery struct {
	Principal    string // current-user-principal path
	CalendarHome string // calendar-home-set path
}

// Discover resolves the account's principal and calendar-home-set. This is the
// first real round-trip against iCloud and validates the credentials: bad
// credentials surface here as an authentication error.
func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	principal, err := c.dav.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return Discovery{}, fmt.Errorf("caldav: find principal: %w", err)
	}
	home, err := c.dav.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return Discovery{}, fmt.Errorf("caldav: find calendar-home-set: %w", err)
	}
	return Discovery{Principal: principal, CalendarHome: home}, nil
}

// FindCalendars lists the calendars in the given calendar-home-set.
func (c *Client) FindCalendars(ctx context.Context, calendarHome string) ([]caldav.Calendar, error) {
	cals, err := c.dav.FindCalendars(ctx, calendarHome)
	if err != nil {
		return nil, fmt.Errorf("caldav: find calendars: %w", err)
	}
	return cals, nil
}
