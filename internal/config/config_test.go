package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func withEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MM_CONFIG_PATH", filepath.Join(dir, "mm", "config.json"))
	t.Setenv("MATTERMOST_URL", "")
	t.Setenv("MATTERMOST_TOKEN", "")
	t.Setenv("MATTERMOST_TEAM", "")
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withEnv(t)
	want := Config{URL: "https://mm.example.com", AuthMethod: "token", Token: "abc123", Team: "core"}
	path, err := Save(want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file mode = %o, want 0600", perm)
		}
		di, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat dir: %v", err)
		}
		if perm := di.Mode().Perm(); perm != 0o700 {
			t.Fatalf("dir mode = %o, want 0700", perm)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	withEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if (c != Config{}) {
		t.Fatalf("expected zero config, got %+v", c)
	}
}

func TestLoadCorrupt(t *testing.T) {
	withEnv(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load()
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestResolveErrorWhenEmpty(t *testing.T) {
	withEnv(t)
	_, err := Resolve()
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("want ErrNotAuthenticated, got %v", err)
	}
}

func TestResolveEnvWins(t *testing.T) {
	withEnv(t)
	if _, err := Save(Config{URL: "https://file.example.com", Token: "file-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATTERMOST_URL", "https://env.example.com")
	t.Setenv("MATTERMOST_TOKEN", "env-token")
	c, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://env.example.com" || c.Token != "env-token" || c.AuthMethod != "env" {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestResolveEnvPartialOverlay(t *testing.T) {
	withEnv(t)
	if _, err := Save(Config{URL: "https://file.example.com", Token: "file-token", AuthMethod: "token", Team: "core"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATTERMOST_TEAM", "platform")
	c, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	// URL+Token come from file; team overlaid from env.
	if c.URL != "https://file.example.com" || c.Token != "file-token" {
		t.Fatalf("expected file URL+token, got %+v", c)
	}
	if c.Team != "platform" {
		t.Fatalf("expected env team override, got %q", c.Team)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	withEnv(t)
	if _, err := Save(Config{URL: "https://a", Token: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(Config{URL: "https://b", Token: "b"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://b" {
		t.Fatalf("second save did not replace first: %+v", c)
	}
}

func TestClear(t *testing.T) {
	withEnv(t)
	if _, err := Save(Config{URL: "https://a", Token: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if (c != Config{}) {
		t.Fatalf("after Clear expected zero, got %+v", c)
	}
	// Idempotent.
	if err := Clear(); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}
