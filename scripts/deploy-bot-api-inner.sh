#!/usr/bin/env bash
# bot_api deploy inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: ***
#
# Deploy flow:
#   1. Validate the committed tar.gz artifact path.
#   2. Extract, locate the App/ directory inside the artifact.
#   3. Back up the live App/ to /home/api/backup/bot_api_<yyyymmdd_HHMMSS>/App.
#   4. Replace /home/api/mixbot_api/App (ownership preserved from the old App).
#   5. Restart: php easyswoole server stop -force && php easyswoole server start -d,
#      verify with server status; roll back the old App if it fails.
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
APP_ROOT="/home/api/mixbot_api"
APP_DIR="$APP_ROOT/App"
BACKUP_ROOT="/home/api/backup"
PHP_BIN="${PHP_BIN:-php}"

fail() {
  echo "$1" >&2
  exit 2
}

empty_dir() {
  local dir="$1"
  [[ -n "$dir" && -d "$dir" ]] || fail "directory is not safe to clean: $dir"
  rm -rf "$dir"/* "$dir"/.[!.]* "$dir"/..?*
}

server_stop() {
  ( cd "$APP_ROOT" && "$PHP_BIN" easyswoole server stop -force )
}

server_start() {
  ( cd "$APP_ROOT" && "$PHP_BIN" easyswoole server start -d )
}

server_status() {
  ( cd "$APP_ROOT" && "$PHP_BIN" easyswoole server status )
}

[[ -n "$ARTIFACT_PATH" ]] || fail "artifact path is required"
case "$ARTIFACT_PATH" in
  "$ARTIFACT_ROOT"/*.tar.gz|"$ARTIFACT_ROOT"/*.tgz) ;;
  *) fail "artifact path is outside committed artifact root or not a tar.gz" ;;
esac
[[ -f "$ARTIFACT_PATH" ]] || fail "artifact not found: $ARTIFACT_PATH"
command -v tar >/dev/null 2>&1 || fail "tar is not installed"

[[ -d "$APP_ROOT" ]] || fail "app directory not found: $APP_ROOT"
[[ -d "$APP_DIR" ]] || fail "App directory not found: $APP_DIR"

WORK_DIR="$(mktemp -d /tmp/bot-api.XXXXXX)"
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

NEW_APP="$SRC/App"
if [[ ! -d "$NEW_APP" ]]; then
  NEW_APP="$(find "$SRC" -maxdepth 2 -type d -name App -print -quit)"
fi
[[ -n "$NEW_APP" && -d "$NEW_APP" ]] || fail "artifact missing App directory"
[[ -f "$NEW_APP/../composer.json" ]] || echo "warning: artifact has no composer.json next to App" >&2

TS="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/bot_api_$TS"
mkdir -p "$BACKUP_DIR"

# Preserve the ownership of the live App on the new one.
APP_OWNER="$(stat -c '%u:%g' "$APP_DIR")"
echo "app owner: $APP_OWNER"

STAGED_APP="$APP_ROOT/.App.new-$TS"
rm -rf "$STAGED_APP"
cp -a "$NEW_APP" "$STAGED_APP"
chown -R "$APP_OWNER" "$STAGED_APP" 2>/dev/null || true

rollback() {
  echo "deploy failed, rolling back from $BACKUP_DIR" >&2
  rm -rf "${APP_DIR:?}"
  if [[ -d "$BACKUP_DIR/App" ]]; then
    cp -a "$BACKUP_DIR/App" "$APP_DIR"
    chown -R "$APP_OWNER" "$APP_DIR" 2>/dev/null || true
  fi
  rm -rf "$STAGED_APP"
  server_stop >/dev/null 2>&1 || true
  server_start >/dev/null 2>&1 || true
}

mv "$APP_DIR" "$BACKUP_DIR/App"
mv "$STAGED_APP" "$APP_DIR"

echo "== 停止服务: $PHP_BIN easyswoole server stop -force =="
if ! stop_output="$(server_stop 2>&1)"; then
  echo "stop 返回非零（可能是服务未运行），继续：" >&2
  printf '%s\n' "$stop_output" >&2
fi
printf '%s\n' "$stop_output" || true

echo "== 启动服务: $PHP_BIN easyswoole server start -d =="
if ! start_output="$(server_start 2>&1)"; then
  printf '%s\n' "$start_output" >&2
  rollback
  exit 1
fi
printf '%s\n' "$start_output"

echo "== 校验服务状态: $PHP_BIN easyswoole server status =="
if ! status_output="$(server_status 2>&1)"; then
  printf '%s\n' "$status_output" >&2
  echo "server status 校验失败，回滚旧 App" >&2
  rollback
  exit 1
fi
printf '%s\n' "$status_output"

echo "artifact: $ARTIFACT_PATH"
echo "backup:  $BACKUP_DIR"
echo "deployed: $APP_DIR"
echo "restarted: $PHP_BIN easyswoole server stop -force / start -d"
