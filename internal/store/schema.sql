-- Local-first cache schema for mm. Owned (written) only by the sync daemon;
-- read-only by `mm query`. Timestamps are Mattermost epoch-milliseconds.
--
-- Forward-only: never edit this file's statements after release; add a new
-- migration string in migrate.go instead.

CREATE TABLE account (
    user_id    TEXT PRIMARY KEY,
    host       TEXT NOT NULL DEFAULT '',
    server_url TEXT NOT NULL DEFAULT '',
    username   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE teams (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT '',
    delete_at    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE channels (
    id              TEXT PRIMARY KEY,
    team_id         TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    -- Final display name: for DMs/group DMs the daemon stores the resolved
    -- "@user" / "@a, @b" form (it has all users locally); named channels keep
    -- their Mattermost display name. SQL views read this verbatim.
    display_name    TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL DEFAULT '',  -- O / P / D / G
    purpose         TEXT NOT NULL DEFAULT '',
    header          TEXT NOT NULL DEFAULT '',
    total_msg_count INTEGER NOT NULL DEFAULT 0,
    last_post_at    INTEGER NOT NULL DEFAULT 0,
    create_at       INTEGER NOT NULL DEFAULT 0,
    update_at       INTEGER NOT NULL DEFAULT 0,
    delete_at       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_channels_team ON channels(team_id);
CREATE INDEX idx_channels_name ON channels(name);
CREATE INDEX idx_channels_lastpost ON channels(last_post_at DESC);

CREATE TABLE channel_members (
    channel_id     TEXT NOT NULL,
    user_id        TEXT NOT NULL,
    msg_count      INTEGER NOT NULL DEFAULT 0,  -- read position
    mention_count  INTEGER NOT NULL DEFAULT 0,
    last_viewed_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX idx_cm_user ON channel_members(user_id);

CREATE TABLE users (
    id         TEXT PRIMARY KEY,
    username   TEXT NOT NULL DEFAULT '',
    nickname   TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    last_name  TEXT NOT NULL DEFAULT '',
    position   TEXT NOT NULL DEFAULT '',
    is_bot     INTEGER NOT NULL DEFAULT 0,
    delete_at  INTEGER NOT NULL DEFAULT 0,
    update_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_users_username ON users(username);

CREATE TABLE statuses (
    user_id          TEXT PRIMARY KEY,
    status           TEXT NOT NULL DEFAULT '',
    manual           INTEGER NOT NULL DEFAULT 0,
    last_activity_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE posts (
    id            TEXT PRIMARY KEY,
    channel_id    TEXT NOT NULL DEFAULT '',
    user_id       TEXT NOT NULL DEFAULT '',
    root_id       TEXT NOT NULL DEFAULT '',  -- '' => root post
    message       TEXT NOT NULL DEFAULT '',
    create_at     INTEGER NOT NULL DEFAULT 0,
    update_at     INTEGER NOT NULL DEFAULT 0,
    edit_at       INTEGER NOT NULL DEFAULT 0,
    delete_at     INTEGER NOT NULL DEFAULT 0,
    reply_count   INTEGER NOT NULL DEFAULT 0,
    props_json    TEXT,                       -- raw Post.Props (bot/webhook/attachments)
    file_ids_json TEXT                        -- raw Post.FileIds array
);
CREATE INDEX idx_posts_channel_create ON posts(channel_id, create_at DESC);
CREATE INDEX idx_posts_root ON posts(root_id);
CREATE INDEX idx_posts_user ON posts(user_id);

-- Full-text index over message text. External-content table mirrors posts via
-- the implicit rowid; triggers keep it in sync.
CREATE VIRTUAL TABLE posts_fts USING fts5(message, content='posts', content_rowid='rowid');

CREATE TRIGGER posts_ai AFTER INSERT ON posts BEGIN
    INSERT INTO posts_fts(rowid, message) VALUES (new.rowid, new.message);
END;
CREATE TRIGGER posts_ad AFTER DELETE ON posts BEGIN
    INSERT INTO posts_fts(posts_fts, rowid, message) VALUES ('delete', old.rowid, old.message);
END;
CREATE TRIGGER posts_au AFTER UPDATE ON posts BEGIN
    INSERT INTO posts_fts(posts_fts, rowid, message) VALUES ('delete', old.rowid, old.message);
    INSERT INTO posts_fts(rowid, message) VALUES (new.rowid, new.message);
END;

CREATE TABLE reactions (
    post_id    TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    emoji_name TEXT NOT NULL,
    create_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (post_id, user_id, emoji_name)
);
CREATE INDEX idx_reactions_post ON reactions(post_id);

CREATE TABLE files (
    id        TEXT PRIMARY KEY,
    post_id   TEXT NOT NULL DEFAULT '',
    name      TEXT NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    mime_type TEXT NOT NULL DEFAULT '',
    extension TEXT NOT NULL DEFAULT '',
    width     INTEGER NOT NULL DEFAULT 0,
    height    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_files_post ON files(post_id);

CREATE TABLE channel_cursors (
    channel_id       TEXT PRIMARY KEY,
    oldest_synced_at INTEGER NOT NULL DEFAULT 0,
    newest_synced_at INTEGER NOT NULL DEFAULT 0
);

-- Single-row daemon heartbeat / freshness. Readers query this to decide whether
-- local data is fresh enough or to fall back to the live API.
CREATE TABLE sync_state (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    daemon_pid        INTEGER NOT NULL DEFAULT 0,
    started_at        INTEGER NOT NULL DEFAULT 0,
    heartbeat_at      INTEGER NOT NULL DEFAULT 0,
    last_event_at     INTEGER NOT NULL DEFAULT 0,
    last_reconcile_at INTEGER NOT NULL DEFAULT 0,
    ws_connected      INTEGER NOT NULL DEFAULT 0,
    backfill_done     INTEGER NOT NULL DEFAULT 0,
    schema_version    INTEGER NOT NULL DEFAULT 0,
    server_url        TEXT NOT NULL DEFAULT '',
    me_user_id        TEXT NOT NULL DEFAULT '',
    last_error        TEXT NOT NULL DEFAULT ''
);
