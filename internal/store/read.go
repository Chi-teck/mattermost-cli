package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/mattermost/mattermost/server/public/model"
)

// ChannelPosts loads a channel's posts from the cache, reconstructed with props,
// file ids, file metadata, and reactions so that format.EnrichPost produces
// output identical to the live path. Returns the posts (unordered) and the
// oldest create_at present, so callers can decide whether the cache covers a
// requested time window.
func ChannelPosts(ctx context.Context, db *sql.DB, channelID string, includeDeleted bool) ([]*model.Post, int64, error) {
	query := `SELECT id, channel_id, user_id, root_id, message, create_at, update_at,
		edit_at, delete_at, reply_count, props_json, file_ids_json
		FROM posts WHERE channel_id = ?`
	if !includeDeleted {
		query += " AND delete_at = 0"
	}
	rows, err := db.QueryContext(ctx, query, channelID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	posts := []*model.Post{}
	byID := map[string]*model.Post{}
	var oldest int64
	for rows.Next() {
		p := &model.Post{}
		var props, fileIDs sql.NullString
		if err := rows.Scan(&p.Id, &p.ChannelId, &p.UserId, &p.RootId, &p.Message,
			&p.CreateAt, &p.UpdateAt, &p.EditAt, &p.DeleteAt, &p.ReplyCount,
			&props, &fileIDs); err != nil {
			return nil, 0, err
		}
		if props.Valid && props.String != "" {
			_ = json.Unmarshal([]byte(props.String), &p.Props)
		}
		if fileIDs.Valid && fileIDs.String != "" {
			var ids model.StringArray
			if json.Unmarshal([]byte(fileIDs.String), &ids) == nil {
				p.FileIds = ids
			}
		}
		p.Metadata = &model.PostMetadata{}
		posts = append(posts, p)
		byID[p.Id] = p
		if oldest == 0 || p.CreateAt < oldest {
			oldest = p.CreateAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(posts) == 0 {
		return posts, 0, nil
	}
	if err := attachFiles(ctx, db, channelID, byID); err != nil {
		return nil, 0, err
	}
	if err := attachReactions(ctx, db, channelID, byID); err != nil {
		return nil, 0, err
	}
	return posts, oldest, nil
}

func attachFiles(ctx context.Context, db *sql.DB, channelID string, byID map[string]*model.Post) error {
	rows, err := db.QueryContext(ctx, `
		SELECT f.id, f.post_id, f.name, f.size, f.mime_type, f.extension, f.width, f.height
		FROM files f JOIN posts p ON p.id = f.post_id WHERE p.channel_id = ?`, channelID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		f := &model.FileInfo{}
		if err := rows.Scan(&f.Id, &f.PostId, &f.Name, &f.Size, &f.MimeType, &f.Extension, &f.Width, &f.Height); err != nil {
			return err
		}
		if p := byID[f.PostId]; p != nil {
			p.Metadata.Files = append(p.Metadata.Files, f)
		}
	}
	return rows.Err()
}

func attachReactions(ctx context.Context, db *sql.DB, channelID string, byID map[string]*model.Post) error {
	rows, err := db.QueryContext(ctx, `
		SELECT r.post_id, r.user_id, r.emoji_name, r.create_at
		FROM reactions r JOIN posts p ON p.id = r.post_id WHERE p.channel_id = ?`, channelID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r := &model.Reaction{}
		if err := rows.Scan(&r.PostId, &r.UserId, &r.EmojiName, &r.CreateAt); err != nil {
			return err
		}
		if p := byID[r.PostId]; p != nil {
			p.Metadata.Reactions = append(p.Metadata.Reactions, r)
		}
	}
	return rows.Err()
}

// UsernamesByID returns username for each given user id present in the cache.
// Missing ids are omitted, so callers can detect incomplete coverage.
func UsernamesByID(ctx context.Context, db *sql.DB, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		var username string
		err := db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, id).Scan(&username)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = username
	}
	return out, nil
}

// ChannelByRefLocal resolves a channel id or slug to (id, display_name) from the
// cache. ok is false when not found (caller falls back to live).
func ChannelByRefLocal(ctx context.Context, db *sql.DB, id, name string) (string, string, bool) {
	var gotID, displayName string
	var err error
	if id != "" {
		err = db.QueryRowContext(ctx, `SELECT id, display_name FROM channels WHERE id = ?`, id).Scan(&gotID, &displayName)
	} else {
		err = db.QueryRowContext(ctx, `SELECT id, display_name FROM channels WHERE name = ? LIMIT 1`, name).Scan(&gotID, &displayName)
	}
	if err != nil {
		return "", "", false
	}
	return gotID, displayName, true
}
