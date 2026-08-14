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

  step "post --file"
  FIXTURE="$(mktemp)"
  printf 'mm CLI smoke attachment %s\n' "$(date +%H:%M:%S)" >"$FIXTURE"
  ATTACH_JSON=$("$BIN" post "$CHANNEL" -m "mm CLI smoke attachment" --file "$FIXTURE")
  echo "$ATTACH_JSON"
  ATTACH_POST_ID=$(echo "$ATTACH_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
  FILE_ID=$(echo "$ATTACH_JSON" | python3 -c "
import sys, json
d = json.load(sys.stdin)
assert d['file_count'] == 1, f\"file_count {d['file_count']} != 1\"
assert d.get('files'), 'files[] missing from the created post'
assert d['files'][0]['name'], 'files[0].name is empty'
print(d['files'][0]['id'])")

  # The real proof: what we uploaded is byte-for-byte what comes back down.
  step "download round-trip"
  "$BIN" download "$FILE_ID" --output - | diff - "$FIXTURE"
  echo "attachment round-tripped unchanged"
  "$BIN" delete "$ATTACH_POST_ID" --yes
  rm -f "$FIXTURE"

  step "post --file - (stdin)"
  STDIN_JSON=$(printf 'piped attachment\n' |
    "$BIN" post "$CHANNEL" -m "mm CLI smoke piped attachment" --file - --filename piped.txt)
  echo "$STDIN_JSON"
  STDIN_POST_ID=$(echo "$STDIN_JSON" | python3 -c "
import sys, json
d = json.load(sys.stdin)
assert d['file_count'] == 1, f\"file_count {d['file_count']} != 1\"
assert d['files'][0]['name'] == 'piped.txt', d['files'][0]['name']
print(d['id'])")
  "$BIN" delete "$STDIN_POST_ID" --yes
fi

step "DONE"
