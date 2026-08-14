# Command Reference

Full reference for every `mm` command. JSON by default, `--human` for
markdown.

Global flags (any position):
- `--human` — markdown output instead of JSON
- `--team TEXT` — restrict to a specific team
- `--debug` — verbose error output

Exit codes: `0` OK · `1` generic · `2` auth expired (run `mm login`) ·
`3` rate limited.

---

## Read commands

### `overview`

The starting point. Returns `{ mentions, unread, active_channels }` in one
call. Each channel has a `ref` you can pass to `mm messages`.

```
mm overview [--since 6h]
```

### `messages`

Read messages from a channel.

```
mm messages <ref> [--since 1h] [--limit 30] [--threads]
```

- `<ref>` — channel name, `@username` (DM), `~name`, or channel ID (group DMs)
- `--since` — `1h`, `2d`, `today`, `2025-03-05`, or `0` for all time
- `--limit` — max messages, capped at 200
- `--threads` — group by thread (root + reply count + last reply)

### `thread`

Read a thread conversation.

```
mm thread <post_id> [--limit 10] [--since 1h]
```

- `<post_id>` accepts any post ID in the thread (root or reply), or a permalink
- `--limit 0` — all replies (default: root + 9)
- `--since` — filter replies; root is always included for context

### `mentions`

Posts @-mentioning you. Reply-mentions include a `root` field with the
original message — no follow-up call needed.

```
mm mentions [--since 1d] [--limit 30]
```

### `search`

Full-text search across all your teams.

```
mm search "<query>" [--limit 30]
```

Supports Mattermost search modifiers: `from:user`, `in:channel`,
`before:`, `after:`, `on:` with `YYYY-MM-DD` dates.

### `find-channel`

