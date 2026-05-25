#!/usr/bin/env bash
# scripts/smoke.sh — end-to-end smoke test against a real Mattermost.
#
# Reads creds from .env.smoke (gitignored). Copy .env.smoke.example to
# .env.smoke and fill in MM_SMOKE_URL + MM_SMOKE_TOKEN before running.
#
# Usage:
#   scripts/smoke.sh           # read-only smoke (safe)
#   scripts/smoke.sh --write   # also exercise post/react/pin/edit/delete
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env.smoke ]]; then
  set -a; source .env.smoke; set +a
fi

: "${MM_SMOKE_URL:?MM_SMOKE_URL is required (see .env.smoke.example)}"
: "${MM_SMOKE_TOKEN:?MM_SMOKE_TOKEN is required (see .env.smoke.example)}"
CHANNEL="${MM_SMOKE_CHANNEL:-town-square}"

WRITE=0
if [[ "${1:-}" == "--write" ]]; then WRITE=1; fi

BIN="$(mktemp -d)/mm"
go build -o "$BIN" ./cmd/mm

export MATTERMOST_URL="$MM_SMOKE_URL"
export MATTERMOST_TOKEN="$MM_SMOKE_TOKEN"

step() { printf '\n=== %s ===\n' "$*"; }

step "whoami";              "$BIN" whoami
step "channels (--type O)"; "$BIN" channels --type O | head -40
step "overview";            "$BIN" overview
step "unread";              "$BIN" unread
step "channel $CHANNEL";    "$BIN" channel "$CHANNEL"
step "messages $CHANNEL";   "$BIN" messages "$CHANNEL" --limit 3
step "search 'welcome'";    "$BIN" search welcome --limit 2 || true
step "mentions --since 90d"; "$BIN" mentions --since 90d
step "members $CHANNEL";    "$BIN" members "$CHANNEL"
step "pinned $CHANNEL";     "$BIN" pinned "$CHANNEL"

if [[ $WRITE -eq 1 ]]; then
  step "post"
  POST_JSON=$("$BIN" post "$CHANNEL" -m "mm CLI smoke $(date +%H:%M:%S)")
  echo "$POST_JSON"
  POST_ID=$(echo "$POST_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

  step "react :white_check_mark:"; "$BIN" react "$POST_ID" :white_check_mark:
  step "pin";                      "$BIN" pin "$POST_ID"
  step "edit";                     "$BIN" edit "$POST_ID" -m "mm CLI smoke (edited)"
  step "thread";                   "$BIN" thread "$POST_ID"
  step "unpin";                    "$BIN" unpin "$POST_ID"
  step "unreact";                  "$BIN" unreact "$POST_ID" white_check_mark
  step "mark-read";                "$BIN" mark-read "$CHANNEL"
  step "delete --yes";             "$BIN" delete "$POST_ID" --yes
fi

step "DONE"
