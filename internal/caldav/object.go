package caldav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PutResult reports the outcome of writing a calendar object.
type PutResult struct {
	// ETag is the server-assigned entity tag, if the response provided one.
	// iCloud does not always return it on PUT; an empty value is not an error.
	ETag string
}

// CreateObject writes a new calendar object at path (relative to the endpoint).
// It uses If-None-Match: * so it never silently overwrites an existing
// resource. Returns the new ETag if the server provides one.
func (c *Client) CreateObject(ctx context.Context, path, icsData string) (PutResult, error) {
	return c.put(ctx, path, icsData, map[string]string{"If-None-Match": "*"})
}

// UpdateObject overwrites the calendar object at path. CalMirror owns the
// destination calendar, so it overwrites unconditionally rather than risk
// 412 churn from a stale ETag.
func (c *Client) UpdateObject(ctx context.Context, path, icsData string) (PutResult, error) {
	return c.put(ctx, path, icsData, nil)
}

func (c *Client) put(ctx context.Context, path, icsData string, headers map[string]string) (PutResult, error) {
	u, err := c.resolveURL(path)
	if err != nil {
		return PutResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), strings.NewReader(icsData))
	if err != nil {
		return PutResult{}, err
	}
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return PutResult{}, fmt.Errorf("caldav: put %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return PutResult{}, fmt.Errorf("caldav: put %s: %s: %s", path, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return PutResult{ETag: strings.Trim(resp.Header.Get("ETag"), `"`)}, nil
}

// DeleteObject removes the calendar object at path. A 404 is treated as success
// (the resource is already gone), so deletion is idempotent.
func (c *Client) DeleteObject(ctx context.Context, path string) error {
	u, err := c.resolveURL(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("caldav: delete %s: %w", path, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("caldav: delete %s: %s: %s", path, resp.Status, strings.TrimSpace(string(snippet)))
	}
}
