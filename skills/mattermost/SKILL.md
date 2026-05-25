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
| `file_count` / `files[]` | Attachments (name + size when the server returns them) |

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
- **Reply, don't post a new thread**, when continuing an existing conversation.
  Use the `thread_id` from a previous post with `mm reply`.
- **Confirm destructive ops with the user.** `mm delete` requires `--yes`;
  don't supply it without explicit instruction.
- **Don't spam reactions or messages on retries.** If a write command's exit
  code is non-zero, check whether it actually went through (e.g. re-fetch
  the channel) before re-running.

## Further reading

- [references/setup.md](references/setup.md) — install, auth, env vars, troubleshooting
- [references/commands.md](references/commands.md) — full flag-level reference for every command
- [references/scenarios.md](references/scenarios.md) — "what did I miss?", "summarize this channel", "is this resolved?"
- [references/workflows.md](references/workflows.md) — multi-step recipes: morning triage, incident reply, channel discovery
