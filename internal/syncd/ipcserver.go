package syncd

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/ipc"
	"github.com/ayusavin/mattermost-cli/internal/store"
)

// serveIPC runs the daemon's local control socket until ctx is cancelled. It
// serves /healthz and the write-ingest endpoint that gives commands immediate
// read-your-writes. Failures are recorded but non-fatal (the WebSocket/reconcile
// keep the cache correct regardless).
func (d *Daemon) serveIPC(ctx context.Context) {
	sock, err := ipc.SocketPath()
	if err != nil {
		_ = store.SetSyncError(ctx, d.db, "ipc path: "+err.Error())
		return
	}
	_ = os.Remove(sock) // clear any stale socket from a previous run
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = store.SetSyncError(ctx, d.db, "ipc listen: "+err.Error())
		return
	}
	_ = os.Chmod(sock, 0o600)

	mux := http.NewServeMux()
	mux.HandleFunc(ipc.HealthzPath, d.handleHealthz)
	mux.HandleFunc(ipc.IngestPostPath, d.handleIngestPost)
	mux.HandleFunc(ipc.SeenPath, d.handleSeen)
	mux.HandleFunc(ipc.LoadHistoryPath, d.handleLoadHistory)
	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
		_ = os.Remove(sock)
	}()
	_ = srv.Serve(ln) // returns once srv.Close runs
}

func (d *Daemon) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s, _ := store.ReadSyncState(r.Context(), d.db)
	writeIPCJSON(w, ipc.Healthz{WSConnected: s.WSConnected, BackfillDone: s.BackfillDone})
}

func (d *Daemon) handleIngestPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var p model.Post
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Upsert only (no count bump): the WebSocket echo owns total_msg_count and
	// reconcile is authoritative; this just makes the post immediately queryable.
	if err := store.UpsertPost(r.Context(), d.db, &p); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if p.UserId != "" {
		_ = d.ensureUsers(r.Context(), []string{p.UserId})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleSeen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req ipc.SeenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if req.ThroughAt <= 0 {
		req.ThroughAt = time.Now().UnixMilli()
	}
	if err := store.SetSeenCursor(r.Context(), d.db, store.CursorGlobal, req.ThroughAt); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleLoadHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req ipc.LoadHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChannelID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	loaded, err := d.LoadHistory(r.Context(), req.ChannelID, req.Limit)
	if err != nil {
		_ = store.SetSyncError(r.Context(), d.db, "load history: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeIPCJSON(w, ipc.LoadHistoryResponse{Loaded: loaded})
}

func writeIPCJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
