# Agent Instructions

Conventions for AI agents (and humans) working on this repository.

## Overview

`mattermost-cli` is a Go CLI named `mm` for reading and writing in Mattermost.
JSON-first output for agent consumption; `--human` for markdown. Distributed as
a single static binary via Homebrew tap and GitHub releases.

## Layout

```
cmd/mm/                       # main package, exits with internal/errs codes
internal/
  cli/                        # cobra commands (one file per command, init() registers it)
  client/                     # SDK wrapper, retry/backoff, login, classify errors
  config/                     # ~/.config/mm/config.json (0600), env-var override
  errs/                       # typed ExitError + exit codes (0/1/2/3)
  format/                     # EnrichedPost, ChannelRow shapes shared by commands
  resolve/                    # user/channel lookup with per-session cache
  timeparse/                  # --since argument parser (relative, named, absolute)
  store/                      # local SQLite cache: schema, views, migrations, write helpers
  syncd/                      # sync daemon: backfill, WebSocket ingest, reconcile, lifecycle
  ipc/                        # Unix-socket control channel (read-your-writes ingest, healthz)
  wsutil/                     # shared WebSocket connect + event-decode helpers
scripts/smoke.sh              # end-to-end smoke against a live server
.goreleaser.yaml              # cross-platform builds + brew formula
.github/workflows/            # CI (go-ci.yml) and release (release.yml)
```

## Key design decisions

- **JSON first, markdown via `--human`.** Every command has both paths.
- **Agent-tuned defaults.** `thread` shows last 9 replies; `mentions` defaults
  to `--since 1d`; `messages` to `--limit 30`. Bare invocation should give
  useful output without flags.
- **Cross-team by default.** Read commands iterate all the user's teams and
  dedupe. `--team` narrows.
- **No passwords on disk.** Only session tokens or PATs (file mode `0600`,
  directory mode `0700`).
- **Retry with backoff** on 429, 5xx, and network errors via
  `internal/client.Retry`. `Login` is wrapped too so cold TLS handshakes
  don't fail single-shot commands.
- **Local-first (optional).** `mm sync start` spawns a detached daemon
  (`__sync-run`) that is the single writer of a local SQLite cache under
  `os.UserCacheDir()/mm` (pure-Go `modernc.org/sqlite`, WAL, FTS5 — keeps
  `CGO_ENABLED=0`). It backfills via REST, applies realtime WebSocket events,
  and reconciles authoritative read state every 60s; an `flock` guarantees one
  writer. `mm query` runs read-only SQL against the cache; `find-channel` and
  other reads prefer it when a fresh daemon is running (`sync_state` heartbeat)
  and fall back to the live API otherwise (`MM_NO_DAEMON=1` forces live).
  Writes go live and are handed to the daemon over a Unix socket
  (`internal/ipc`) for read-your-writes.

## Adding a command

1. Create `internal/cli/<name>.go`.
2. `func init() { Register(newXxxCmd) }`.
3. `newXxxCmd()` returns a `*cobra.Command` whose `RunE` calls a `runXxx(ctx, ...)`.
4. Inside the runner: `LoadContext(ctx)` for an authenticated SDK client;
   `resolve.New(c.Client, c.Me.Id)` for name → object resolution.
5. Output via `writeJSON(os.Stdout, value)` when `!Globals.Human`, else a
   markdown render. Write commands return the created/updated resource so
   agents can chain.
6. Use `--message` + `--read` (stdin) for any command that takes free text.
   See `internal/cli/m2_input.go::readMessageInput`. Commands that create a
   post go through `internal/cli/attach.go::readPostInput` instead, which adds
   `--file`/`--filename` and arbitrates who gets stdin.

## Conventions

- Channel references in write commands are resolved via
  `resolveMessagesChannel` (accepts ID, name, or `~name`).
- Post references in `reply`/`react`/`pin`/`edit`/`delete` go through
  `extractPostID` so permalinks work too.
- Destructive operations require `--yes` (currently just `delete`).
- Attachments upload to the target channel *before* `CreatePost`, then land in
  `Post.FileIds`. See `internal/cli/attach.go`; uploads borrow a longer HTTP
  timeout via `client.WithTimeout` since `defaultTimeout` covers the body too.
- Empty result arrays must marshal to `[]`, never `null` — initialize as
  `rows := []rowType{}` instead of `var rows []rowType`.
- Use `model.Status*` constants from the SDK for status values, never raw strings.

## Testing

```bash
go test ./... -count=1     # unit tests
go vet ./...
scripts/smoke.sh           # read-only end-to-end
scripts/smoke.sh --write   # full write coverage
```

Unit tests cover pure logic (timeparse, format, resolve, config, input
parsing). Smoke is the only way we currently exercise the SDK.

## Releases

Tag with `vX.Y.Z` and push. The `release.yml` workflow runs GoReleaser,
which builds darwin/linux amd64+arm64 archives, publishes a GitHub release,
and updates `ayusavin/homebrew-tap`.

Required secrets:
- `HOMEBREW_TAP_TOKEN` — PAT with `contents: write` on the tap repo.

## Gotchas

- The Mattermost SDK takes `(scheme, host, port)`, not a URL — `client.New`
  parses the URL and threads it through `model.NewAPIv4Client`.
- DM channel names are `{uid1}__{uid2}` (double underscore); group DMs are
  hashes. Display names are resolved via `Resolver.FormatChannelDisplayName`.
- `GetPostsForChannel` returns posts newest-first; sort by `create_at` for
  chronological display.
- `GetPostThread` includes the root post but may also include posts from the
  same channel that match thread queries — `selectThreadPosts` deduplicates.
- Self-DM is valid in Mattermost — `dm @<me>` opens or reuses the user's own
  DM channel.
