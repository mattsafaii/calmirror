package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/mattsafaii/calmirror/internal/config"
	"github.com/mattsafaii/calmirror/internal/engine"
	"github.com/mattsafaii/calmirror/internal/feed"
	"github.com/mattsafaii/calmirror/internal/notify"
	"github.com/mattsafaii/calmirror/internal/store"
)

func cmdSync(args []string) int {
	cfg, err := config.Load()
	if errors.Is(err, fs.ErrNotExist) {
		return fail("not set up yet; run `calmirror setup` first")
	}
	if err != nil {
		return fail("load config: %v", err)
	}
	if len(cfg.Feeds) == 0 {
		fmt.Println("No feeds configured. Run `calmirror setup` to add one.")
		return 0
	}

	st, err := openStore()
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	syncer := &engine.Syncer{
		Store:       st,
		Destination: destinationFactory(cfg),
		Fetcher:     &feed.Fetcher{},
		Notifier:    notify.Osascript{},
	}
	results, err := syncer.Sync(ctx, cfg.Feeds)
	if err != nil {
		return fail("%v", err)
	}

	failed := false
	for _, r := range results {
		if r.Err != nil {
			failed = true
			fmt.Printf("✗ %s: %v\n", r.Feed, r.Err)
			continue
		}
		fmt.Printf("✓ %s: %d created, %d updated, %d deleted, %d unchanged\n",
			r.Feed, r.Created, r.Updated, r.Deleted, r.Unchanged)
	}
	if failed {
		return 1
	}
	return 0
}

// openStore opens the SQLite state DB at the default location.
func openStore() (*store.Store, error) {
	path, err := config.StateDBPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	return st, nil
}
