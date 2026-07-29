#!/usr/bin/env bash
# mixbot deploy inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: /opt/server-shell-mcp/scripts/deploy-mixbot-inner.sh
#
# Deploy flow:
#   1. Validate the committed tar.gz artifact path.
#   2. Extract to a temp dir and verify modules/, public/prod/, public/dist/ exist.
#   3. Stage the required directories.
#   4. Back up live dirs from /var/www/html/mixbot/mixbot into /var/www/html/backup/mixbot_<yyyymmdd_HHMMSS>.
#   5. Copy new dirs into place, rolling back on failure.
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
HTML_ROOT="/var/www/html"
MIXBOT_DIR="$HTML_ROOT/mixbot/mixbot"
BACKUP_ROOT="$HTML_ROOT/backup"
WEB_OWNER="${WEB_OWNER:-root:root}"

fail() {
  echo "$1" >&2
  exit 2
}

empty_dir() {
  local dir="$1"
  [[ -n "$dir" && -d "$dir" ]] || fail "directory is not safe to clean: $dir"
  rm -rf "$dir"/* "$dir"/.[!.]* "$dir"/..?*
}

[[ -n "$ARTIFACT_PATH" ]] || fail "artifact path is required"

case "$ARTIFACT_PATH" in
  "$ARTIFACT_ROOT"/*.tar.gz|"$ARTIFACT_ROOT"/*.tgz) ;;
  *) fail "artifact path is outside committed artifact root or not a tar.gz" ;;
esac

[[ -f "$ARTIFACT_PATH" ]] || fail "artifact not found: $ARTIFACT_PATH"
command -v tar >/dev/null 2>&1 || fail "tar is not installed"

STAGING_DIR="$(mktemp -d /tmp/mixbot-stage.XXXXXX)"
WORK_DIR="$(mktemp -d /tmp/mixbot.XXXXXX)"
cleanup() {
  rm -rf "$WORK_DIR" "$STAGING_DIR"
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

for required in modules public/prod public/dist; do
  [[ -d "$SRC/$required" ]] || fail "artifact missing directory: $required"
done

mkdir -p "$STAGING_DIR"
empty_dir "$STAGING_DIR"
for d in modules public/prod public/dist; do
  mkdir -p "$(dirname "$STAGING_DIR/$d")"
  cp -a "$SRC/$d" "$STAGING_DIR/$d"
done

TS="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/mixbot_$TS"
mkdir -p "$BACKUP_DIR"

rollback() {
  echo "deploy failed, rolling back from $BACKUP_DIR" >&2
  for d in modules public/prod public/dist; do
    if [[ -d "$BACKUP_DIR/$d" ]]; then
      rm -rf "${MIXBOT_DIR:?}/$d"
      mkdir -p "$(dirname "$MIXBOT_DIR/$d")"
      cp -a "$BACKUP_DIR/$d" "$MIXBOT_DIR/$d"
    fi
  done
}

for d in modules public/prod public/dist; do
  if [[ -e "$MIXBOT_DIR/$d" ]]; then
    mkdir -p "$(dirname "$BACKUP_DIR/$d")"
    mv "$MIXBOT_DIR/$d" "$BACKUP_DIR/$d"
  fi
done

deploy_failed=0
for d in modules public/prod public/dist; do
  mkdir -p "$(dirname "$MIXBOT_DIR/$d")"
  if ! cp -a "$STAGING_DIR/$d" "$MIXBOT_DIR/$d"; then
    deploy_failed=1
    break
  fi
done

if [[ "$deploy_failed" == "1" ]]; then
  rollback
  exit 1
fi

chown -R "$WEB_OWNER" "$MIXBOT_DIR/modules" "$MIXBOT_DIR/public/prod" "$MIXBOT_DIR/public/dist" 2>/dev/null || true

echo "staging: $STAGING_DIR"
echo "backup:  $BACKUP_DIR"
echo "deployed: $MIXBOT_DIR/modules, $MIXBOT_DIR/public/prod, $MIXBOT_DIR/public/dist"
