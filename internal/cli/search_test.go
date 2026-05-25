package cli

import (
	"testing"
	"time"

	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

func TestBuildSearchTerms(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	sinceMs, err := timeparse.Parse("1d", now)
	if err != nil {
		t.Fatal(err)
	}
	got := buildSearchTerms("query", "foo", "alice", sinceMs)
	want := "query in:foo from:alice after:2026-05-23"
	if got != want {
		t.Fatalf("buildSearchTerms() = %q, want %q", got, want)
	}
}
