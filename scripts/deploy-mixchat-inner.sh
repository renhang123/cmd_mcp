#!/usr/bin/env bash
# mixchat deploy inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: /opt/server-shell-mcp/scripts/deploy-mixchat-inner.sh
set -euo pipefail

FIRST_ARTIFACT_PATH="${1:-}"
SECOND_ARTIFACT_PATH="${2:-}"
EXTRA_ARTIFACT_PATH="${3:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
HTML_ROOT="/var/www/html"
MIXCHAT_DIR="$HTML_ROOT/mixchat"
H5_DIR="$MIXCHAT_DIR/h5"
BACKUP_ROOT="$HTML_ROOT/backup"
WEB_OWNER="${WEB_OWNER:-root:root}"
RESTART_CONTAINERS=(mixchat-rails-1 mixchat-sidekiq-1 mixchat-mixchat_h5-1 mixchat-nginx-1)
H5_DEPLOY_DIRS=()

fail() {
  echo "$1" >&2
  exit 2
}

empty_dir() {
  local dir="$1"
  [[ -n "$dir" && -d "$dir" ]] || fail "directory is not safe to clean: $dir"
  rm -rf "$dir"/* "$dir"/.[!.]* "$dir"/..?*
}

validate_artifact() {
  local artifact_path="$1"
  local label="$2"

  [[ -n "$artifact_path" ]] || fail "$label artifact path is required"
  case "$artifact_path" in
    "$ARTIFACT_ROOT"/*.tar.gz|"$ARTIFACT_ROOT"/*.tgz) ;;
    *) fail "$label artifact path is outside committed artifact root or not a tar.gz" ;;
  esac
  [[ -f "$artifact_path" ]] || fail "$label artifact not found: $artifact_path"
}

extract_artifact() {
  local artifact_path="$1"
  local work_dir="$2"

  empty_dir "$work_dir"
  tar -xzf "$artifact_path" -C "$work_dir"

  local top_dirs
  local top_count
  top_dirs="$(find "$work_dir" -mindepth 1 -maxdepth 1 -type d)"
  top_count="$(find "$work_dir" -mindepth 1 -maxdepth 1 | wc -l)"
  if [[ "$top_count" == "1" && -n "$top_dirs" ]]; then
    printf '%s\n' "$top_dirs"
  else
    printf '%s\n' "$work_dir"
  fi
}

looks_like_h5_artifact() {
  local artifact_path="$1"
  local artifact_name
  artifact_name="$(basename "$artifact_path")"

  [[ "$artifact_name" =~ (^|[-_])h5([-_.]|$) ]]
}

restart_containers() {
  command -v docker >/dev/null 2>&1 || fail "docker is not installed"
  docker restart "${RESTART_CONTAINERS[@]}"
}

run_migrations() {
  command -v docker >/dev/null 2>&1 || fail "docker is not installed"
  docker exec mixchat-rails-1 bundle exec rails db:migrate
}

stage_artifact() {
  local artifact_path="$1"
  local src="$2"
  local h5_dirs=()

  for d in .next public node_modules; do
    if [[ -d "$src/$d" ]]; then
      h5_dirs+=("$d")
    fi
  done

  if (( ${#h5_dirs[@]} > 0 )); then
    [[ "$DEPLOY_H5" == "0" ]] || fail "multiple h5 artifacts were provided"
    DEPLOY_H5=1
    mkdir -p "$STAGING_DIR/h5"
    empty_dir "$STAGING_DIR/h5"
    H5_DEPLOY_DIRS=("${h5_dirs[@]}")
    for d in "${H5_DEPLOY_DIRS[@]}"; do
      cp -a "$src/$d" "$STAGING_DIR/h5/$d"
    done
    return
  fi

  if looks_like_h5_artifact "$artifact_path"; then
    fail "h5 artifact missing deployable directory: .next, public, or node_modules"
  fi

  [[ "$DEPLOY_APP" == "0" ]] || fail "multiple app artifacts were provided"
  DEPLOY_APP=1
  mkdir -p "$STAGING_DIR/app"
  empty_dir "$STAGING_DIR/app"
  cp -a "$src"/. "$STAGING_DIR/app/"
}

command -v tar >/dev/null 2>&1 || fail "tar is not installed"
[[ -n "$FIRST_ARTIFACT_PATH" ]] || fail "artifact path is required"
[[ -z "$EXTRA_ARTIFACT_PATH" ]] || fail "at most two artifact paths are supported"
validate_artifact "$FIRST_ARTIFACT_PATH" "first"
if [[ -n "$SECOND_ARTIFACT_PATH" ]]; then
  validate_artifact "$SECOND_ARTIFACT_PATH" "second"
fi

STAGING_DIR="$(mktemp -d /tmp/mixchat-stage.XXXXXX)"
FIRST_WORK_DIR="$(mktemp -d /tmp/mixchat-first.XXXXXX)"
SECOND_WORK_DIR="$(mktemp -d /tmp/mixchat-second.XXXXXX)"
DEPLOY_APP=0
DEPLOY_H5=0
cleanup() {
  rm -rf "$FIRST_WORK_DIR" "$SECOND_WORK_DIR" "$STAGING_DIR"
}
trap cleanup EXIT

FIRST_SRC="$(extract_artifact "$FIRST_ARTIFACT_PATH" "$FIRST_WORK_DIR")"
stage_artifact "$FIRST_ARTIFACT_PATH" "$FIRST_SRC"
if [[ -n "$SECOND_ARTIFACT_PATH" ]]; then
  SECOND_SRC="$(extract_artifact "$SECOND_ARTIFACT_PATH" "$SECOND_WORK_DIR")"
  stage_artifact "$SECOND_ARTIFACT_PATH" "$SECOND_SRC"
fi

TS="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/mixchat_$TS"
mkdir -p "$BACKUP_DIR"

rollback() {
  echo "deploy failed, rolling back from $BACKUP_DIR" >&2
  if [[ "$DEPLOY_APP" == "1" ]]; then
    if [[ -d "$MIXCHAT_DIR/app/storage" && -d "$BACKUP_DIR/app" && ! -e "$BACKUP_DIR/app/storage" ]]; then
      mv "$MIXCHAT_DIR/app/storage" "$BACKUP_DIR/app/storage"
    fi
    rm -rf "${MIXCHAT_DIR:?}/app"
    if [[ -d "$BACKUP_DIR/app" ]]; then
      cp -a "$BACKUP_DIR/app" "$MIXCHAT_DIR/app"
    fi
  fi
  if [[ "$DEPLOY_H5" == "1" ]]; then
    for d in "${H5_DEPLOY_DIRS[@]}"; do
      rm -rf "${H5_DIR:?}/$d"
      if [[ -d "$BACKUP_DIR/h5/$d" ]]; then
        mkdir -p "$H5_DIR"
        cp -a "$BACKUP_DIR/h5/$d" "$H5_DIR/$d"
      fi
    done
  fi
}

mkdir -p "$MIXCHAT_DIR"
if [[ "$DEPLOY_APP" == "1" && -e "$MIXCHAT_DIR/app" ]]; then
  mv "$MIXCHAT_DIR/app" "$BACKUP_DIR/app"
fi
if [[ "$DEPLOY_H5" == "1" ]]; then
  mkdir -p "$H5_DIR" "$BACKUP_DIR/h5"
  for d in "${H5_DEPLOY_DIRS[@]}"; do
    if [[ -e "$H5_DIR/$d" ]]; then
      mv "$H5_DIR/$d" "$BACKUP_DIR/h5/$d"
    fi
  done
fi

deploy_failed=0
if [[ "$DEPLOY_APP" == "1" ]]; then
  if ! cp -a "$STAGING_DIR/app" "$MIXCHAT_DIR/app"; then
    deploy_failed=1
  elif [[ -d "$BACKUP_DIR/app/storage" ]]; then
    rm -rf "$MIXCHAT_DIR/app/storage"
    if ! mv "$BACKUP_DIR/app/storage" "$MIXCHAT_DIR/app/storage"; then
      deploy_failed=1
    fi
  fi
fi
if [[ "$deploy_failed" == "0" && "$DEPLOY_H5" == "1" ]]; then
  for d in "${H5_DEPLOY_DIRS[@]}"; do
    if ! cp -a "$STAGING_DIR/h5/$d" "$H5_DIR/$d"; then
      deploy_failed=1
      break
    fi
  done
fi

if [[ "$deploy_failed" == "1" ]]; then
  rollback
  exit 1
fi

if [[ "$DEPLOY_APP" == "1" ]]; then
  chown -R "$WEB_OWNER" "$MIXCHAT_DIR/app" 2>/dev/null || true
fi
if [[ "$DEPLOY_H5" == "1" ]]; then
  for d in "${H5_DEPLOY_DIRS[@]}"; do
    chown -R "$WEB_OWNER" "$H5_DIR/$d" 2>/dev/null || true
  done
fi

restart_output=""
if ! restart_output="$(restart_containers 2>&1)"; then
  restart_output="restart warning: ${restart_output}"
fi

migrate_output=""
if [[ "$restart_output" != restart\ warning:* ]]; then
  if ! migrate_output="$(run_migrations 2>&1)"; then
    printf '%s\n' "$migrate_output" >&2
    exit 1
  fi
else
  migrate_output="migration skipped because container restart failed"
fi

deployed_paths=()
if [[ "$DEPLOY_APP" == "1" ]]; then
  deployed_paths+=("$MIXCHAT_DIR/app")
fi
if [[ "$DEPLOY_H5" == "1" ]]; then
  for d in "${H5_DEPLOY_DIRS[@]}"; do
    deployed_paths+=("$H5_DIR/$d")
  done
fi

echo "staging: $STAGING_DIR"
echo "backup:  $BACKUP_DIR"
echo "deployed: ${deployed_paths[*]}"
if [[ "$restart_output" == restart\ warning:* ]]; then
  echo "$restart_output"
else
  echo "restarted: ${RESTART_CONTAINERS[*]}"
  printf '%s\n' "$restart_output"
fi
echo "migrated: mixchat-rails-1"
printf '%s\n' "$migrate_output"
