# Setup Guide

Getting `mm` working from scratch. Install, authenticate, verify.

## 1. Install

`mm` is a single static Go binary. **Homebrew is canonical:**

```bash
brew install ayusavin/tap/mm
```

This also installs bash/zsh/fish completions automatically.

### Alternatives

```bash
# Go toolchain (if you already have Go ≥ 1.22)
go install github.com/ayusavin/mattermost-cli/cmd/mm@latest

# Prebuilt binary (darwin/linux × amd64/arm64)
# https://github.com/ayusavin/mattermost-cli/releases
# Download the right tarball, extract `mm`, put it on $PATH.
```

> The old PyPI package `mattermost-cli` (`pip install mattermost-cli`) is
> dead — the project was rewritten in Go. Don't install it.

## 2. Find your Mattermost server URL

The base URL you type in your browser to access chat — e.g.
`https://chat.example.com`. If unsure, check the address bar while logged
into Mattermost, or ask your team.

## 3. Authenticate

Two paths. Personal Access Token is strongly preferred for agent use.

### Option A: Personal Access Token (recommended)

1. Log into Mattermost in your browser.
2. **Profile > Security > Personal Access Tokens**
   - If you don't see this option, your admin disabled PATs — use Option B.
3. Click **Create Token**, name it (e.g. `mm-cli`), copy the value.
4. Run:

```bash
mm login --url https://chat.example.com --token YOUR_PAT
```

For automation, read the token from stdin to keep it out of shell history:

```bash
echo "$PAT" | mm login --url https://chat.example.com --read-token
```

The token does not expire unless revoked.

### Option B: Password + MFA

```bash
mm login --url https://chat.example.com --login you@example.com --mfa 123456
```

Omit `--mfa` if your account has no MFA. The resulting session token is
stored locally; sessions can expire, so periodic re-login may be needed.
For agent workflows always prefer Option A.

## 4. Verify

```bash
mm whoami
```

Expect username, user ID, and the teams you belong to. On `auth expired`
re-run `mm login`. Exit code 2 means "auth expired or invalid".

## 5. Smoke check

```bash
mm overview        # what's pending
mm messages general
mm messages @colleague
```

## Where config lives

`~/.config/mm/config.json` (file mode `0600`, directory `0700`). Contains
the server URL and the session token. **No password is ever written to
disk.**

## Environment variables

Env vars override the config file:

| Variable          | Purpose |
|-------------------|---------|
| `MATTERMOST_URL`  | Server URL |
| `MATTERMOST_TOKEN`| Auth token (PAT or session) |
| `MATTERMOST_TEAM` | Default team filter |

## Multiple servers

The config file holds one server. Either re-run `mm login` with a different
`--url` to switch, or use env vars per-invocation:

```bash
MATTERMOST_URL=https://chat-a.example.com MATTERMOST_TOKEN=abc mm overview
MATTERMOST_URL=https://chat-b.example.com MATTERMOST_TOKEN=def mm overview
```

## Shell completion

Homebrew installs completions for you. Manual setup:

```bash
mm completion bash > /usr/local/etc/bash_completion.d/mm
mm completion zsh  > "${fpath[1]}/_mm"
mm completion fish > ~/.config/fish/completions/mm.fish
```

## Troubleshooting

**Exit code 2 / "Auth expired"** — run `mm login` again. Use a PAT to avoid
recurrence.

**Exit code 3 / "rate limited"** — back off (Mattermost server limit hit).

**`Unable to reach server` / TLS errors** — network/VPN/firewall reachability.
For self-signed certs, set `SSL_CERT_FILE` to your CA bundle.

**`go install` builds an old version** — pin a tag: `go install
github.com/ayusavin/mattermost-cli/cmd/mm@v0.1.0`. Prefer Homebrew for
release-cadence updates.

**`brew install` says "Error: Cask 'mm' is unavailable"** — make sure you
used the full tap path: `ayusavin/tap/mm`, not just `mm`.
