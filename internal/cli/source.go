package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mattsafaii/heyday/internal/config"
	"github.com/mattsafaii/heyday/internal/feed"
	"github.com/mattsafaii/heyday/internal/hey"
)

// sourceRouter is the engine's Fetcher: it reads each feed from its configured
// source kind — an ICS URL over HTTP, or HEY calendars through the hey CLI.
type sourceRouter struct {
	ics *feed.Fetcher
	hey *hey.Source
}

func newSourceRouter() *sourceRouter {
	return &sourceRouter{ics: &feed.Fetcher{}, hey: &hey.Source{}}
}

func (r *sourceRouter) Fetch(ctx context.Context, f config.Feed, start, end time.Time) (*feed.Parsed, error) {
	switch f.Source {
	case config.SourceICS:
		return r.ics.Fetch(ctx, f.SourceURL)
	case config.SourceHEY:
		return r.hey.Fetch(ctx, f.HEYCalendarID, start, end)
	default:
		return nil, fmt.Errorf("unknown source %q", f.Source)
	}
}
