// Package ipc is the thin local control channel between mm commands and the
// sync daemon over a Unix domain socket. Reads never use it (the CLI queries
// SQLite directly); it exists so a write command can hand the freshly created
// post to the daemon — the single DB writer — for immediate read-your-writes.
package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

const (
	HealthzPath    = "/healthz"
	IngestPostPath = "/v1/ingest/post"

	dialTimeout = 2 * time.Second
)

// SocketPath is the daemon's Unix socket path under the cache dir.
func SocketPath() (string, error) {
	base, err := store.BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "daemon.sock"), nil
}

// Healthz is the daemon's liveness/freshness summary.
type Healthz struct {
	WSConnected  bool `json:"ws_connected"`
	BackfillDone bool `json:"backfill_done"`
}

func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: dialTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}
}

// NotifyPost hands a created/updated post to the daemon for immediate local
// upsert. Best-effort: if the daemon isn't running the WebSocket echo and the
// next reconcile will pick the post up, so any error is silently ignored.
func NotifyPost(ctx context.Context, p *model.Post) {
	if p == nil {
		return
	}
	sock, err := SocketPath()
	if err != nil {
		return
	}
	body, err := json.Marshal(p)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+IngestPostPath, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := unixClient(sock).Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Health queries the daemon's /healthz. ok is false if the daemon is unreachable.
func Health(ctx context.Context) (h Healthz, ok bool) {
	sock, err := SocketPath()
	if err != nil {
		return Healthz{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+HealthzPath, nil)
	if err != nil {
		return Healthz{}, false
	}
	resp, err := unixClient(sock).Do(req)
	if err != nil {
		return Healthz{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Healthz{}, false
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return Healthz{}, false
	}
	return h, true
}
