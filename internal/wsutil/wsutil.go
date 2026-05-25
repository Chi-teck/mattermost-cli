// Package wsutil holds the Mattermost WebSocket connection and event-decoding
// helpers shared by the `watch` command and the sync daemon. Keeping one
// implementation means the daemon's realtime ingestion and `mm watch` stay in
// lockstep on URL handling, reconnection, and payload extraction.
package wsutil

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

const apiSuffix = "/api/v4/websocket"

// Connect opens a listening Mattermost WebSocket client for the server at rawURL
// (an http(s) base URL, with or without scheme) authenticated with token. The
// returned client is already listening; drain happens in a background goroutine.
func Connect(rawURL, token string) (*model.WebSocketClient, error) {
	wsURL, err := WSURL(rawURL)
	if err != nil {
		return nil, err
	}
	// NewWebSocketClient4 appends /api/v4/websocket internally, so pass the base.
	base := strings.TrimSuffix(wsURL, apiSuffix)
	c, err := model.NewWebSocketClient4(base, token)
	if err != nil {
		return nil, err
	}
	c.Listen()
	go drainResponses(c.ResponseChannel)
	return c, nil
}

func drainResponses(ch <-chan *model.WebSocketResponse) {
	for range ch {
	}
}

// WSURL converts an http(s) server URL into the full ws(s) websocket endpoint
// URL, preserving any base path.
func WSURL(raw string) (string, error) {
	u, err := parseURL(raw)
	if err != nil {
		return "", err
	}
	return urlWithSuffix(u), nil
}

func parseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, fmt.Errorf("no host in URL %q", raw)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func urlWithSuffix(u *url.URL) string {
	v := *u
	base := strings.TrimRight(v.Path, "/")
	if base == "" {
		v.Path = apiSuffix
	} else {
		v.Path = path.Join(base, "api", "v4", "websocket")
	}
	return v.String()
}

// EventData merges an event's broadcast scope (team/channel/user/connection ids)
// with its data payload into a single map.
func EventData(ev *model.WebSocketEvent) map[string]any {
	data := make(map[string]any)
	if b := ev.GetBroadcast(); b != nil {
		if b.TeamId != "" {
			data["team_id"] = b.TeamId
		}
		if b.ChannelId != "" {
			data["channel_id"] = b.ChannelId
		}
		if b.UserId != "" {
			data["user_id"] = b.UserId
		}
		if b.ConnectionId != "" {
			data["connection_id"] = b.ConnectionId
		}
	}
	for k, v := range ev.GetData() {
		data[k] = v
	}
	return data
}

// ChannelID returns the event's channel id from its broadcast scope or data.
func ChannelID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.ChannelId != "" {
		return b.ChannelId
	}
	return StringFromMap(ev.GetData(), "channel_id")
}

// TeamID returns the event's team id from its broadcast scope or data.
func TeamID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.TeamId != "" {
		return b.TeamId
	}
	return StringFromMap(ev.GetData(), "team_id")
}

// UserID returns the event's user id from its broadcast scope or data.
func UserID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.UserId != "" {
		return b.UserId
	}
	return StringFromMap(ev.GetData(), "user_id")
}

// DecodePayload decodes a WebSocket sub-payload into dst. Mattermost sends these
// as JSON strings, raw bytes, or (already-decoded) maps depending on the path.
func DecodePayload(value, dst any) {
	switch v := value.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), dst)
	case []byte:
		_ = json.Unmarshal(v, dst)
	case map[string]any:
		b, err := json.Marshal(v)
		if err == nil {
			_ = json.Unmarshal(b, dst)
		}
	}
}

// StringFromMap returns data[key] as a string, or "" if absent/non-string.
func StringFromMap(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	if s, ok := data[key].(string); ok {
		return s
	}
	return ""
}

// FirstNonEmpty returns the first non-empty string argument.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
