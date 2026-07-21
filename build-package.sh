#!/usr/bin/env bash
set -euo pipefail

GOOS_TARGET="${GOOS_TARGET:-linux}"
GOARCH_TARGET="${GOARCH_TARGET:-amd64}"
PACKAGE_NAME="${PACKAGE_NAME:-server-shell-mcp}"
VERSION="${VERSION:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
COMMANDS_FILE="${COMMANDS_FILE:-configs/commands.example.json}"
MCP_PUBLIC_KEY_FILE="${MCP_PUBLIC_KEY_FILE:-}"
DIST_DIR="${DIST_DIR:-dist}"
PACKAGE_DIR="$DIST_DIR/$PACKAGE_NAME-$VERSION-$GOOS_TARGET-$GOARCH_TARGET"
ARCHIVE="$DIST_DIR/$PACKAGE_NAME-$VERSION-$GOOS_TARGET-$GOARCH_TARGET.tar.gz"

if [[ ! -f "$COMMANDS_FILE" ]]; then
  echo "commands config not found: $COMMANDS_FILE" >&2
  exit 1
fi

if [[ -n "$MCP_PUBLIC_KEY_FILE" && ! -f "$MCP_PUBLIC_KEY_FILE" ]]; then
  echo "public key file not found: $MCP_PUBLIC_KEY_FILE" >&2
  exit 1
fi

rm -rf "$PACKAGE_DIR"
mkdir -p "$PACKAGE_DIR"

echo "Building $PACKAGE_DIR/server-shell-mcp"
GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o "$PACKAGE_DIR/server-shell-mcp" ./cmd/server

cp "$COMMANDS_FILE" "$PACKAGE_DIR/commands.json"
if [[ -n "$MCP_PUBLIC_KEY_FILE" ]]; then
  cp "$MCP_PUBLIC_KEY_FILE" "$PACKAGE_DIR/authorized_key.pub"
fi
cp deploy-remote.sh "$PACKAGE_DIR/install.sh"
chmod +x "$PACKAGE_DIR/install.sh" "$PACKAGE_DIR/server-shell-mcp"

tar -czf "$ARCHIVE" -C "$DIST_DIR" "$(basename "$PACKAGE_DIR")"

echo "Package created: $ARCHIVE"
echo "Install on server: tar -xzf $(basename "$ARCHIVE") && cd $(basename "$PACKAGE_DIR") && ./install.sh"
