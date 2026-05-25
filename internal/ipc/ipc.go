// Package ipc is the thin local control channel between mm commands and the
// sync daemon over a Unix domain socket. Reads never use it (the CLI queries
// SQLite directly); it exists so a write command can hand the freshly created
// post to the daemon — the single DB writer — for immediate read-your-writes.
package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

const (
	HealthzPath     = "/healthz"
	IngestPostPath  = "/v1/ingest/post"
	SeenPath        = "/v1/seen"
	LoadHistoryPath = "/v1/load-history"

	dialTimeout = 2 * time.Second
	// loadHistoryTimeout is generous: paging older history can take several
	// round-trips against a slow server.
	loadHistoryTimeout = 60 * time.Second
)

// SeenRequest advances the agent's processing cursor. ThroughAt is epoch ms;
// zero means "now" (filled in by the daemon).
type SeenRequest struct {
	ThroughAt int64 `json:"through_at"`
}

// LoadHistoryRequest asks the daemon to fetch older posts for a channel into
// the cache.
type LoadHistoryRequest struct {
	ChannelID string `json:"channel_id"`
	Limit     int    `json:"limit"`
}

// LoadHistoryResponse reports how many posts the daemon fetched.
type LoadHistoryResponse struct {
	Loaded int `json:"loaded"`
}

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

func unixClient(sock string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
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
	resp, err := unixClient(sock, dialTimeout).Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Seen advances the agent's processing cursor via the daemon (the single DB
// writer). Unlike NotifyPost this is not best-effort: it returns an error when
// the daemon is unreachable, since the caller needs the cursor persisted.
func Seen(ctx context.Context, throughAt int64) error {
	sock, err := SocketPath()
	if err != nil {
		return err
	}
	body, err := json.Marshal(SeenRequest{ThroughAt: throughAt})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+SeenPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := unixClient(sock, dialTimeout).Do(req)
	if err != nil {
		return fmt.Errorf("sync daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	return nil
}

// LoadHistory asks the daemon to fetch up to limit older posts for a channel
// into the cache. Returns the number fetched. Requires a running daemon.
func LoadHistory(ctx context.Context, channelID string, limit int) (int, error) {
	sock, err := SocketPath()
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(LoadHistoryRequest{ChannelID: channelID, Limit: limit})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+LoadHistoryPath, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := unixClient(sock, loadHistoryTimeout).Do(req)
	if err != nil {
		return 0, fmt.Errorf("sync daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("daemon returned %s", resp.Status)
	}
	var out LoadHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Loaded, nil
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
	resp, err := unixClient(sock, dialTimeout).Do(req)
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