Search channels you have access to (public + private you're already in).
Matches on channel name, display name, purpose, and header.

```
mm find-channel <term> [--team <name|id>] [--type any|public|private] [--limit 50]
```

Returns rows of `{id, name, display_name, type, purpose, header, team, team_id, ref}`.
Pass `ref` directly to `mm messages` or `mm join`.

### `search-user`

Fuzzy lookup for users. Use this when you don't know the exact username —
matches username, full name, nickname, and email fragments.

```
mm search-user <term> [--team <name|id>] [--limit 25] [--include-inactive]
```

Returns rows of `{id, username, full_name, nickname, position, email, ref, is_bot, deleted}`.
The leading `@` on `<term>` is stripped automatically.

### `channel`

Single-channel summary.

```
mm channel <ref>
```

### `channels`

List channels you belong to.

```
mm channels [--type public|private|dm|group] [--since 6h]
```

### `unread`

Channels with unread messages.

```
mm unread [--include-muted]
```

### `pinned`

Pinned posts in a channel.

```
mm pinned <ref> [--limit 10]
```

### `members`

Channel members with online status (online sorted first).

```
mm members <ref>
```

### `user`

User profile, status, timezone.

```
mm user @username
```

`@` prefix is optional.

### `watch`

Stream Mattermost WebSocket events to stdout (line-delimited JSON). Useful
for tail-style monitoring.

```
mm watch
```

Ctrl-C to stop.

### `download`

Fetch a file attached to a Mattermost post. File IDs come from the `files[].id`
field returned by `mm messages` / `mm thread` / `mm mentions` / `mm search`.

```
mm download <file-id>                         # save to cwd using original name
mm download <file-id> --output path/dir/      # save into a directory
mm download <file-id> --output ./renamed.png  # save under an explicit name
mm download <file-id> --output -              # stream raw bytes to stdout
mm download <file-id> --force                 # overwrite an existing file
```

JSON output (when not streaming): `{id, name, size, mime_type, extension,
width, height, path}`. Refuses to overwrite an existing path unless `--force`
is passed.

---

## Local cache commands

A background daemon mirrors Mattermost into a local SQLite cache and keeps it
live over the WebSocket. `mm query` reads that cache with SQL (instant, no
network). When the daemon is running and its backfill is complete, `find-channel`,
`unread`, `channels`, and `messages` answer from the cache automatically (output
identical to live) and fall back to the live API otherwise. `MM_NO_DAEMON=1`
forces the live path; `MM_CACHE_PATH` overrides the cache dir
(default `os.UserCacheDir()/mm`).

Backfill keeps only a recent window of posts per channel (sized to cover unread);
unread *counts* are always exact regardless. Pull deeper history into the cache
on demand with `mm history`.

### `sync`

Manage the daemon. Not started automatically.

```
mm sync start     # backfill, then realtime-sync in the background (no-op if already running)
mm sync status    # {running, ipc_reachable, ws_connected, backfill_done, heartbeat_age_ms, ...}
mm sync stop      # SIGTERM + WAL checkpoint
mm sync logs      # daemon log path + tail
```

`status` is how you check freshness before trusting `mm query` results.

### `query`

Run a read-only SQL statement against the local cache and return JSON rows
(`--human` for a table). Only `SELECT` / `WITH` / `EXPLAIN` are allowed;
`ATTACH`, `PRAGMA` writes, and any mutation are rejected. Row cap 5000,
statement timeout 10s.

```
mm query "<SQL>"
mm query --schema    # print table/view definitions (discover the surface)
```

Enriched views (the contract — prefer these over raw tables):
- `v_post` — `id, thread_id, is_reply, author, message, created_at, channel_id,
  channel, team, file_count, reply_count, is_bot, bot_name, reactions (JSON),
  files (JSON), root_id, user_id, create_at, delete_at`
- `v_channel` — `id, name, display_name, type, team, purpose, header,
  total_msg_count, last_post_at, ...`
- `v_unread` — `id, name, display_name, type, team, unread_count, mention_count,
  last_post_at, last_post_at_iso`
- `v_thread` — posts grouped by `thread_id`

Full-text search over message text (FTS5):
```
mm query "SELECT p.id, p.message FROM posts p
          JOIN posts_fts f ON p.rowid = f.rowid
          WHERE posts_fts MATCH 'deploy' LIMIT 20"
```

Freshness: `mm query "SELECT * FROM sync_state"` returns the daemon's heartbeat,
`ws_connected`, `backfill_done`, and last reconcile time.

One SQL `SELECT` replaces looping `find-channel` over guessed names, escalating
`--since`, and piping output to `grep`/`python`.

### `new`

List posts you haven't processed yet — newer than the agent's "seen" cursor,
across all channels, excluding your own posts. Decoupled from Mattermost
read/unread (querying never marks anything read; the cursor is your own
processing watermark).

```
mm new [--limit 100] [--since 1d]
```

Output is the `v_post` shape (same as `mm query` over `v_post`). On a fresh
cache the cursor is 0, so the first `mm new` returns the whole window — run
`mm seen` once to set a baseline.

### `seen`

Advance the processing cursor (used by `mm new`). Writes go through the daemon,
so it must be running.

```
mm seen [--at <epoch-ms>]    # default: now
```

### `history`

Load older posts for a channel into the cache (backfill only keeps a recent
window). Pages backwards via the daemon; afterwards `mm query` / `mm messages`
see the deeper history locally.

```
mm history <channel-ref> [--limit 500]
```

Returns `{channel_id, channel, loaded}`. `loaded` is page-granular (rounded up
to the 200-post page size).

---

## Write commands

All write commands return the created/updated resource as JSON.

### `post`

```
mm post <ref> -m "body"
echo "body" | mm post <ref> --read
mm post <ref> -m "body" --file ./a.log --file ./b.png
```

### `reply`

```
mm reply <post_id> -m "body"
echo "body" | mm reply <post_id> --read
mm reply <post_id> -m "body" --file ./a.log
```

### `dm`

Send a direct message. Opens (or reuses) the DM channel.

```
mm dm @username -m "body"
mm dm @username --file ./report.pdf
```

Self-DM is allowed (`mm dm @<you>`).

### Attachments (`post` / `reply` / `dm`)

`--file PATH` attaches a local file; repeat it for up to 10 files per post.
The message body becomes optional once at least one `--file` is given.

```
mm post <ref> --file ./report.pdf                            # no -m needed
generate-report | mm post <ref> --file - --filename out.txt  # bytes from stdin
```

`--file -` reads the attachment from stdin and requires `--filename` to name
it, so it must be the only attachment. It cannot be combined with `--read`,
which also consumes stdin.

Files are uploaded to the target channel before the post is created. If any
upload fails, no post is made; the files already uploaded stay unattached and
the server reaps them. Paths are validated before anything is sent, so a typo
costs no network round trip.

The returned post carries `file_count` and a populated `files[]`; pass
`files[].id` to `mm download` to fetch a file back.

### `react` / `unreact`

```
mm react   <post_id> :emoji_name:
mm unreact <post_id> :emoji_name:
```

Colons around the emoji name are accepted but not required — both `+1`
and `:+1:` work.

### `pin` / `unpin`

```
mm pin   <post_id>
mm unpin <post_id>
```

### `edit`

Edit your own post.

```
mm edit <post_id> -m "new body"
echo "new body" | mm edit <post_id> --read
```

### `delete`

Delete your own post. Requires `--yes` to confirm.

```
mm delete <post_id> --yes
```

Don't supply `--yes` without explicit user confirmation.

### `mark-read`

Reset the unread badge on a channel.

```
mm mark-read <ref>
```

### `create-channel`

Create a new public or private channel in a team.

```
mm create-channel <name> [--type public|private] [--team <name|id>] \
                        [--display-name "<text>"] [--purpose "<text>"] [--header "<text>"]
```

- `<name>` — URL-safe channel name (lowercased automatically)
- `--type` — `public` (default) or `private`
- `--team` — required if you belong to more than one team; matches by ID, name, or display name
- `--display-name` — pretty name shown in the UI; defaults to `<name>`

Returns `{id, name, display_name, type, team, team_id, purpose, header}`.

### `add-user`

Add a user to a channel.

```
mm add-user <ref> @username
```

Returns the resulting `{channel_id, channel, user_id, user, roles}`.

### `join`

Join a public channel yourself. For private channels, ask an existing
member to invite you via `mm add-user`.

```
mm join <channel-ref> [--team <name|id>]
```

`--team` is needed only when the same channel name exists in more than one
of your teams. Returns `{channel_id, channel, channel_name, team_id, type, roles}`.

### `status`

```
mm status <state>                              # online | away | dnd | offline
mm status online -m "in a call" --emoji :phone:
mm status --clear                              # remove custom status only
```

---

## Auth commands

### `login`

```
mm login --url https://chat.example.com --token YOUR_PAT
echo "$PAT" | mm login --url ... --read-token         # token from stdin
mm login --url ... --login user@example.com --mfa 123456   # password+MFA
```

### `logout`

Revoke the session and clear `~/.config/mm/config.json`.

```
mm logout
```

### `whoami`

Validate the current credentials; print user + teams.

```
mm whoami
```

### `completion`

Generate shell completion scripts.

```
mm completion bash | zsh | fish | powershell
```
