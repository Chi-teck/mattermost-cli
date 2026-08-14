package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/wsutil"
)

const (
	defaultWatchTypes = "posted,reaction_added,reaction_removed,channel_viewed,status_change,typing"
	// defaultWatchReconnectDelay matches syncd.reconnectDelay so the two
	// long-lived WebSocket consumers behave the same on an outage.
	defaultWatchReconnectDelay = 3 * time.Second
)

func init() {
	Register(newWatchCmd)
}

func newWatchCmd() *cobra.Command {
	var (
		channels       []string
		typesCSV       string
		limit          int
		timeout        time.Duration
		includeSelf    bool
		reconnectDelay time.Duration
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
			return runWatch(ctx, channels, include, limit, timeout, includeSelf, max(reconnectDelay, 0))
		},
	}
	cmd.Flags().StringArrayVar(&channels, "channel", nil, "Filter to a channel id (repeatable)")
	cmd.Flags().StringVar(&typesCSV, "types", defaultWatchTypes, "Comma-separated WebSocket event types to include")
	cmd.Flags().IntVar(&limit, "limit", 0, "Exit after N events (0 = no limit)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Exit after a duration of output inactivity (0 = no timeout)")
	cmd.Flags().BoolVar(&includeSelf, "include-self", false, "Also emit events caused by the authenticated user (excluded by default)")
	cmd.Flags().DurationVar(&reconnectDelay, "reconnect-delay", defaultWatchReconnectDelay, "Wait this long before re-establishing a dropped connection")
	return cmd
}

// watchEventLine is one line of `mm watch` output. UserID is the broadcast
// scope and is empty for post events; ActorID and Post are the decoded
// convenience fields consumers should prefer. Data keeps the raw payload,
// nested JSON strings and all, so existing consumers keep working.
type watchEventLine struct {
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	ChannelID string         `json:"channel_id"`
	TeamID    string         `json:"team_id"`
	UserID    string         `json:"user_id"`
	ActorID   string         `json:"actor_id,omitempty"`
	Post      *model.Post    `json:"post,omitempty"`
	Data      map[string]any `json:"data"`
}

// runWatch streams events until ctx is cancelled, --limit is reached, or
// --timeout elapses with no output. A dropped connection is never fatal: it
// reconnects forever, mirroring the sync daemon's WebSocket loop.
func runWatch(ctx context.Context, channels []string, include map[model.WebsocketEventType]bool, limit int, timeout time.Duration, includeSelf bool, reconnectDelay time.Duration) error {
	lc, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	channelSet := stringSet(channels)
	received := 0

	// --timeout cancels the whole stream rather than just the current
	// connection, so silence counts while we are disconnected too. AfterFunc is
	// what makes that cheap: rearming on each emitted event is a plain Reset.
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	var idle *time.Timer
	if timeout > 0 {
		idle = time.AfterFunc(timeout, stopStream)
		defer idle.Stop()
	}
	resetIdle := func() {
		if idle != nil {
			idle.Reset(timeout)
		}
	}

	// consume streams from one connection. It reports done=true when watching is
	// over (cancelled, timed out, limit reached) and false when the connection
	// merely dropped and should be re-established.
	consume := func(client *model.WebSocketClient) (bool, error) {
		for {
			select {
			case <-streamCtx.Done():
				return true, nil
			case <-client.PingTimeoutChannel:
				fmt.Fprintln(os.Stderr, "warning: websocket ping timed out; reconnecting")
				return false, nil
			case ev, ok := <-client.EventChannel:
				if !ok {
					fmt.Fprintln(os.Stderr, "warning: websocket disconnected; reconnecting")
					return false, nil
				}
				if ev == nil || !eventTypeIncluded(ev.EventType(), include) || !eventChannelIncluded(ev, channelSet) {
					continue
				}

				data := wsutil.EventData(ev)
				if !includeSelf && eventActorID(ev, data) == lc.Me.Id {
					continue
				}

				now := time.Now().UTC()
				if Globals.Human {
					fmt.Fprintln(out, formatWatchEventHuman(ev, data, now))
				} else {
					if err := enc.Encode(formatWatchEventJSON(ev, data, now)); err != nil {
						return true, errs.Errorf(errs.CodeGeneric, "write event: %s", err.Error())
					}
				}
				if err := out.Flush(); err != nil {
					return true, errs.Errorf(errs.CodeGeneric, "flush event: %s", err.Error())
				}
				resetIdle()
				received++
				if limit > 0 && received >= limit {
					return true, nil
				}
			}
		}
	}

	for client, err := range wsutil.Connections(streamCtx, lc.Cfg.URL, lc.Cfg.Token, reconnectDelay) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: connect websocket: %s; retrying in %s\n", err.Error(), reconnectDelay)
			continue
		}
		done, err := consume(client)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return nil
}

