package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// BaseDir returns the per-user cache directory for mm's local data, honoring
// $MM_CACHE_PATH then os.UserCacheDir() (~/Library/Caches/mm on macOS,
// ~/.cache/mm on Linux). The DB is regenerable cache, hence cache dir — not the
// config dir where credentials live.
func BaseDir() (string, error) {
	if p := os.Getenv("MM_CACHE_PATH"); p != "" {
		return p, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache dir: %w", err)
	}
	return filepath.Join(cache, "mm"), nil
}

// DBPath returns the SQLite database path under BaseDir, creating the directory
// (0700) when create is true.
func DBPath(create bool) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	if create {
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", fmt.Errorf("create cache dir: %w", err)
		}
	}
	return filepath.Join(base, "store.db"), nil
}
