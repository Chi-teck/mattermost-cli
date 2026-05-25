package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/mattermost/mattermost/server/public/model"
)

// Execer is satisfied by both *sql.DB and *sql.Tx so write helpers work inside
// or outside a transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx runs fn inside a transaction, committing on success and rolling back on
// error. The sync daemon batches a burst of WebSocket events into one tx.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// UpsertTeam inserts or updates a team row.
func UpsertTeam(ctx context.Context, ex Execer, t *model.Team) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO teams(id, name, display_name, type, delete_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, display_name=excluded.display_name,
			type=excluded.type, delete_at=excluded.delete_at`,
		t.Id, t.Name, t.DisplayName, string(t.Type), t.DeleteAt)
	return err
}

// UpsertChannel inserts or updates a channel. displayName is the resolved
// display name the daemon computes (DM/group -> usernames); for named channels
// pass ch.DisplayName.
func UpsertChannel(ctx context.Context, ex Execer, ch *model.Channel, displayName string) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO channels(id, team_id, name, display_name, type, purpose, header,
			total_msg_count, last_post_at, create_at, update_at, delete_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			team_id=excluded.team_id, name=excluded.name, display_name=excluded.display_name,
			type=excluded.type, purpose=excluded.purpose, header=excluded.header,
			total_msg_count=excluded.total_msg_count, last_post_at=excluded.last_post_at,
			create_at=excluded.create_at, update_at=excluded.update_at, delete_at=excluded.delete_at`,
		ch.Id, ch.TeamId, ch.Name, displayName, string(ch.Type), ch.Purpose, ch.Header,
		ch.TotalMsgCount, ch.LastPostAt, ch.CreateAt, ch.UpdateAt, ch.DeleteAt)
	return err
}

// UpsertChannelMember stores the user's read state for a channel.
func UpsertChannelMember(ctx context.Context, ex Execer, m *model.ChannelMember) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO channel_members(channel_id, user_id, msg_count, mention_count, last_viewed_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(channel_id, user_id) DO UPDATE SET
			msg_count=excluded.msg_count, mention_count=excluded.mention_count,
			last_viewed_at=excluded.last_viewed_at`,
		m.ChannelId, m.UserId, m.MsgCount, m.MentionCount, m.LastViewedAt)
	return err
}

// UpsertUser inserts or updates a user.
func UpsertUser(ctx context.Context, ex Execer, u *model.User) error {
	isBot := 0
	if u.IsBot {
		isBot = 1
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO users(id, username, nickname, first_name, last_name, position, is_bot, delete_at, update_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			username=excluded.username, nickname=excluded.nickname,
			first_name=excluded.first_name, last_name=excluded.last_name,
			position=excluded.position, is_bot=excluded.is_bot,
			delete_at=excluded.delete_at, update_at=excluded.update_at`,
		u.Id, u.Username, u.Nickname, u.FirstName, u.LastName, u.Position, isBot, u.DeleteAt, u.UpdateAt)
	return err
}

