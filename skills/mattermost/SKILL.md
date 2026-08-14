---
name: mattermost
description: Read, search, and act in Mattermost via the `mm` CLI. Use whenever the user mentions Mattermost, chat messages, team chat, unreads, DMs, channel history, mentions, threads, or wants to catch up, post a message, react to something, set their status, pin/edit/delete a post, or run any other chat action from the terminal. The CLI outputs agent-friendly JSON by default with thread IDs, bot detection, reactions, and channel refs - no parsing needed.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, AskUserQuestion
---

# Mattermost CLI (`mm`)

Read **and write** in Mattermost from the command line. Single static Go
binary, JSON output by default for agents, `--human` for markdown.

## Setup

Check if `mm` is already on PATH and authenticated:

```bash
mm whoami
```

If that works, jump to [Start here](#start-here-mm-overview).

If `mm` is missing, install it. **Homebrew is the canonical path:**

```bash
brew install ayusavin/tap/mm
```

Alternatives if Homebrew isn't available:

```bash
# Go toolchain
go install github.com/ayusavin/mattermost-cli/cmd/mm@latest

# Or grab a prebuilt binary
# https://github.com/ayusavin/mattermost-cli/releases
```

> Note: the old PyPI package `mattermost-cli` is dead. Do not install it.

Then authenticate once with a Personal Access Token (Profile > Security >
Personal Access Tokens in Mattermost):

```bash
mm login --url https://chat.example.com --token YOUR_PAT
mm whoami   # verify
```

Password + MFA fallback:

```bash
mm login --url https://chat.example.com --login you@example.com --mfa 123456
```

`MATTERMOST_URL` / `MATTERMOST_TOKEN` / `MATTERMOST_TEAM` env vars override
the on-disk config (`~/.config/mm/config.json`, mode `0600`). See
[references/setup.md](references/setup.md) for multi-server tricks and
troubleshooting.

## Start here: `mm overview`

Always run this first. One call returns everything you need to triage:

```bash
mm overview              # last 6 hours (default)
mm overview --since 1d   # last 24 hours
```

Response sections:
- **mentions** — posts that @-mention you, including `root` context for replies
- **unread** — channels with unread counts
- **active_channels** — channels with recent posts, sorted by recency

Every channel entry carries a `ref` field you pass straight into `mm
messages`. Never reconstruct refs by hand — use what `mm` gives you.

## Fast local reads: sync daemon + `mm query`

For heavy reading, run the **sync daemon** once. It mirrors your Mattermost
(channels, members, posts, reactions, read state) into a local SQLite cache and
keeps it live over the WebSocket. Then query that cache with SQL: instant, no
per-command network round-trip, no cold start.

```bash
mm sync start     # backfills, then realtime-syncs in the background
mm sync status    # {running, ipc_reachable, ws_connected, backfill_done, ...}
mm sync stop
mm sync logs
```

Once `backfill_done` is true, query the cache (read-only SQL → JSON rows, like
Notion's query-database):

```bash
mm query --schema                                  # discover tables + views
mm query "SELECT name, display_name, unread_count, mention_count
          FROM v_unread ORDER BY unread_count DESC LIMIT 20"
mm query "SELECT author, message, created_at FROM v_post
          WHERE channel_id='<id>' AND delete_at=0
          ORDER BY create_at DESC LIMIT 30"
mm query "SELECT display_name, type, team FROM v_channel
          WHERE lower(name) LIKE '%deploy%'"        # one query, not a loop
mm query "SELECT * FROM sync_state"                 # freshness / liveness
```

Enriched views mirror the post/channel JSON shapes:
- `v_post` — id, thread_id, is_reply, author, message, created_at, channel,
  team, file_count, reply_count, is_bot, bot_name, reactions (JSON), files (JSON).
- `v_channel` — channels + team + resolved display_name.
- `v_unread` — unread_count, mention_count per channel.
- `v_thread` — posts grouped by thread.
- Full-text search via FTS5:
  `SELECT p.* FROM posts p JOIN posts_fts f ON p.rowid=f.rowid WHERE posts_fts MATCH 'deploy'`.

`mm query` is **read-only** (SELECT / WITH / EXPLAIN only). Writes still go
through the normal commands (`mm post`/`reply`/`dm`/`edit`), which hand the
result to the daemon so a follow-up `mm query` sees it immediately
(read-your-writes). When the daemon is running and fresh, `find-channel` and
other reads use the cache automatically; otherwise they fall back to the live
API. `MM_NO_DAEMON=1` forces the live path.

**Don't loop or over-fetch.** One `mm query` SELECT replaces the classic
anti-patterns that make sessions slow:
- looping `find-channel` over guessed names → `... WHERE lower(name) LIKE '%x%'`
- escalating `--since` (6h → 1d → 30d → all) → `WHERE create_at > <epoch_ms>`
- piping output to `grep`/`python` to filter or aggregate → do it in SQL

### Process new messages: `mm new` / `mm seen`

`mm new` lists posts you haven't processed yet — newer than your cursor, across
all channels, excluding your own posts. Decoupled from Mattermost read/unread:
querying never marks anything read, and the cursor is *your* processing
watermark, not a server flag. After reviewing, `mm seen` advances it to now.

```bash
mm new                 # posts since your cursor (run `mm seen` once to set a baseline)
mm new --since 1d      # don't look back further than 1d
mm seen                # record "processed through now"
```

### Load older history: `mm history`

Backfill keeps only a recent window per channel (sized to cover unread). To pull
deeper history into the cache so `mm query` / `mm messages` see it locally:

```bash
mm history <channel> --limit 500
```

Reading deeper than the window without `mm history` still works — `mm messages`
falls back to the live API — it just isn't cached.

## Reading

```bash
mm messages <ref>                # last 30 messages, chronological
mm messages <ref> --since 2h
mm messages <ref> --threads      # group by thread (root + reply count + last)
mm messages @username            # DM with someone
mm thread <thread_id>            # root + last 9 replies
mm thread <thread_id> --limit 0  # entire thread
mm mentions --since 3d
mm search "from:alice deploy after:2025-01-01"
mm find-channel <term>           # search your teams' channels by name/purpose
mm find-channel <term> --type public
mm search-user <term>            # users by username, full name, nickname, email
mm channel <ref>                 # purpose, header, member/pinned counts
mm channels --type dm --since 6h
mm pinned <ref>
mm members <ref>
mm user @someone
mm download <file-id>            # save attachment to cwd; --output -, --force
mm watch                         # follow the WebSocket event stream
```

`<ref>` accepts a channel name (`off-topic`), `@username` for DMs, a `~name`
form, or a raw channel ID (use this for group DMs from `overview` output).

## Acting (write commands)

```bash
mm post <ref> -m "deploy started"          # plain text
echo "long body" | mm post <ref> --read    # body from stdin
mm reply <post_id> -m "ack"                # threaded reply
mm dm @alice -m "ping"                     # direct message

# Attachments: --file is repeatable (max 10); -m is optional once a file is set.
mm post <ref> -m "the trace" --file ./crash.log --file ./shot.png
mm dm @alice --file ./report.pdf
make-report | mm post <ref> --file - --filename report.txt   # bytes from stdin

mm react   <post_id> :white_check_mark:
mm unreact <post_id> :white_check_mark:

mm pin   <post_id>
mm unpin <post_id>

mm edit   <post_id> -m "fixed typo"        # only own posts
mm delete <post_id> --yes                  # only own posts; --yes is required

mm mark-read <ref>                         # clear unread badge

mm create-channel <name> --type public      # new channel (--type public|private)
mm create-channel <name> --team eng --purpose "..." --header "..."
mm add-user <ref> @username                 # add a user to a channel
mm join <channel>                           # join a public channel yourself

mm status away
mm status online -m "back at it" --emoji :coffee:
mm status --clear                          # remove custom status
```

`<post_id>` is the `id` field of any post you've already seen, or a full
permalink — `mm` extracts the ID either way. `<ref>` and `<post_id>` are
both accepted from previous JSON output without transformation.

Write commands return the created/updated resource as JSON so you can
chain (e.g. take the `id` from `mm post` and feed it into `mm pin`).

## Key JSON fields on posts

| Field | What it's for |
|-------|---------------|
| `id` | The post ID — pass to `thread`, `reply`, `react`, `pin`, `edit`, `delete` |
| `thread_id` | Pass to `mm thread` to read the full conversation |
| `ref` | On channel entries; pass to `mm messages` |
| `is_bot` / `bot_name` | Webhook and bot posts are flagged automatically |
| `root` | On reply-mentions; the original message being replied to |
| `is_reply` / `reply_count` | Thread structure |
| `reactions` | Emoji counts like `{":+1:": 3, ":white_check_mark:": 1}` |
| `created_at` | ISO 8601 UTC |
| `file_count` / `files[]` | Attachments. Each entry has `id`, `name`, `size`, `mime_type`, `extension`, and `width`/`height` for images. Pass `id` to `mm download`; send files with `mm post --file`. |

Bot posts from webhooks have alert content extracted from Slack-format
attachments, so you see actual alert text, not empty messages.

## Exit codes

| Code | Meaning |
|------|---------|
| 0    | OK |
| 1    | Generic error |
| 2    | Auth expired or invalid — run `mm login` again |
| 3    | Rate limited by the server — back off and retry |

Always check the exit code before assuming a write succeeded.

## Agent etiquette

- **Read before you write.** Run `mm overview` or `mm messages <ref>` first so
  you have correct `ref`/`id`/`thread_id` values.
- **Prefer `mm query` for discovery and bulk reads** when a sync daemon is
  running (check `mm sync status`). One SQL SELECT beats looping `find-channel`
  over guessed names or re-fetching with an ever-wider `--since`.
- **Reply, don't post a new thread**, when continuing an existing conversation.
  Use the `thread_id` from a previous post with `mm reply`.
- **Confirm destructive ops with the user.** `mm delete` requires `--yes`;
  don't supply it without explicit instruction.
- **Don't spam reactions or messages on retries.** If a write command's exit
  code is non-zero, check whether it actually went through (e.g. re-fetch
  the channel) before re-running.
- **Always inspect attachments.** A post text alone is often not the full
  message — screenshots, logs, PDFs, and design files carry the actual
  context. Whenever a post has `file_count > 0`, pull each entry from
  `files[]` with `mm download <id>` and open it (image viewer for screenshots,
  read for text/logs, pass to the appropriate tool for other formats) before
  responding or summarizing. Treating a post as understood without looking at
  its attachments is a frequent failure mode.

## Further reading

- [references/setup.md](references/setup.md) — install, auth, env vars, troubleshooting
- [references/commands.md](references/commands.md) — full flag-level reference for every command
- [references/scenarios.md](references/scenarios.md) — "what did I miss?", "summarize this channel", "is this resolved?"
- [references/workflows.md](references/workflows.md) — multi-step recipes: morning triage, incident reply, channel discovery