// eventActorID returns the id of the user who caused the event. Post and
// reaction payloads carry the actor, while the broadcast scope does not — it is
// empty for `posted`, so wsutil.UserID alone would match nothing.
func eventActorID(ev *model.WebSocketEvent, data map[string]any) string {
	switch ev.EventType() {
	case model.WebsocketEventPosted, model.WebsocketEventPostEdited, model.WebsocketEventPostDeleted:
		return extractPost(data).UserID
	case model.WebsocketEventReactionAdded, model.WebsocketEventReactionRemoved:
		return extractReaction(data).UserID
	default:
		return wsutil.UserID(ev)
	}
}

func formatWatchEventJSON(ev *model.WebSocketEvent, data map[string]any, ts time.Time) watchEventLine {
	return watchEventLine{
		Type:      string(ev.EventType()),
		Timestamp: ts.Format(time.RFC3339),
		ChannelID: wsutil.ChannelID(ev),
		TeamID:    wsutil.TeamID(ev),
		UserID:    wsutil.UserID(ev),
		ActorID:   eventActorID(ev, data),
		Post:      extractPostPayload(data),
		Data:      data,
	}
}

// extractPostPayload decodes data["post"] into a real post so consumers can read
// .post.root_id or .post.message without parsing the nested JSON string
// Mattermost sends. Returns nil when the event carries no post.
func extractPostPayload(data map[string]any) *model.Post {
	raw, ok := data["post"]
	if !ok {
		return nil
	}
	var post model.Post
	wsutil.DecodePayload(raw, &post)
	if post.Id == "" {
		return nil
	}
	return &post
}

func formatWatchEventHuman(ev *model.WebSocketEvent, data map[string]any, ts time.Time) string {
	clock := ts.Local().Format("15:04:05")
	switch ev.EventType() {
	case model.WebsocketEventPosted:
		post := extractPost(data)
		user := post.UserID
		if user == "" {
			user = wsutil.UserID(ev)
		}
		channel := post.ChannelID
		if channel == "" {
			channel = wsutil.ChannelID(ev)
		}
		return fmt.Sprintf("[%s] @%s in #%s: %s", clock, user, channel, shortPreview(post.Message, 60))
	case model.WebsocketEventReactionAdded:
		reaction := extractReaction(data)
		user := wsutil.FirstNonEmpty(reaction.UserID, wsutil.UserID(ev))
		channel := wsutil.FirstNonEmpty(reaction.ChannelID, wsutil.ChannelID(ev))
		emoji := wsutil.FirstNonEmpty(reaction.EmojiName, wsutil.StringFromMap(data, "emoji_name"))
		return fmt.Sprintf("[%s] @%s reacted :%s: in #%s", clock, user, emoji, channel)
	default:
		return fmt.Sprintf("[%s] %s in %s", clock, ev.EventType(), wsutil.ChannelID(ev))
	}
}

type watchPostPreview struct {
	Message   string
	UserID    string
	ChannelID string
}

func extractPost(data map[string]any) watchPostPreview {
	var post watchPostPreview
	wsutil.DecodePayload(data["post"], &struct {
		Message   *string `json:"message"`
		UserID    *string `json:"user_id"`
		ChannelID *string `json:"channel_id"`
	}{Message: &post.Message, UserID: &post.UserID, ChannelID: &post.ChannelID})
	post.Message = wsutil.FirstNonEmpty(post.Message, wsutil.StringFromMap(data, "message"))
	post.UserID = wsutil.FirstNonEmpty(post.UserID, wsutil.StringFromMap(data, "user_id"))
	post.ChannelID = wsutil.FirstNonEmpty(post.ChannelID, wsutil.StringFromMap(data, "channel_id"))
	return post
}

type watchReactionPreview struct {
	EmojiName string
	UserID    string
	ChannelID string
}

func extractReaction(data map[string]any) watchReactionPreview {
	var reaction watchReactionPreview
	wsutil.DecodePayload(data["reaction"], &struct {
		EmojiName *string `json:"emoji_name"`
		UserID    *string `json:"user_id"`
		ChannelID *string `json:"channel_id"`
	}{EmojiName: &reaction.EmojiName, UserID: &reaction.UserID, ChannelID: &reaction.ChannelID})
	reaction.EmojiName = wsutil.FirstNonEmpty(reaction.EmojiName, wsutil.StringFromMap(data, "emoji_name"))
	reaction.UserID = wsutil.FirstNonEmpty(reaction.UserID, wsutil.StringFromMap(data, "user_id"))
	reaction.ChannelID = wsutil.FirstNonEmpty(reaction.ChannelID, wsutil.StringFromMap(data, "channel_id"))
	return reaction
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
	_, ok := channels[wsutil.ChannelID(ev)]
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