// UpsertStatus stores a user's presence status.
func UpsertStatus(ctx context.Context, ex Execer, s *model.Status) error {
	manual := 0
	if s.Manual {
		manual = 1
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO statuses(user_id, status, manual, last_activity_at)
		VALUES (?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
			status=excluded.status, manual=excluded.manual, last_activity_at=excluded.last_activity_at`,
		s.UserId, s.Status, manual, s.LastActivityAt)
	return err
}

// UpsertPost inserts or updates a post. When the post carries metadata
// (backfill, thread fetches) its files and reactions are synced too.
func UpsertPost(ctx context.Context, ex Execer, p *model.Post) error {
	propsJSON := marshalJSON(p.Props)
	fileIDsJSON := marshalJSON([]string(p.FileIds))
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO posts(id, channel_id, user_id, root_id, message,
			create_at, update_at, edit_at, delete_at, reply_count, props_json, file_ids_json)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			channel_id=excluded.channel_id, user_id=excluded.user_id, root_id=excluded.root_id,
			message=excluded.message, update_at=excluded.update_at, edit_at=excluded.edit_at,
			delete_at=excluded.delete_at, reply_count=excluded.reply_count,
			props_json=excluded.props_json, file_ids_json=excluded.file_ids_json`,
		p.Id, p.ChannelId, p.UserId, p.RootId, p.Message,
		p.CreateAt, p.UpdateAt, p.EditAt, p.DeleteAt, p.ReplyCount, propsJSON, fileIDsJSON); err != nil {
		return err
	}
	if p.Metadata == nil {
		return nil
	}
	for _, f := range p.Metadata.Files {
		if f == nil {
			continue
		}
		if err := UpsertFile(ctx, ex, f); err != nil {
			return err
		}
	}
	if len(p.Metadata.Reactions) > 0 {
		if _, err := ex.ExecContext(ctx, `DELETE FROM reactions WHERE post_id=?`, p.Id); err != nil {
			return err
		}
		for _, r := range p.Metadata.Reactions {
			if r == nil {
				continue
			}
			if err := UpsertReaction(ctx, ex, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarkPostDeleted soft-deletes a post (Mattermost keeps the total message count).
func MarkPostDeleted(ctx context.Context, ex Execer, postID string, deleteAt int64) error {
	_, err := ex.ExecContext(ctx, `UPDATE posts SET delete_at=? WHERE id=?`, deleteAt, postID)
	return err
}

// UpsertFile stores file metadata (never bytes).
func UpsertFile(ctx context.Context, ex Execer, f *model.FileInfo) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO files(id, post_id, name, size, mime_type, extension, width, height)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			post_id=excluded.post_id, name=excluded.name, size=excluded.size,
			mime_type=excluded.mime_type, extension=excluded.extension,
			width=excluded.width, height=excluded.height`,
		f.Id, f.PostId, f.Name, f.Size, f.MimeType, f.Extension, f.Width, f.Height)
	return err
}

// UpsertReaction adds a reaction.
func UpsertReaction(ctx context.Context, ex Execer, r *model.Reaction) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO reactions(post_id, user_id, emoji_name, create_at)
		VALUES (?,?,?,?)
		ON CONFLICT(post_id, user_id, emoji_name) DO UPDATE SET create_at=excluded.create_at`,
		r.PostId, r.UserId, r.EmojiName, r.CreateAt)
	return err
}

// DeleteReaction removes a reaction.
func DeleteReaction(ctx context.Context, ex Execer, r *model.Reaction) error {
	_, err := ex.ExecContext(ctx,
		`DELETE FROM reactions WHERE post_id=? AND user_id=? AND emoji_name=?`,
		r.PostId, r.UserId, r.EmojiName)
	return err
}

// BumpChannelOnPost advances a channel's total message count and last_post_at
// when a new post arrives over the WebSocket. No-op if the channel is unknown.
func BumpChannelOnPost(ctx context.Context, ex Execer, channelID string, postCreateAt int64) error {
	_, err := ex.ExecContext(ctx, `
		UPDATE channels
		SET total_msg_count = total_msg_count + 1,
			last_post_at = CASE WHEN ? > last_post_at THEN ? ELSE last_post_at END
		WHERE id = ?`, postCreateAt, postCreateAt, channelID)
	return err
}

// MarkChannelViewed clears unread for a channel: the read position catches up to
// the total and mentions reset. Mirrors what the server does on view.
func MarkChannelViewed(ctx context.Context, ex Execer, channelID, userID string, viewedAt int64) error {
	_, err := ex.ExecContext(ctx, `
		UPDATE channel_members
		SET last_viewed_at = ?,
			msg_count = (SELECT total_msg_count FROM channels WHERE id = ?),
			mention_count = 0
		WHERE channel_id = ? AND user_id = ?`,
		viewedAt, channelID, channelID, userID)
	return err
}

// SetChannelCursor records the oldest/newest post timestamps synced for a channel.
func SetChannelCursor(ctx context.Context, ex Execer, channelID string, oldest, newest int64) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO channel_cursors(channel_id, oldest_synced_at, newest_synced_at)
		VALUES (?,?,?)
		ON CONFLICT(channel_id) DO UPDATE SET
			oldest_synced_at = MIN(channel_cursors.oldest_synced_at, excluded.oldest_synced_at),
			newest_synced_at = MAX(channel_cursors.newest_synced_at, excluded.newest_synced_at)`,
		channelID, oldest, newest)
	return err
}

// NewestPostAt returns the newest synced post timestamp for a channel, or 0.
func NewestPostAt(ctx context.Context, ex Execer, channelID string) (int64, error) {
	var ts sql.NullInt64
	err := ex.QueryRowContext(ctx,
		`SELECT newest_synced_at FROM channel_cursors WHERE channel_id=?`, channelID).Scan(&ts)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return ts.Int64, nil
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
