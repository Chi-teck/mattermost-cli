package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
)

const (
	defaultWatchTypes = "posted,reaction_added,reaction_removed,channel_viewed,status_change,typing"
	watchAPISuffix    = "/api/v4/websocket"
)

func init() {
	Register(newWatchCmd)
}

func newWatchCmd() *cobra.Command {
	var (
		channels []string
		typesCSV string
		limit    int
		timeout  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream Mattermost WebSocket events",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			include := parseWatchEventTypes(typesCSV)
			return runWatch(ctx, channels, include, limit, timeout)
		},
	}
	cmd.Flags().StringArrayVar(&channels, "channel", nil, "Filter to a channel id (repeatable)")
	cmd.Flags().StringVar(&typesCSV, "types", defaultWatchTypes, "Comma-separated WebSocket event types to include")
	cmd.Flags().IntVar(&limit, "limit", 0, "Exit after N events (0 = no limit)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Exit after a duration of output inactivity (0 = no timeout)")
	return cmd
}

type watchEventLine struct {
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	ChannelID string         `json:"channel_id"`
	TeamID    string         `json:"team_id"`
	UserID    string         `json:"user_id"`
	Data      map[string]any `json:"data"`
}

func runWatch(ctx context.Context, channels []string, include map[model.WebsocketEventType]bool, limit int, timeout time.Duration) error {
	lc, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	wsURL, err := wsURLFromAPIURL(lc.Cfg.URL)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "build websocket URL: %s", err.Error())
	}

	client, err := connectWatchWebSocket(wsURL, lc.Cfg.Token)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "connect websocket: %s", err.Error())
	}
	defer client.Close()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	channelSet := stringSet(channels)
	received := 0
	pingTimeouts := 0
	disconnectReconnects := 0
	var timer *time.Timer
	var inactivity <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		inactivity = timer.C
	}
	resetTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(timeout)
	}

	reconnect := func(reason string) error {
		fmt.Fprintf(os.Stderr, "warning: websocket %s; reconnecting once\n", reason)
		client.Close()
		next, err := connectWatchWebSocket(wsURL, lc.Cfg.Token)
		if err != nil {
			return errs.Errorf(errs.CodeGeneric, "reconnect websocket after %s: %s", reason, err.Error())
		}
		client = next
		resetTimer()
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-inactivity:
			return nil
		case <-client.PingTimeoutChannel:
			pingTimeouts++
			if pingTimeouts > 1 {
				return errs.Errorf(errs.CodeGeneric, "websocket ping timed out twice")
			}
			if err := reconnect("ping timed out"); err != nil {
				return err
			}
		case ev, ok := <-client.EventChannel:
			if !ok {
				disconnectReconnects++
				if disconnectReconnects > 1 {
					return errs.Errorf(errs.CodeGeneric, "websocket disconnected after reconnect")
				}
				if err := reconnect("disconnected"); err != nil {
					return err
				}
				continue
			}
			if ev == nil || !eventTypeIncluded(ev.EventType(), include) || !eventChannelIncluded(ev, channelSet) {
				continue
			}

			now := time.Now().UTC()
			if Globals.Human {
				fmt.Fprintln(out, formatWatchEventHuman(ev, now))
			} else {
				if err := enc.Encode(formatWatchEventJSON(ev, now)); err != nil {
					return errs.Errorf(errs.CodeGeneric, "write event: %s", err.Error())
				}
			}
			if err := out.Flush(); err != nil {
				return errs.Errorf(errs.CodeGeneric, "flush event: %s", err.Error())
			}
			resetTimer()
			received++
			if limit > 0 && received >= limit {
				return nil
			}
		}
	}
}

func connectWatchWebSocket(wsURL, token string) (*model.WebSocketClient, error) {
	// NewWebSocketClient4 appends /api/v4/websocket internally, so pass the base URL.
	wsBaseURL := strings.TrimSuffix(wsURL, watchAPISuffix)
	c, err := model.NewWebSocketClient4(wsBaseURL, token)
	if err != nil {
		return nil, err
	}
	c.Listen()
	go drainWatchResponses(c.ResponseChannel)
	return c, nil
}

func drainWatchResponses(ch <-chan *model.WebSocketResponse) {
	for range ch {
	}
}

func wsURLFromAPIURL(raw string) (string, error) {
	u, err := parseWatchURL(raw)
	if err != nil {
		return "", err
	}
	return watchURLWithSuffix(u), nil
}

