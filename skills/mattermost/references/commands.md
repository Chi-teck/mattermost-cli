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

## Write commands

All write commands return the created/updated resource as JSON.

### `post`

```
mm post <ref> -m "body"
echo "body" | mm post <ref> --read
```

### `reply`

```
mm reply <post_id> -m "body"
echo "body" | mm reply <post_id> --read
```

### `dm`

Send a direct message. Opens (or reuses) the DM channel.

```
mm dm @username -m "body"
```

Self-DM is allowed (`mm dm @<you>`).

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
