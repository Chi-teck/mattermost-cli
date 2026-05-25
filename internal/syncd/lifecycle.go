package syncd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

// heartbeatStale is how long without a heartbeat before a daemon is considered
// dead. The daemon beats every heartbeatInterval (10s).
const heartbeatStale = 30 * time.Second

func runtimeFile(name string) (string, error) {
	base, err := store.BaseDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	return filepath.Join(base, name), nil
}

// LockPath is the single-writer lock file path.
func LockPath() (string, error) { return runtimeFile("daemon.lock") }

// LogPath is the daemon's log file path.
func LogPath() (string, error) { return runtimeFile("daemon.log") }

// AcquireLock takes an exclusive, non-blocking flock guaranteeing a single
// writer. The returned file must be kept open for the daemon's lifetime; the OS
// releases the lock on process exit. Returns ErrAlreadyRunning if held.
func AcquireLock() (*os.File, error) {
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}

// ErrAlreadyRunning means another daemon holds the writer lock.
var ErrAlreadyRunning = errors.New("sync daemon already running")

// HeartbeatFresh reports whether the daemon's last heartbeat is recent enough to
// consider it alive.
func HeartbeatFresh(s store.SyncState) bool {
	if s.HeartbeatAt == 0 {
		return false
	}
	age := time.Since(time.UnixMilli(s.HeartbeatAt))
	return age >= 0 && age < heartbeatStale
}

// ReadState opens the store read-only and returns the daemon row. exists is
// false when no database has been created yet (daemon never ran).
func ReadState(ctx context.Context) (state store.SyncState, exists bool, err error) {
	path, err := store.DBPath(false)
	if err != nil {
		return store.SyncState{}, false, err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return store.SyncState{}, false, nil
	}
	db, err := store.Open(path, true)
	if err != nil {
		return store.SyncState{}, false, err
	}
	defer db.Close()
	s, err := store.ReadSyncState(ctx, db)
	if err != nil {
		return store.SyncState{}, true, err
	}
	return s, true, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

// Spawn launches a detached daemon process (mm __sync-run) writing to the log
// file. It returns once the child is started; the child does the work.
func Spawn() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	logPath, err := LogPath()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logf.Close()

	cmd := exec.Command(self, "__sync-run")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this session
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Release the child so it isn't a zombie when this process exits.
	return cmd.Process.Release()
}

// WaitFresh polls the daemon state until its heartbeat is fresh or timeout.
func WaitFresh(ctx context.Context, timeout time.Duration) (store.SyncState, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s, exists, err := ReadState(ctx); err == nil && exists && HeartbeatFresh(s) {
			return s, true
		}
		if !sleep(ctx, 200*time.Millisecond) {
			break
		}
	}
	s, _, _ := ReadState(ctx)
	return s, HeartbeatFresh(s)
}

// Stop signals the running daemon to terminate and waits for it to exit.
func Stop(ctx context.Context) error {
	s, exists, err := ReadState(ctx)
	if err != nil {
		return err
	}
	if !exists || !HeartbeatFresh(s) || !processAlive(int(s.DaemonPID)) {
		return ErrNotRunning
	}
	if err := unix.Kill(int(s.DaemonPID), unix.SIGTERM); err != nil {
		return fmt.Errorf("signal daemon: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(int(s.DaemonPID)) {
			return nil
		}
		if !sleep(ctx, 200*time.Millisecond) {
			break
		}
	}
	if processAlive(int(s.DaemonPID)) {
		return fmt.Errorf("daemon (pid %d) did not exit within 10s", s.DaemonPID)
	}
	return nil
}

// ErrNotRunning means no live daemon was found.
var ErrNotRunning = errors.New("sync daemon is not running")
