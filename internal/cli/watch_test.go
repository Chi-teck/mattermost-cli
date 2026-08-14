package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/wsutil"
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

func TestEventActorID(t *testing.T) {
	tests := []struct {
		name  string
		event *model.WebSocketEvent
		want  string
	}{
		{
			name: "posted with post as json string",
			event: model.NewWebSocketEvent(model.WebsocketEventPosted, "team1", "chan1", "", nil, "").
				SetData(map[string]any{"post": `{"id":"p1","user_id":"author1","channel_id":"chan1","message":"hi"}`}),
			want: "author1",
		},
		{
			name: "posted with post as object",
			event: model.NewWebSocketEvent(model.WebsocketEventPosted, "team1", "chan1", "", nil, "").
				SetData(map[string]any{"post": map[string]any{"id": "p1", "user_id": "author2", "channel_id": "chan1"}}),
			want: "author2",
		},
		{
			name: "post_edited uses the post author",
			event: model.NewWebSocketEvent(model.WebsocketEventPostEdited, "team1", "chan1", "", nil, "").
				SetData(map[string]any{"post": `{"id":"p1","user_id":"author3"}`}),
			want: "author3",
		},
		{
			name: "reaction_added uses the reactor",
			event: model.NewWebSocketEvent(model.WebsocketEventReactionAdded, "team1", "chan1", "", nil, "").
				SetData(map[string]any{"reaction": `{"user_id":"reactor1","post_id":"p1","emoji_name":"tada"}`}),
			want: "reactor1",
		},
		{
			name:  "status_change falls back to broadcast scope",
			event: model.NewWebSocketEvent(model.WebsocketEventStatusChange, "", "", "user9", nil, "").SetData(map[string]any{"status": "online"}),
			want:  "user9",
		},
		{
			name: "typing falls back to the data payload",
			event: model.NewWebSocketEvent(model.WebsocketEventTyping, "team1", "chan1", "", nil, "").
				SetData(map[string]any{"user_id": "typer1"}),
			want: "typer1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventActorID(tt.event, wsutil.EventData(tt.event))
			if got != tt.want {
				t.Fatalf("eventActorID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatWatchEventJSON(t *testing.T) {
	ts := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	t.Run("post as json string", func(t *testing.T) {
		ev := model.NewWebSocketEvent(model.WebsocketEventPosted, "team1", "chan1", "", nil, "").
			SetData(map[string]any{"post": `{"id":"p1","user_id":"author1","channel_id":"chan1","root_id":"r1","message":"hi"}`})
		line := formatWatchEventJSON(ev, wsutil.EventData(ev), ts)

		if line.ActorID != "author1" {
			t.Fatalf("ActorID = %q, want %q", line.ActorID, "author1")
		}
		if line.Post == nil {
			t.Fatalf("Post = nil, want a decoded post")
		}
		if line.Post.Id != "p1" || line.Post.RootId != "r1" || line.Post.Message != "hi" {
			t.Fatalf("Post = %+v, want id p1 / root r1 / message hi", line.Post)
		}
		if line.UserID != "" {
			t.Fatalf("UserID = %q, want empty broadcast scope to be preserved", line.UserID)
		}
		if _, ok := line.Data["post"]; !ok {
			t.Fatalf("Data lost the raw post payload: %+v", line.Data)
		}
	})

	t.Run("post as object", func(t *testing.T) {
		ev := model.NewWebSocketEvent(model.WebsocketEventPosted, "team1", "chan1", "", nil, "").
			SetData(map[string]any{"post": map[string]any{"id": "p2", "user_id": "author2", "message": "yo"}})
		line := formatWatchEventJSON(ev, wsutil.EventData(ev), ts)

		if line.Post == nil || line.Post.Id != "p2" || line.Post.Message != "yo" {
			t.Fatalf("Post = %+v, want id p2 / message yo", line.Post)
		}
		if line.ActorID != "author2" {
			t.Fatalf("ActorID = %q, want %q", line.ActorID, "author2")
		}
	})

	t.Run("event without a post omits the field", func(t *testing.T) {
		ev := model.NewWebSocketEvent(model.WebsocketEventStatusChange, "", "", "user9", nil, "").
			SetData(map[string]any{"status": "online"})
		line := formatWatchEventJSON(ev, wsutil.EventData(ev), ts)

		if line.Post != nil {
			t.Fatalf("Post = %+v, want nil", line.Post)
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if strings.Contains(string(encoded), `"post"`) {
			t.Fatalf("omitempty failed, line still carries a post key: %s", encoded)
		}
		if line.ActorID != "user9" {
			t.Fatalf("ActorID = %q, want %q", line.ActorID, "user9")
		}
	})
}

// The broadcast scope of a `posted` event carries no user id, so a self-filter
// written against it would silently match nothing. This is why eventActorID
// reaches into the payload instead.
func TestEventActorIDPostedHasNoBroadcastUser(t *testing.T) {
	ev := model.NewWebSocketEvent(model.WebsocketEventPosted, "team1", "chan1", "", nil, "").
		SetData(map[string]any{"post": `{"id":"p1","user_id":"author1"}`})
	if got := wsutil.UserID(ev); got != "" {
		t.Fatalf("wsutil.UserID() = %q, want empty for a posted event", got)
	}
	if got := eventActorID(ev, wsutil.EventData(ev)); got != "author1" {
		t.Fatalf("eventActorID() = %q, want %q", got, "author1")
	}
}
