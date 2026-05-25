package syncd

import (
	"context"
	"database/sql"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
	"github.com/ayusavin/mattermost-cli/internal/wsutil"
)

// handleEvent applies a single WebSocket event to the local store. Unhandled
// event types are ignored (reconciliation covers membership/channel changes).
func (d *Daemon) handleEvent(ctx context.Context, ev *model.WebSocketEvent) error {
	data := wsutil.EventData(ev)
	var err error
	switch ev.EventType() {
	case model.WebsocketEventPosted:
		err = d.handlePosted(ctx, data)
	case model.WebsocketEventPostEdited:
		err = d.handlePostEdited(ctx, data)
	case model.WebsocketEventPostDeleted:
		err = d.handlePostDeleted(ctx, data)
	case model.WebsocketEventReactionAdded:
		err = d.handleReaction(ctx, data, true)
	case model.WebsocketEventReactionRemoved:
		err = d.handleReaction(ctx, data, false)
	case model.WebsocketEventMultipleChannelsViewed:
		err = d.handleChannelsViewed(ctx, data)
	case model.WebsocketEventStatusChange:
		err = d.handleStatusChange(ctx, data)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return store.SetLastEvent(ctx, d.db, time.Now().UnixMilli())
}

func (d *Daemon) handlePosted(ctx context.Context, data map[string]any) error {
	p := decodePost(data["post"])
	if p == nil {
		return nil
	}
	if err := store.WithTx(ctx, d.db, func(tx *sql.Tx) error {
		if err := store.UpsertPost(ctx, tx, p); err != nil {
			return err
		}
		return store.BumpChannelOnPost(ctx, tx, p.ChannelId, p.CreateAt)
	}); err != nil {
		return err
	}
	if p.UserId != "" {
		return d.ensureUsers(ctx, []string{p.UserId})
	}
	return nil
}

func (d *Daemon) handlePostEdited(ctx context.Context, data map[string]any) error {
	p := decodePost(data["post"])
	if p == nil {
		return nil
	}
	return store.UpsertPost(ctx, d.db, p)
}

func (d *Daemon) handlePostDeleted(ctx context.Context, data map[string]any) error {
	p := decodePost(data["post"])
	if p == nil {
		return nil
	}
	deleteAt := p.DeleteAt
	if deleteAt == 0 {
		deleteAt = time.Now().UnixMilli()
	}
	return store.MarkPostDeleted(ctx, d.db, p.Id, deleteAt)
}

func (d *Daemon) handleReaction(ctx context.Context, data map[string]any, added bool) error {
	var r model.Reaction
	wsutil.DecodePayload(data["reaction"], &r)
	if r.PostId == "" || r.EmojiName == "" {
		return nil
	}
	if added {
		return store.UpsertReaction(ctx, d.db, &r)
	}
	return store.DeleteReaction(ctx, d.db, &r)
}

// handleChannelsViewed clears unread for the channels the user just viewed. The
// modern event carries a channel_times map ({channelID: timestamp}); older
// single-channel payloads carry channel_id.
func (d *Daemon) handleChannelsViewed(ctx context.Context, data map[string]any) error {
	now := time.Now().UnixMilli()
	if times, ok := data["channel_times"].(map[string]any); ok {
		for channelID := range times {
			if channelID == "" {
				continue
			}
			if err := store.MarkChannelViewed(ctx, d.db, channelID, d.me.Id, now); err != nil {
				return err
			}
		}
		return nil
	}
	if channelID := wsutil.StringFromMap(data, "channel_id"); channelID != "" {
		return store.MarkChannelViewed(ctx, d.db, channelID, d.me.Id, now)
	}
	return nil
}

func (d *Daemon) handleStatusChange(ctx context.Context, data map[string]any) error {
	userID := wsutil.StringFromMap(data, "user_id")
	if userID == "" {
		return nil
	}
	return store.UpsertStatus(ctx, d.db, &model.Status{
		UserId:         userID,
		Status:         wsutil.StringFromMap(data, "status"),
		LastActivityAt: time.Now().UnixMilli(),
	})
}

// decodePost decodes a WebSocket post payload (a JSON string) into a *model.Post,
// returning nil if it can't be decoded or has no id.
func decodePost(raw any) *model.Post {
	var p model.Post
	wsutil.DecodePayload(raw, &p)
	if p.Id == "" {
		return nil
	}
	return &p
}
