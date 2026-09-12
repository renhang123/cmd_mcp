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
#
# 注意: php 命令的 stdout/stderr 一律重定向到日志文件并 </dev/null，
#       否则 `server start -d` 的守护进程会持有调用方（MCP）的输出管道，
#       导致 MCP 命令迟迟等不到 EOF 而挂住。
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
APP_ROOT="/home/api/mixbot_api"
APP_DIR="$APP_ROOT/App"
BACKUP_ROOT="/home/api/backup"
PHP_BIN="${PHP_BIN:-php}"
LOG_FILE="${LOG_FILE:-/tmp/bot-api-deploy-$(date +%Y%m%d_%H%M%S).log}"

fail() {
  echo "$1" >&2
  exit 2
}

empty_dir() {
  local dir="$1"
  [[ -n "$dir" && -d "$dir" ]] || fail "directory is not safe to clean: $dir"
  rm -rf "$dir"/* "$dir"/.[!.]* "$dir"/..?*
}

# 在 $APP_ROOT 下执行 php easyswoole 子命令，输出写日志，避免守护进程占用管道。
php_call() {
  local label="$1"
  shift
  echo "== $label =="
  local before after
  before="$(wc -l <"$LOG_FILE" 2>/dev/null || echo 0)"
  local rc=0
  if command -v timeout >/dev/null 2>&1; then
    ( cd "$APP_ROOT" && timeout 120 "$PHP_BIN" "$@" ) </dev/null >>"$LOG_FILE" 2>&1 || rc=$?
  else
    ( cd "$APP_ROOT" && "$PHP_BIN" "$@" ) </dev/null >>"$LOG_FILE" 2>&1 || rc=$?
  fi
  after="$(wc -l <"$LOG_FILE" 2>/dev/null || echo 0)"
  if [ "$after" -gt "$before" ]; then
    tail -n +$((before + 1)) "$LOG_FILE"
  fi
  return "$rc"
}

server_stop() {
  php_call "停止服务: $PHP_BIN easyswoole server stop -force" easyswoole server stop -force
}

server_start() {
  php_call "启动服务: $PHP_BIN easyswoole server start -d" easyswoole server start -d
}

server_status() {
  php_call "校验服务状态: $PHP_BIN easyswoole server status" easyswoole server status
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

: >"$LOG_FILE"

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

if ! server_stop; then
  echo "stop 返回非零（服务可能未运行），继续启动" >&2
fi

if ! server_start; then
  echo "server start 失败，回滚旧 App" >&2
  rollback
  exit 1
fi

if ! server_status; then
  echo "server status 校验失败，回滚旧 App" >&2
  rollback
  exit 1
fi

echo "artifact: $ARTIFACT_PATH"
echo "backup:  $BACKUP_DIR"
echo "deployed: $APP_DIR"
echo "restarted: $PHP_BIN easyswoole server stop -force / start -d"
echo "log: $LOG_FILE"
