# mattermost-cli

[![CI](https://github.com/ayusavin/mattermost-cli/actions/workflows/go-ci.yml/badge.svg)](https://github.com/ayusavin/mattermost-cli/actions/workflows/go-ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Mattermost CLI for humans and agents. Ships as a single static binary `mm`.
JSON output by default for agent consumption; pass `--human` for markdown.

## Install

```bash
# macOS / Linux via Homebrew
brew install ayusavin/tap/mm

# Or with Go (downloads the published module)
go install github.com/ayusavin/mattermost-cli/cmd/mm@latest
```

Or [download a release binary](https://github.com/ayusavin/mattermost-cli/releases).

### Build from source

```bash
git clone https://github.com/ayusavin/mattermost-cli
cd mattermost-cli
CGO_ENABLED=0 go build -o mm ./cmd/mm
```

`CGO_ENABLED=0` keeps `mm` a single static binary; the local cache uses a
pure-Go SQLite driver so no C toolchain is needed. To stamp the version the way
releases do:

```bash
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always)" \
  -o mm ./cmd/mm
```

Without those flags `mm --version` reports `dev`.

## Authenticate

```bash
# Personal Access Token (recommended)
mm login --url https://chat.example.com --token <PAT>

# Verify
mm whoami
```

Environment variables override the config file: `MATTERMOST_URL`,
`MATTERMOST_TOKEN`, `MATTERMOST_TEAM`. The on-disk config lives at
`~/.config/mm/config.json` (token only, `0600` permissions).

## Read

```bash
mm overview                    # unreads + recent mentions in one call
mm channels                    # all channels you're a member of
mm channels --type D           # only DMs
mm channel <ref>               # channel info (purpose, members, last activity)
mm unread                      # channels with unread messages
mm messages <ref> --limit 30   # recent messages, JSON
mm thread <post-id>            # root + last 9 replies (default)
mm members <ref>               # who's in the channel
mm pinned <ref>                # pinned posts
mm user @alice                 # profile + status + timezone
mm mentions --since 7d         # posts @-mentioning you
mm search "deployment"         # search across all your teams
mm find-channel <term>         # channels across your teams by name or purpose
mm search-user <term>          # users by username, full name, nickname, email
mm download <file-id>          # save an attachment (IDs come from files[].id)
mm watch                       # follow the WebSocket event stream (your own events excluded)
```

Channel references (`<ref>`) accept a channel name (`off-topic`), a `~name`
form, `@username` for a DM, or a raw channel ID. Post references accept a post
ID or a permalink. See
[`skills/mattermost/references/commands.md`](skills/mattermost/references/commands.md)
for every command and flag.

## Local-first: sync daemon + `mm query`

Run a background daemon that mirrors Mattermost into a local SQLite cache and
keeps it live over the WebSocket, then query it with read-only SQL — instant,
with no per-command API round-trip or cold start.

```bash
mm sync start                  # backfill + realtime sync in the background
mm sync status                 # running / ipc_reachable / ws_connected / backfill_done
mm sync stop

mm query --schema              # tables + views (v_post, v_channel, v_unread, v_thread)
mm query "SELECT name, unread_count FROM v_unread ORDER BY unread_count DESC"
mm query "SELECT author, message, created_at FROM v_post
          WHERE channel_id='<id>' ORDER BY create_at DESC LIMIT 30"
```

`mm query` is read-only — only `SELECT`, `WITH`, and `EXPLAIN` are accepted.
The cache lives under `os.UserCacheDir()` (`~/Library/Caches/mm` on macOS,
`~/.cache/mm` on Linux); override it with `MM_CACHE_PATH`. When the daemon is
running and fresh, reads such as `find-channel` use the cache automatically,
falling back to the live API when it is not (`MM_NO_DAEMON=1` always forces
live). Writes still go through the normal commands and are reflected in the
cache immediately (read-your-writes).

## Write

```bash
mm post <ref> -m "hello"               # new post
mm reply <post-id> -m "ack"            # threaded reply
mm dm @alice -m "ping"                 # direct message
echo "from stdin" | mm post <ref> --read

mm react <post-id> :white_check_mark:  # add reaction
mm unreact <post-id> :white_check_mark:

mm pin <post-id>
mm unpin <post-id>

mm edit <post-id> -m "fixed typo"      # only your own posts
mm delete <post-id> --yes              # only your own posts; --yes required

mm mark-read <ref>                     # reset unread badges

mm status away                         # online | away | dnd | offline
mm status online -m "back" --emoji :coffee:
mm status --clear                      # remove custom status
```

## JSON shape

Every post returned by `messages`/`thread`/`search`/`mentions` includes:

- `id`, `thread_id`, `is_reply`, `reply_count` (on root posts)
- `author` (e.g. `@alice`), `message`, `created_at` (ISO 8601 UTC)
- `channel_id`, `channel` (display name), `team` (where relevant)
- `file_count`, `files[]` (each entry has `id`, `name`, `size`, `mime_type`,
  `extension`, plus `width`/`height` for images; pass `id` to `mm download`)
- `reactions` (map: `{":wave:": 2}`)

`unread` / `channels` rows include a `ref` field — the exact string to pass
to `mm messages <ref>`. Always use `ref`, not raw IDs or display names.

## Exit codes

| Code | Meaning |
|------|---------|
| 0    | OK |
| 1    | Generic error |
| 2    | Auth expired or invalid — run `mm login` |
| 3    | Rate limited by the server |
| 4    | Timed out waiting (`mm watch --timeout`) |

## Shell completion

```bash
mm completion bash   > /etc/bash_completion.d/mm            # Linux
mm completion bash   > /usr/local/etc/bash_completion.d/mm  # macOS (Homebrew)
mm completion zsh    > "${fpath[1]}/_mm"
mm completion fish   > ~/.config/fish/completions/mm.fish
```

Homebrew installs completions automatically.

## Develop

```bash
go test ./... -count=1
go vet ./...
```

Unit tests cover pure logic only; the smoke script is the only thing that
exercises the Mattermost SDK. Copy `.env.smoke.example` to `.env.smoke` and
fill in the URL and PAT, then:

```bash
scripts/smoke.sh          # read-only commands
scripts/smoke.sh --write  # also exercises post/react/pin/edit/delete/watch
```

`.env.smoke` is gitignored.

## License

MIT
