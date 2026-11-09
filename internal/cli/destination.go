package cli

import (
	"context"

	"github.com/mattsafaii/heyday/internal/config"
	"github.com/mattsafaii/heyday/internal/engine"
	"github.com/mattsafaii/heyday/internal/google"
)

// destinationFactory returns an engine.DestinationFor that connects every feed
// to the configured Google account. The Calendar service is built lazily, once
// per pass, so the token exchange happens at most once. A failure (missing
// secret, revoked token, deleted OAuth client) is returned to the engine as a
// per-feed error and recorded in status.
func destinationFactory(cfg *config.Config) engine.DestinationFor {
	var dest *google.Destination
	return func(ctx context.Context, f config.Feed) (engine.Destination, error) {
		if dest == nil {
			svc, err := google.NewService(ctx, cfg.Google.Account, cfg.Google.ClientID)
			if err != nil {
				return nil, err
			}
			dest = google.NewDestination(svc)
		}
		return dest, nil
	}
}
