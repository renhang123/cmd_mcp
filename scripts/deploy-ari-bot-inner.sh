#!/usr/bin/env bash
# ari-bot deploy inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: /opt/server-shell-mcp/scripts/deploy-ari-bot-inner.sh
#
# Deploy flow:
#   1. Validate the committed tar.gz artifact path.
#   2. Extract to a temp dir and locate the ari-bot binary (verify it is an ELF).
#   3. Back up the live binary to /var/www/html/backup/ari-bot_<yyyymmdd_HHMMSS>.
#   4. Replace /var/www/html/ari-bot/ari-bot atomically, keeping config untouched.
#   5. Restart through manage.sh and verify status; roll back the binary if it fails.
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
HTML_ROOT="/var/www/html"
APP_DIR="$HTML_ROOT/ari-bot"
APP_BIN="$APP_DIR/ari-bot"
BACKUP_ROOT="$HTML_ROOT/backup"
APP_OWNER="${APP_OWNER:-root:root}"

fail() {
  echo "$1" >&2
  exit 2
}

empty_dir() {
  local dir="$1"
  [[ -n "$dir" && -d "$dir" ]] || fail "directory is not safe to clean: $dir"
  rm -rf "$dir"/* "$dir"/.[!.]* "$dir"/..?*
}

is_elf() {
  local file="$1"
  [[ -f "$file" ]] || return 1
  [[ "$(od -An -tx1 -N4 "$file" | tr -d ' \n')" == "7f454c46" ]]
}

restart_managed_service() {
  ( cd "$APP_DIR" && ./manage.sh restart )
}

service_running() {
  ( cd "$APP_DIR" && ./manage.sh status >/dev/null 2>&1 )
}

[[ -n "$ARTIFACT_PATH" ]] || fail "artifact path is required"
case "$ARTIFACT_PATH" in
  "$ARTIFACT_ROOT"/*.tar.gz|"$ARTIFACT_ROOT"/*.tgz) ;;
  *) fail "artifact path is outside committed artifact root or not a tar.gz" ;;
esac
[[ -f "$ARTIFACT_PATH" ]] || fail "artifact not found: $ARTIFACT_PATH"
command -v tar >/dev/null 2>&1 || fail "tar is not installed"

[[ -d "$APP_DIR" ]] || fail "app directory not found: $APP_DIR"
[[ -f "$APP_DIR/manage.sh" ]] || fail "manage.sh not found in $APP_DIR"
[[ -x "$APP_DIR/manage.sh" ]] || fail "manage.sh is not executable: $APP_DIR/manage.sh"

WORK_DIR="$(mktemp -d /tmp/ari-bot.XXXXXX)"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

empty_dir "$WORK_DIR"
tar -xzf "$ARTIFACT_PATH" -C "$WORK_DIR"

# The tarball may wrap everything in a single top-level directory.
SRC="$WORK_DIR"
top_dirs="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 -type d)"
top_count="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 | wc -l)"
if [[ "$top_count" == "1" && -n "$top_dirs" ]]; then
  SRC="$top_dirs"
fi

NEW_BIN="$SRC/ari-bot"
if [[ ! -f "$NEW_BIN" ]]; then
  NEW_BIN="$(find "$SRC" -maxdepth 3 -type f -name ari-bot -print -quit)"
fi
[[ -n "$NEW_BIN" && -f "$NEW_BIN" ]] || fail "artifact missing ari-bot binary"
is_elf "$NEW_BIN" || fail "artifact ari-bot is not a Linux ELF binary"

TS="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/ari-bot_$TS"
mkdir -p "$BACKUP_DIR"

HAD_OLD_BIN=0
if [[ -f "$APP_BIN" ]]; then
  cp -a "$APP_BIN" "$BACKUP_DIR/ari-bot"
  HAD_OLD_BIN=1
fi

STAGED_BIN="$APP_DIR/.ari-bot.new-$TS"
install -m 0755 "$NEW_BIN" "$STAGED_BIN"
chown "$APP_OWNER" "$STAGED_BIN" 2>/dev/null || true

rollback() {
  echo "deploy failed, rolling back from $BACKUP_DIR" >&2
  if [[ "$HAD_OLD_BIN" == "1" && -f "$BACKUP_DIR/ari-bot" ]]; then
    install -m 0755 "$BACKUP_DIR/ari-bot" "$APP_BIN"
    chown "$APP_OWNER" "$APP_BIN" 2>/dev/null || true
    restart_managed_service >/dev/null 2>&1 || true
  fi
  rm -f "$STAGED_BIN"
}

mv -f "$STAGED_BIN" "$APP_BIN"
chown "$APP_OWNER" "$APP_BIN" 2>/dev/null || true

if ! restart_output="$(restart_managed_service 2>&1)"; then
  printf '%s\n' "$restart_output" >&2
  rollback
  exit 1
fi

if ! service_running; then
  printf '%s\n' "$restart_output" >&2
  echo "manage.sh status reports the service is not running after restart" >&2
  rollback
  exit 1
fi

echo "artifact: $ARTIFACT_PATH"
echo "backup:  $BACKUP_DIR"
echo "deployed: $APP_BIN"
echo "restarted: $APP_DIR/manage.sh restart"
printf '%s\n' "$restart_output"
