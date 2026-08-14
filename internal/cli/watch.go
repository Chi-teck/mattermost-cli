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

const defaultWatchTypes = "posted,reaction_added,reaction_removed,channel_viewed,status_change,typing"

func init() {
	Register(newWatchCmd)
}

func newWatchCmd() *cobra.Command {
	var (
		channels    []string
		typesCSV    string
		limit       int
		timeout     time.Duration
		includeSelf bool
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
			return runWatch(ctx, channels, include, limit, timeout, includeSelf)
		},
	}
	cmd.Flags().StringArrayVar(&channels, "channel", nil, "Filter to a channel id (repeatable)")
	cmd.Flags().StringVar(&typesCSV, "types", defaultWatchTypes, "Comma-separated WebSocket event types to include")
	cmd.Flags().IntVar(&limit, "limit", 0, "Exit after N events (0 = no limit)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Exit after a duration of output inactivity (0 = no timeout)")
	cmd.Flags().BoolVar(&includeSelf, "include-self", false, "Also emit events caused by the authenticated user (excluded by default)")
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

func runWatch(ctx context.Context, channels []string, include map[model.WebsocketEventType]bool, limit int, timeout time.Duration, includeSelf bool) error {
	lc, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	client, err := wsutil.Connect(lc.Cfg.URL, lc.Cfg.Token)
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
		next, err := wsutil.Connect(lc.Cfg.URL, lc.Cfg.Token)
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

			data := wsutil.EventData(ev)
			if !includeSelf && eventActorID(ev, data) == lc.Me.Id {
				continue
			}

			now := time.Now().UTC()
			if Globals.Human {
				fmt.Fprintln(out, formatWatchEventHuman(ev, data, now))
			} else {
				if err := enc.Encode(formatWatchEventJSON(ev, data, now)); err != nil {
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
		Data:      data,
	}
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
