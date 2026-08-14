// Package syncd is the mm sync daemon: it is the single writer of the local
// SQLite cache, backfilling from the Mattermost API, applying realtime
// WebSocket events, and periodically reconciling read state. `mm query` reads
// the cache it maintains; write commands reach it over IPC (added separately).
package syncd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/client"
	"github.com/ayusavin/mattermost-cli/internal/config"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/store"
	"github.com/ayusavin/mattermost-cli/internal/wsutil"
)

const (
	heartbeatInterval = 10 * time.Second
	reconcileInterval = 60 * time.Second
	reconnectDelay    = 3 * time.Second
)

// Daemon owns the live API client and the read-write store.
type Daemon struct {
	api      client.API
	db       *sql.DB
	resolver *resolve.Resolver
	me       *model.User
	cfg      config.Config
}

// New authenticates, opens the read-write store (running migrations), and
// returns a ready Daemon. The caller owns the lifecycle via Run.
func New(ctx context.Context) (*Daemon, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return nil, err
	}
	api, err := client.New(cfg.URL, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}
	me, err := client.Login(ctx, api)
	if err != nil {
		return nil, err
	}
	path, err := store.DBPath(true)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(path, false)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		api:      api,
		db:       db,
		resolver: resolve.New(api, me.Id),
		me:       me,
		cfg:      cfg,
	}, nil
}

// Run executes the daemon until ctx is cancelled: init state, backfill, then a
// reconnecting WebSocket consume loop with a periodic reconcile and heartbeat.
func (d *Daemon) Run(ctx context.Context) error {
	defer d.close()

	if err := store.InitSyncState(ctx, d.db, os.Getpid(), d.cfg.URL, d.me.Id); err != nil {
		return fmt.Errorf("init sync state: %w", err)
	}

	go d.heartbeatLoop(ctx)
	go d.serveIPC(ctx) // control socket up early so write commands can ingest during backfill

	if err := d.Backfill(ctx); err != nil {
		_ = store.SetSyncError(ctx, d.db, "backfill: "+err.Error())
		return fmt.Errorf("backfill: %w", err)
	}
	if err := store.SetBackfillDone(ctx, d.db, true); err != nil {
		return err
	}

	go d.reconcileLoop(ctx)

	return d.webSocketLoop(ctx)
}

func (d *Daemon) close() {
	// Truncate the WAL so the -wal sidecar doesn't grow unbounded across restarts.
	_, _ = d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	_ = d.db.Close()
}

func (d *Daemon) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = store.Heartbeat(ctx, d.db)
		}
	}
}

func (d *Daemon) reconcileLoop(ctx context.Context) {
	t := time.NewTicker(reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.Reconcile(ctx); err != nil {
				_ = store.SetSyncError(ctx, d.db, "reconcile: "+err.Error())
			}
		}
	}
}

// webSocketLoop connects, consumes events until the connection drops, then
// reconnects (with a short delay) until ctx is cancelled. A reconcile runs on
// every (re)connect to close any gap accumulated while disconnected.
func (d *Daemon) webSocketLoop(ctx context.Context) error {
	for ws, err := range wsutil.Connections(ctx, d.cfg.URL, d.cfg.Token, reconnectDelay) {
		if err != nil {
			_ = store.SetWSConnected(ctx, d.db, false)
			_ = store.SetSyncError(ctx, d.db, "ws connect: "+err.Error())
			continue
		}
		_ = store.SetWSConnected(ctx, d.db, true)
		_ = store.SetSyncError(ctx, d.db, "")
		if err := d.Reconcile(ctx); err != nil {
			_ = store.SetSyncError(ctx, d.db, "reconcile: "+err.Error())
		}

		d.consume(ctx, ws)
		_ = store.SetWSConnected(ctx, d.db, false)
	}
	return nil
}

func (d *Daemon) consume(ctx context.Context, ws *model.WebSocketClient) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.PingTimeoutChannel:
			return
		case ev, ok := <-ws.EventChannel:
			if !ok {
				return
			}
			if ev == nil {
				continue
			}
			if err := d.handleEvent(ctx, ev); err != nil {
				_ = store.SetSyncError(ctx, d.db, "apply event: "+err.Error())
			}
		}
	}
}

// ensureUsers fetches and stores any of the given user IDs not already cached
// locally, so post authors resolve to usernames in v_post.
func (d *Daemon) ensureUsers(ctx context.Context, ids []string) error {
	missing := d.missingUserIDs(ctx, ids)
	for _, chunk := range chunkStrings(missing, 100) {
		users, err := retryList(ctx, func() ([]*model.User, *model.Response, error) {
			return d.api.GetUsersByIds(ctx, chunk)
		})
		if err != nil {
			return err
		}
		for _, u := range users {
			if u == nil {
				continue
			}
			if err := store.UpsertUser(ctx, d.db, u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Daemon) missingUserIDs(ctx context.Context, ids []string) []string {
	seen := make(map[string]bool, len(ids))
	var missing []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var exists int
		err := d.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=?`, id).Scan(&exists)
		if err == sql.ErrNoRows {
			missing = append(missing, id)
		}
	}
	return missing
}

// retryList wraps a list-returning SDK call in the shared backoff retry.
func retryList[T any](ctx context.Context, fn func() (T, *model.Response, error)) (T, error) {
	var out T
	_, err := client.Retry(ctx, func() (*model.Response, error) {
		var resp *model.Response
		var err error
		out, resp, err = fn()
		return resp, err
	})
	return out, err
}

func chunkStrings(s []string, size int) [][]string {
	if size <= 0 || len(s) == 0 {
		if len(s) == 0 {
			return nil
		}
		return [][]string{s}
	}
	var out [][]string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

// sleep waits d or until ctx is done; returns false if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
