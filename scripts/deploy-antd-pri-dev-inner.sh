#!/usr/bin/env bash
# antd-pri-dev deploy inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: /opt/server-shell-mcp/scripts/deploy-antd-pri-dev-inner.sh
#
# Deploy flow:
#   1. Validate the committed zip artifact path.
#   2. Unzip to a temp dir and verify app/, public/dist/, public/prod/ exist.
#   3. Stage into /var/www/html/admin_<yyyymmdd>.
#   4. Back up the live admin dirs into /var/www/html/backup/admin_<yyyymmdd_HHMMSS>.
#   5. Copy the new dirs into /var/www/html/admin, rolling back on failure.
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
HTML_ROOT="/var/www/html"
ADMIN_DIR="$HTML_ROOT/admin"
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
  "$ARTIFACT_ROOT"/*.zip) ;;
  *) fail "artifact path is outside committed artifact root or not a zip" ;;
esac

[[ -f "$ARTIFACT_PATH" ]] || fail "artifact not found: $ARTIFACT_PATH"
command -v unzip >/dev/null 2>&1 || fail "unzip is not installed"

DATE="$(date +%Y%m%d)"
STAGING_DIR="$(mktemp -d /tmp/antd-pri-dev-stage.XXXXXX)"

WORK_DIR="$(mktemp -d /tmp/antd-pri-dev.XXXXXX)"
cleanup() {
  rm -rf "$WORK_DIR" "$STAGING_DIR"
}
trap cleanup EXIT

empty_dir "$WORK_DIR"
unzip -q "$ARTIFACT_PATH" -d "$WORK_DIR"

# The zip may wrap everything in a single top-level directory.
SRC="$WORK_DIR"
top_dirs="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 -type d)"
top_count="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 | wc -l)"
if [[ "$top_count" == "1" && -n "$top_dirs" ]]; then
  SRC="$top_dirs"
fi

for required in app public/dist public/prod; do
  [[ -d "$SRC/$required" ]] || fail "artifact missing directory: $required"
done

mkdir -p "$STAGING_DIR"
empty_dir "$STAGING_DIR"
cp -a "$SRC"/. "$STAGING_DIR"/

TS="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/admin_$TS"
mkdir -p "$BACKUP_DIR"

rollback() {
  echo "deploy failed, rolling back from $BACKUP_DIR" >&2
  for d in app public/dist public/prod; do
    if [[ -d "$BACKUP_DIR/$d" ]]; then
      rm -rf "${ADMIN_DIR:?}/$d"
      mkdir -p "$(dirname "$ADMIN_DIR/$d")"
      cp -a "$BACKUP_DIR/$d" "$ADMIN_DIR/$d"
    fi
  done
}

# Move live dirs aside first (atomic), then install the new copies.
for d in app public/dist public/prod; do
  if [[ -e "$ADMIN_DIR/$d" ]]; then
    mkdir -p "$(dirname "$BACKUP_DIR/$d")"
    mv "$ADMIN_DIR/$d" "$BACKUP_DIR/$d"
  fi
done

deploy_failed=0
for d in app public/dist public/prod; do
  mkdir -p "$(dirname "$ADMIN_DIR/$d")"
  if ! cp -a "$STAGING_DIR/$d" "$ADMIN_DIR/$d"; then
    deploy_failed=1
    break
  fi
done

if [[ "$deploy_failed" == "1" ]]; then
  rollback
  exit 1
fi

chown -R "$WEB_OWNER" "$ADMIN_DIR/app" "$ADMIN_DIR/public/dist" "$ADMIN_DIR/public/prod" 2>/dev/null || true

echo "staging: $STAGING_DIR"
echo "backup:  $BACKUP_DIR"
echo "deployed: $ADMIN_DIR/app, $ADMIN_DIR/public/dist, $ADMIN_DIR/public/prod"
