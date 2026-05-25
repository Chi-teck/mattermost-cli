package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/ipc"
	"github.com/ayusavin/mattermost-cli/internal/syncd"
)

func init() {
	Register(newSyncCmd)
	Register(newSyncRunCmd)
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// newSyncRunCmd is the hidden entrypoint the detached daemon process runs.
func newSyncRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__sync-run",
		Hidden: true,
		Short:  "Internal: run the sync daemon in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			lock, err := syncd.AcquireLock()
			if err != nil {
				if errors.Is(err, syncd.ErrAlreadyRunning) {
					return nil // another daemon owns the writer lock; exit quietly
				}
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			}
			defer lock.Close()

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			d, err := syncd.New(ctx)
			if err != nil {
				return err
			}
			return d.Run(ctx)
		},
	}
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage the local-sync daemon (backfills + realtime-syncs into local SQLite)",
	}
	cmd.AddCommand(syncStartCmd(), syncStopCmd(), syncStatusCmd(), syncLogsCmd())
	return cmd
}

func syncStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the sync daemon (no-op if already running)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmdContext(cmd)
			if s, exists, err := syncd.ReadState(ctx); err == nil && exists && syncd.Running(s) {
				return emitSyncStatus(ctx, "already running")
			}
			if err := syncd.Spawn(); err != nil {
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			}
			if _, fresh := syncd.WaitFresh(ctx, 15*time.Second); !fresh {
				logPath, _ := syncd.LogPath()
				return errs.Errorf(errs.CodeGeneric,
					"daemon spawned but did not report a heartbeat; check %s", logPath)
			}
			return emitSyncStatus(ctx, "started")
		},
	}
}

func syncStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running sync daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmdContext(cmd)
			switch err := syncd.Stop(ctx); {
			case errors.Is(err, syncd.ErrNotRunning):
				return writeSyncMessage("not running", false)
			case err != nil:
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			default:
				return writeSyncMessage("stopped", false)
			}
		},
	}
}

func syncStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show sync daemon status and freshness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return emitSyncStatus(cmdContext(cmd), "")
		},
	}
}

func syncLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Print the daemon log file path and its tail",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := syncd.LogPath()
			if err != nil {
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			}
			fmt.Fprintln(os.Stderr, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return errs.Errorf(errs.CodeGeneric, "read log: %s", err.Error())
			}
			const maxTail = 8 << 10
			if len(data) > maxTail {
				data = data[len(data)-maxTail:]
			}
			_, _ = os.Stdout.Write(data)
			return nil
		},
	}
}

type syncStatusOut struct {
	Status             string `json:"status,omitempty"`
	Running            bool   `json:"running"`
	PID                int64  `json:"pid,omitempty"`
	IPCReachable       bool   `json:"ipc_reachable"`
	WSConnected        bool   `json:"ws_connected"`
	BackfillDone       bool   `json:"backfill_done"`
	HeartbeatAgeMs     int64  `json:"heartbeat_age_ms,omitempty"`
	LastEventAgeMs     int64  `json:"last_event_age_ms,omitempty"`
	LastReconcileAgeMs int64  `json:"last_reconcile_age_ms,omitempty"`
	ServerURL          string `json:"server_url,omitempty"`
	UserID             string `json:"user_id,omitempty"`
	LastError          string `json:"last_error,omitempty"`
}

func emitSyncStatus(ctx context.Context, status string) error {
	s, exists, err := syncd.ReadState(ctx)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
	}
	running := exists && syncd.Running(s)
	_, ipcOK := ipc.Health(ctx)
	out := syncStatusOut{
		Status:       status,
		Running:      running,
		PID:          s.DaemonPID,
		IPCReachable: ipcOK,
		WSConnected:  s.WSConnected,
		BackfillDone: s.BackfillDone,
		ServerURL:    s.ServerURL,
		UserID:       s.MeUserID,
		LastError:    s.LastError,
	}
	if s.HeartbeatAt > 0 {
		out.HeartbeatAgeMs = ageMS(s.HeartbeatAt)
	}
	if s.LastEventAt > 0 {
		out.LastEventAgeMs = ageMS(s.LastEventAt)
	}
	if s.LastReconcileAt > 0 {
		out.LastReconcileAgeMs = ageMS(s.LastReconcileAt)
	}
	if Globals.Human {
		fmt.Fprintln(os.Stdout, humanSyncStatus(out))
		return nil
	}
	return writeJSON(os.Stdout, out)
}

func ageMS(epochMS int64) int64 {
	age := time.Since(time.UnixMilli(epochMS)).Milliseconds()
	if age < 0 {
		return 0
	}
	return age
}

func humanSyncStatus(s syncStatusOut) string {
	state := "stopped"
	if s.Running {
		state = "running"
	}
	line := fmt.Sprintf("Daemon: %s", state)
	if s.Running {
		line += fmt.Sprintf(" (pid %d, ws_connected=%t, backfill_done=%t)", s.PID, s.WSConnected, s.BackfillDone)
	}
	if s.LastError != "" {
		line += "\nLast error: " + s.LastError
	}
	return line
}

func writeSyncMessage(msg string, running bool) error {
	if Globals.Human {
		fmt.Fprintln(os.Stdout, msg)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{"status": msg, "running": running})
}
