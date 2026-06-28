package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/mattsafaii/calmirror/internal/caldav"
	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/engine"
	"github.com/mattsafaii/calmirror/internal/google"
	"github.com/mattsafaii/calmirror/internal/keychain"
)

// destinationFactory returns an engine.DestinationFor that routes each feed to
// its configured backend. Destinations are built lazily and reused across feeds
// of the same kind, so iCloud discovery and the Google token exchange happen at
// most once per pass. A construction failure (missing secret, revoked token) is
// returned to the engine as a per-feed error, isolating it from other feeds.
func destinationFactory(cfg *config.Config) engine.DestinationFor {
	var (
		icloud *caldav.Destination
		goog   *google.Destination
	)
	return func(ctx context.Context, f config.Feed) (engine.Destination, error) {
		switch f.Kind {
		case config.KindICloud:
			if icloud == nil {
				pw, err := keychain.Get(cfg.ICloud.Username)
				if errors.Is(err, keychain.ErrNotFound) {
					return nil, fmt.Errorf("no Keychain password for %q; run `calmirror setup`", cfg.ICloud.Username)
				}
				if err != nil {
					return nil, err
				}
				client, err := caldav.New(cfg.ICloud.Username, pw)
				if err != nil {
					return nil, err
				}
				icloud = caldav.NewDestination(client)
			}
			return icloud, nil
		case config.KindGoogle:
			if goog == nil {
				svc, err := google.NewService(ctx, cfg.Google.Account, cfg.Google.ClientID)
				if err != nil {
					return nil, err
				}
				goog = google.NewDestination(svc)
			}
			return goog, nil
		default:
			return nil, fmt.Errorf("feed %q: unknown destination kind %q", f.Name, f.Kind)
		}
	}
}
