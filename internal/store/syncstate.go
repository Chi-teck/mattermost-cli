package store

import (
	"context"
	"database/sql"
	"time"
)

// SyncState is the daemon's single-row heartbeat / freshness record. Readers use
// it to decide whether local data is trustworthy or to fall back to the live API.
type SyncState struct {
	DaemonPID       int64  `json:"daemon_pid"`
	StartedAt       int64  `json:"started_at"`
	HeartbeatAt     int64  `json:"heartbeat_at"`
	LastEventAt     int64  `json:"last_event_at"`
	LastReconcileAt int64  `json:"last_reconcile_at"`
	WSConnected     bool   `json:"ws_connected"`
	BackfillDone    bool   `json:"backfill_done"`
	SchemaVersion   int    `json:"schema_version"`
	ServerURL       string `json:"server_url"`
	MeUserID        string `json:"me_user_id"`
	LastError       string `json:"last_error"`
}

func nowMS() int64 { return time.Now().UnixMilli() }

// InitSyncState creates (or resets) the daemon row at startup.
func InitSyncState(ctx context.Context, db *sql.DB, pid int, serverURL, meUserID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO sync_state(id, daemon_pid, started_at, heartbeat_at, ws_connected,
			backfill_done, schema_version, server_url, me_user_id, last_error)
		VALUES (1, ?, ?, ?, 0, 0, ?, ?, ?, '')
		ON CONFLICT(id) DO UPDATE SET
			daemon_pid=excluded.daemon_pid, started_at=excluded.started_at,
			heartbeat_at=excluded.heartbeat_at, ws_connected=0, backfill_done=0,
			schema_version=excluded.schema_version, server_url=excluded.server_url,
			me_user_id=excluded.me_user_id, last_error=''`,
		pid, nowMS(), nowMS(), SchemaVersion, serverURL, meUserID)
	return err
}

// Heartbeat bumps heartbeat_at to now. Readers treat a stale heartbeat as a dead
// daemon and fall back to live.
func Heartbeat(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET heartbeat_at=? WHERE id=1`, nowMS())
	return err
}

// SetWSConnected records whether the WebSocket is currently connected.
func SetWSConnected(ctx context.Context, db *sql.DB, connected bool) error {
	v := 0
	if connected {
		v = 1
	}
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET ws_connected=?, heartbeat_at=? WHERE id=1`, v, nowMS())
	return err
}

// SetBackfillDone marks the initial backfill complete.
func SetBackfillDone(ctx context.Context, db *sql.DB, done bool) error {
	v := 0
	if done {
		v = 1
	}
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET backfill_done=? WHERE id=1`, v)
	return err
}

// SetLastEvent records the timestamp of the last applied WebSocket event.
func SetLastEvent(ctx context.Context, db *sql.DB, ts int64) error {
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET last_event_at=?, heartbeat_at=? WHERE id=1`, ts, nowMS())
	return err
}

// SetReconcileAt records a successful reconciliation pass.
func SetReconcileAt(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET last_reconcile_at=? WHERE id=1`, nowMS())
	return err
}

// SetSyncError stores the latest fatal/transient error message for `mm sync status`.
func SetSyncError(ctx context.Context, db *sql.DB, msg string) error {
	_, err := db.ExecContext(ctx, `UPDATE sync_state SET last_error=? WHERE id=1`, msg)
	return err
}

// ReadSyncState returns the daemon row. Missing row yields a zero SyncState.
func ReadSyncState(ctx context.Context, db *sql.DB) (SyncState, error) {
	var s SyncState
	var ws, backfill int
	err := db.QueryRowContext(ctx, `
		SELECT daemon_pid, started_at, heartbeat_at, last_event_at, last_reconcile_at,
			ws_connected, backfill_done, schema_version, server_url, me_user_id, last_error
		FROM sync_state WHERE id=1`).Scan(
		&s.DaemonPID, &s.StartedAt, &s.HeartbeatAt, &s.LastEventAt, &s.LastReconcileAt,
		&ws, &backfill, &s.SchemaVersion, &s.ServerURL, &s.MeUserID, &s.LastError,
	)
	if err == sql.ErrNoRows {
		return SyncState{}, nil
	}
	if err != nil {
		return SyncState{}, err
	}
	s.WSConnected = ws == 1
	s.BackfillDone = backfill == 1
	return s, nil
}
