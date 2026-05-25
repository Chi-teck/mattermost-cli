package cli

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

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
