#!/usr/bin/env bash
set -euo pipefail

ARTIFACT_PATH="${1:-}"
ARTIFACT_ROOT="/home/cmd_mcp/artifacts/committed"
DEPLOY_ROOT="/home/cmd_mcp/deployments/server-shell-mcp-test"

if [[ -z "$ARTIFACT_PATH" ]]; then
  echo "artifact path is required" >&2
  exit 2
fi

case "$ARTIFACT_PATH" in
  "$ARTIFACT_ROOT"/*.tar.gz) ;;
  *)
    echo "artifact path is outside committed artifact root" >&2
    exit 2
    ;;
esac

if [[ ! -f "$ARTIFACT_PATH" ]]; then
  echo "artifact not found: $ARTIFACT_PATH" >&2
  exit 2
fi

WORK_DIR="$(mktemp -d /home/cmd_mcp/deploy-server-shell-mcp-test.XXXXXX)"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

tar -xzf "$ARTIFACT_PATH" -C "$WORK_DIR"
PACKAGE_DIR="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
if [[ -z "$PACKAGE_DIR" ]]; then
  echo "artifact does not contain a package directory" >&2
  exit 2
fi

for required in server-shell-mcp commands.json install.sh; do
  if [[ ! -f "$PACKAGE_DIR/$required" ]]; then
    echo "artifact missing $required" >&2
    exit 2
  fi
done

mkdir -p "$DEPLOY_ROOT/releases"
RELEASE_ID="$(basename "$ARTIFACT_PATH" .tar.gz)"
TARGET_DIR="$DEPLOY_ROOT/releases/$RELEASE_ID"
rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"
cp -R "$PACKAGE_DIR"/. "$TARGET_DIR"/
chmod +x "$TARGET_DIR/server-shell-mcp" "$TARGET_DIR/install.sh"
ln -sfn "$TARGET_DIR" "$DEPLOY_ROOT/current"

printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n' | \
  "$TARGET_DIR/server-shell-mcp" -commands "$TARGET_DIR/commands.json" >/dev/null

echo "deployed staged release: $RELEASE_ID"
echo "current: $DEPLOY_ROOT/current"