func wsBaseURLFromAPIURL(raw string) (string, error) {
	u, err := parseWatchURL(raw)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func parseWatchURL(raw string) (*url.URL, error) {
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

func watchURLWithSuffix(u *url.URL) string {
	v := *u
	base := strings.TrimRight(v.Path, "/")
	if base == "" {
		v.Path = watchAPISuffix
	} else {
		v.Path = path.Join(base, "api", "v4", "websocket")
	}
	return v.String()
}

func parseWatchEventTypes(csv string) map[model.WebsocketEventType]bool {
	parts := strings.Split(csv, ",")
	include := make(map[model.WebsocketEventType]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		include[model.WebsocketEventType(part)] = true
	}
	return include
}

func eventTypeIncluded(eventType model.WebsocketEventType, include map[model.WebsocketEventType]bool) bool {
	if len(include) == 0 {
		return true
	}
	return include[eventType]
}

func eventChannelIncluded(ev *model.WebSocketEvent, channels map[string]bool) bool {
	if len(channels) == 0 {
		return true
	}
	_, ok := channels[eventChannelID(ev)]
	return ok
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func formatWatchEventJSON(ev *model.WebSocketEvent, ts time.Time) watchEventLine {
	return watchEventLine{
		Type:      string(ev.EventType()),
		Timestamp: ts.Format(time.RFC3339),
		ChannelID: eventChannelID(ev),
		TeamID:    eventTeamID(ev),
		UserID:    eventUserID(ev),
		Data:      mergedWatchEventData(ev),
	}
}

func mergedWatchEventData(ev *model.WebSocketEvent) map[string]any {
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

func eventChannelID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.ChannelId != "" {
		return b.ChannelId
	}
	return stringFromMap(ev.GetData(), "channel_id")
}

func eventTeamID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.TeamId != "" {
		return b.TeamId
	}
	return stringFromMap(ev.GetData(), "team_id")
}

func eventUserID(ev *model.WebSocketEvent) string {
	if b := ev.GetBroadcast(); b != nil && b.UserId != "" {
		return b.UserId
	}
	return stringFromMap(ev.GetData(), "user_id")
}

func formatWatchEventHuman(ev *model.WebSocketEvent, ts time.Time) string {
	data := mergedWatchEventData(ev)
	clock := ts.Local().Format("15:04:05")
	switch ev.EventType() {
	case model.WebsocketEventPosted:
		post := extractPost(data)
		user := post.UserID
		if user == "" {
			user = eventUserID(ev)
		}
		channel := post.ChannelID
		if channel == "" {
			channel = eventChannelID(ev)
		}
		return fmt.Sprintf("[%s] @%s in #%s: %s", clock, user, channel, shortPreview(post.Message, 60))
	case model.WebsocketEventReactionAdded:
		reaction := extractReaction(data)
		user := firstNonEmpty(reaction.UserID, eventUserID(ev))
		channel := firstNonEmpty(reaction.ChannelID, eventChannelID(ev))
		emoji := firstNonEmpty(reaction.EmojiName, stringFromMap(data, "emoji_name"))
		return fmt.Sprintf("[%s] @%s reacted :%s: in #%s", clock, user, emoji, channel)
	default:
		return fmt.Sprintf("[%s] %s in %s", clock, ev.EventType(), eventChannelID(ev))
	}
}

type watchPostPreview struct {
	Message   string
	UserID    string
	ChannelID string
}

func extractPost(data map[string]any) watchPostPreview {
	var post watchPostPreview
	decodeWatchPayload(data["post"], &struct {
		Message   *string `json:"message"`
		UserID    *string `json:"user_id"`
		ChannelID *string `json:"channel_id"`
	}{Message: &post.Message, UserID: &post.UserID, ChannelID: &post.ChannelID})
	post.Message = firstNonEmpty(post.Message, stringFromMap(data, "message"))
	post.UserID = firstNonEmpty(post.UserID, stringFromMap(data, "user_id"))
	post.ChannelID = firstNonEmpty(post.ChannelID, stringFromMap(data, "channel_id"))
	return post
}

type watchReactionPreview struct {
	EmojiName string
	UserID    string
	ChannelID string
}

func extractReaction(data map[string]any) watchReactionPreview {
	var reaction watchReactionPreview
	decodeWatchPayload(data["reaction"], &struct {
		EmojiName *string `json:"emoji_name"`
		UserID    *string `json:"user_id"`
		ChannelID *string `json:"channel_id"`
	}{EmojiName: &reaction.EmojiName, UserID: &reaction.UserID, ChannelID: &reaction.ChannelID})
	reaction.EmojiName = firstNonEmpty(reaction.EmojiName, stringFromMap(data, "emoji_name"))
	reaction.UserID = firstNonEmpty(reaction.UserID, stringFromMap(data, "user_id"))
	reaction.ChannelID = firstNonEmpty(reaction.ChannelID, stringFromMap(data, "channel_id"))
	return reaction
}

func decodeWatchPayload(value any, dst any) {
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

func stringFromMap(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	if s, ok := data[key].(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func shortPreview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
