#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/server-shell-mcp}"
CONFIG_DIR="${CONFIG_DIR:-/etc/server-shell-mcp}"
COMMANDS_FILE="${COMMANDS_FILE:-configs/commands.example.json}"
BINARY_NAME="${BINARY_NAME:-server-shell-mcp}"
DIST_DIR="${DIST_DIR:-dist}"

if [[ ! -f "$COMMANDS_FILE" ]]; then
  echo "commands config not found: $COMMANDS_FILE" >&2
  exit 1
fi

mkdir -p "$DIST_DIR"
BINARY_PATH="$DIST_DIR/$BINARY_NAME"

echo "Building $BINARY_PATH"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BINARY_PATH" ./cmd/server

echo "Installing under $INSTALL_DIR and $CONFIG_DIR"
sudo install -d -m 0755 "$INSTALL_DIR" "$CONFIG_DIR"
sudo install -m 0755 "$BINARY_PATH" "$INSTALL_DIR/server"
sudo install -m 0640 "$COMMANDS_FILE" "$CONFIG_DIR/commands.json"

echo "Testing MCP initialize response"
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n' | \
  "$INSTALL_DIR/server" -commands "$CONFIG_DIR/commands.json"

echo "Installation complete"
echo "MCP command: $INSTALL_DIR/server -commands $CONFIG_DIR/commands.json"
