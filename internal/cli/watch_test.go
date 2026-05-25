package cli

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestWSURLFromAPIURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: "https://chat.example.com", want: "wss://chat.example.com/api/v4/websocket"},
		{name: "http", raw: "http://localhost:8065", want: "ws://localhost:8065/api/v4/websocket"},
		{name: "path", raw: "https://chat.example.com/mm/", want: "wss://chat.example.com/mm/api/v4/websocket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wsURLFromAPIURL(tt.raw)
			if err != nil {
				t.Fatalf("wsURLFromAPIURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("wsURLFromAPIURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEventTypeIncluded(t *testing.T) {
	include := parseWatchEventTypes("posted, reaction_added")
	if !eventTypeIncluded(model.WebsocketEventPosted, include) {
		t.Fatalf("posted should be included")
	}
	if !eventTypeIncluded(model.WebsocketEventReactionAdded, include) {
		t.Fatalf("reaction_added should be included")
	}
	if eventTypeIncluded(model.WebsocketEventTyping, include) {
		t.Fatalf("typing should not be included")
	}
}

func TestEventTypeIncludedEmptyMeansAll(t *testing.T) {
	if !eventTypeIncluded(model.WebsocketEventTyping, nil) {
		t.Fatalf("nil include set should include all event types")
	}
	if !eventTypeIncluded(model.WebsocketEventTyping, parseWatchEventTypes("")) {
		t.Fatalf("empty include set should include all event types")
	}
}
