// Package config handles persistent storage of Mattermost credentials.
//
// Tokens are written atomically with file mode 0600 in a directory with mode
// 0700. Env vars take precedence over the on-disk config:
//
//	MATTERMOST_URL > config.url
//	MATTERMOST_TOKEN > config.token
//	MATTERMOST_TEAM > config.team
//
// The on-disk format is JSON and intentionally compatible with the legacy
// Python implementation so both clients can share a config.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the on-disk credential blob.
type Config struct {
	URL        string `json:"url"`
	AuthMethod string `json:"auth_method,omitempty"`
	Token      string `json:"token"`
	Team       string `json:"team,omitempty"`
}

// ErrNotAuthenticated is returned by Resolve when no credentials can be found.
var ErrNotAuthenticated = errors.New(
	"not authenticated. Run 'mm login' to set up credentials, " +
		"or set MATTERMOST_URL and MATTERMOST_TOKEN environment variables",
)

// Path returns the resolved config file path, honoring $MM_CONFIG_PATH and
// then $XDG_CONFIG_HOME. Falls back to $HOME/.config/mm/config.json.
func Path() (string, error) {
	if p := os.Getenv("MM_CONFIG_PATH"); p != "" {
		return p, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mm", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".config", "mm", "config.json"), nil
}

// Load reads the config file. Missing file returns a zero Config and nil error.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config at %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config at %s: %w", path, err)
	}
	return c, nil
}

// Save writes the config atomically with mode 0600 in a 0700 directory.
//
// Atomicity: writes a sibling temp file, fsyncs it, then renames over the
// destination. A Ctrl-C in the middle never leaves a partial config.
func Save(c Config) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	// Best-effort tightening if the dir already existed with wider perms.
	_ = os.Chmod(dir, 0o700)

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "config.*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	// Ensure cleanup if anything below fails.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("fsync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("rename temp config to %s: %w", path, err)
	}
	return path, nil
}

// Clear deletes the config file. Missing file is not an error.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove config at %s: %w", path, err)
	}
	return nil
}

// Resolve merges env vars and file config and returns the effective credentials.
// Env vars override file values; both URL and Token must be present after the merge.
func Resolve() (Config, error) {
	url := os.Getenv("MATTERMOST_URL")
	token := os.Getenv("MATTERMOST_TOKEN")
	team := os.Getenv("MATTERMOST_TEAM")

	if url != "" && token != "" {
		out := Config{URL: url, Token: token, AuthMethod: "env"}
		if team != "" {
			out.Team = team
		}
		return out, nil
	}

	c, err := Load()
	if err != nil {
		return Config{}, err
	}
	if c.URL == "" || c.Token == "" {
		return Config{}, ErrNotAuthenticated
	}
	if url != "" {
		c.URL = url
	}
	if token != "" {
		c.Token = token
	}
	if team != "" {
		c.Team = team
	}
	return c, nil
}
