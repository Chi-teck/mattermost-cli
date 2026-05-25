-- Read-side views: the contract `mm query` exposes to agents. They hide the
-- joins and reproduce the enrichment shape of internal/format.EnrichedPost so a
-- single SELECT replaces the old find-channel loops and shell post-processing.

-- v_channel: channel metadata + team name. display_name is already the resolved
-- form (see channels.display_name).
CREATE VIEW v_channel AS
SELECT
    c.id,
    c.name,
    c.display_name,
    c.type,
    t.display_name AS team,
    c.purpose,
    c.header,
    c.total_msg_count,
    c.last_post_at,
    c.create_at,
    c.update_at,
    c.delete_at
FROM channels c
LEFT JOIN teams t ON t.id = c.team_id;

-- v_unread: channels with unread messages. unread = total_msg_count - read
-- position, exactly the math in the live `unread` command.
CREATE VIEW v_unread AS
SELECT
    c.id,
    c.name,
    c.display_name,
    c.type,
    t.display_name AS team,
    (c.total_msg_count - cm.msg_count) AS unread_count,
    cm.mention_count,
    c.last_post_at,
    CASE WHEN c.last_post_at > 0
         THEN strftime('%Y-%m-%dT%H:%M:%SZ', c.last_post_at / 1000, 'unixepoch')
         ELSE '' END AS last_post_at_iso
FROM channels c
JOIN channel_members cm ON cm.channel_id = c.id
LEFT JOIN teams t ON t.id = c.team_id
WHERE c.delete_at = 0
  AND (c.total_msg_count - cm.msg_count) > 0;

-- v_post: enriched posts. Mirrors EnrichedPost fields; reactions/files are JSON
-- aggregates. is_bot/bot_name derive from Props.from_webhook, matching EnrichPost.
CREATE VIEW v_post AS
SELECT
    p.id,
    CASE WHEN p.root_id = '' THEN p.id ELSE p.root_id END AS thread_id,
    CASE WHEN p.root_id = '' THEN 0 ELSE 1 END AS is_reply,
    u.username AS author,
    p.message,
    CASE WHEN p.create_at > 0
         THEN strftime('%Y-%m-%dT%H:%M:%SZ', p.create_at / 1000, 'unixepoch')
         ELSE '' END AS created_at,
    p.channel_id,
    CASE WHEN p.file_ids_json IS NULL OR p.file_ids_json = ''
         THEN 0 ELSE json_array_length(p.file_ids_json) END AS file_count,
    CASE WHEN p.root_id = '' THEN p.reply_count ELSE 0 END AS reply_count,
    CASE WHEN json_extract(p.props_json, '$.from_webhook') = 'true' THEN 1 ELSE 0 END AS is_bot,
    json_extract(p.props_json, '$.override_username') AS bot_name,
    c.display_name AS channel,
    t.display_name AS team,
    (SELECT json_group_object(emoji, n) FROM (
        SELECT emoji_name AS emoji, COUNT(*) AS n
        FROM reactions r WHERE r.post_id = p.id GROUP BY emoji_name
    )) AS reactions,
    (SELECT json_group_array(json_object(
        'id', f.id, 'name', f.name, 'size', f.size,
        'mime_type', f.mime_type, 'extension', f.extension,
        'width', f.width, 'height', f.height))
     FROM files f WHERE f.post_id = p.id) AS files,
    p.root_id,
    p.user_id,
    p.create_at,
    p.delete_at
FROM posts p
LEFT JOIN users u ON u.id = p.user_id
LEFT JOIN channels c ON c.id = p.channel_id
LEFT JOIN teams t ON t.id = c.team_id;

-- v_thread: convenience for reading a thread (root + replies) by thread_id.
CREATE VIEW v_thread AS
SELECT * FROM v_post
WHERE thread_id IS NOT NULL;
