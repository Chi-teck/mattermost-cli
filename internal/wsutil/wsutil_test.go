package wsutil

import (
	"context"
	"testing"
	"time"
)

func TestWSURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: "https://chat.example.com", want: "wss://chat.example.com/api/v4/websocket"},
		{name: "http", raw: "http://localhost:8065", want: "ws://localhost:8065/api/v4/websocket"},
		{name: "path", raw: "https://chat.example.com/mm/", want: "wss://chat.example.com/mm/api/v4/websocket"},
		{name: "no scheme defaults https", raw: "chat.example.com", want: "wss://chat.example.com/api/v4/websocket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WSURL(tt.raw)
			if err != nil {
				t.Fatalf("WSURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("WSURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWSURLErrors(t *testing.T) {
	for _, raw := range []string{"", "ftp://host"} {
		if _, err := WSURL(raw); err == nil {
			t.Fatalf("WSURL(%q) expected error, got nil", raw)
		}
	}
}

// An empty URL fails in WSURL before any dial, so these exercise the retry loop
// without touching the network.
const unreachableURL = ""

func TestConnectionsRetriesDialFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	for c, err := range Connections(ctx, unreachableURL, "token", time.Millisecond) {
		if c != nil {
			t.Fatalf("expected a nil client on dial failure, got %v", c)
		}
		if err == nil {
			t.Fatalf("expected a dial error, got nil")
		}
		attempts++
		if attempts == 3 {
			break
		}
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 — a dial failure must not end the sequence", attempts)
	}
}

func TestConnectionsStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range Connections(ctx, unreachableURL, "token", time.Millisecond) {
		t.Fatalf("expected no iterations for an already-cancelled context")
	}
}

func TestConnectionsStopsWhenContextIsCancelledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	for range Connections(ctx, unreachableURL, "token", time.Millisecond) {
		attempts++
		if attempts == 2 {
			cancel()
		}
		if attempts > 10 {
			t.Fatalf("cancelling the context did not stop the sequence")
		}
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
