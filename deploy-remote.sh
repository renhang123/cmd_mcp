#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/opt/server-shell-mcp}"
CONFIG_DIR="${CONFIG_DIR:-/etc/server-shell-mcp}"
CONFIG_MODE="${CONFIG_MODE:-0644}"
MCP_USER="${MCP_USER:-cmd_mcp}"
BINARY_FILE="${BINARY_FILE:-$SCRIPT_DIR/server-shell-mcp}"
COMMANDS_FILE="${COMMANDS_FILE:-$SCRIPT_DIR/commands.json}"
TEST_DEPLOY_HELPER="${TEST_DEPLOY_HELPER:-$SCRIPT_DIR/deploy-server-shell-mcp-test.sh}"
SCRIPTS_DIR="${SCRIPTS_DIR:-$SCRIPT_DIR/scripts}"
MCP_PUBLIC_KEY="${MCP_PUBLIC_KEY:-}"
MCP_PUBLIC_KEY_FILE="${MCP_PUBLIC_KEY_FILE:-}"
FORCED_COMMAND="${INSTALL_DIR}/server -commands ${CONFIG_DIR}/commands.json"

if [[ ! -f "$BINARY_FILE" ]]; then
  echo "binary not found: $BINARY_FILE" >&2
  exit 1
fi

if [[ ! -f "$COMMANDS_FILE" ]]; then
  echo "commands config not found: $COMMANDS_FILE" >&2
  exit 1
fi

if [[ -z "$MCP_PUBLIC_KEY" ]]; then
  if [[ -n "$MCP_PUBLIC_KEY_FILE" ]]; then
    MCP_PUBLIC_KEY="$(<"$MCP_PUBLIC_KEY_FILE")"
  elif [[ -f "$SCRIPT_DIR/authorized_key.pub" ]]; then
    MCP_PUBLIC_KEY="$(<"$SCRIPT_DIR/authorized_key.pub")"
  fi
fi

if [[ -z "$MCP_PUBLIC_KEY" ]]; then
  echo "MCP_PUBLIC_KEY, MCP_PUBLIC_KEY_FILE, or $SCRIPT_DIR/authorized_key.pub is required" >&2
  exit 1
fi

if ! id -u "$MCP_USER" >/dev/null 2>&1; then
  echo "Creating user $MCP_USER"
  sudo useradd --system --create-home --shell /bin/sh "$MCP_USER"
fi
sudo usermod -p "$(openssl passwd -6 "$(openssl rand -base64 32)")" -s /bin/sh "$MCP_USER"

MCP_HOME="$(getent passwd "$MCP_USER" | cut -d: -f6)"
if [[ -z "$MCP_HOME" ]]; then
  echo "cannot determine home directory for $MCP_USER" >&2
  exit 1
fi

SSH_DIR="$MCP_HOME/.ssh"
AUTHORIZED_KEYS="$SSH_DIR/authorized_keys"
MARKER_BEGIN="# server-shell-mcp managed key begin"
MARKER_END="# server-shell-mcp managed key end"
sudo install -d -m 0700 -o "$MCP_USER" -g "$MCP_USER" "$SSH_DIR"

TMP_AUTHORIZED_KEYS="$(mktemp)"
if sudo test -f "$AUTHORIZED_KEYS"; then
  sudo awk -v begin="$MARKER_BEGIN" -v end="$MARKER_END" '
    $0 == begin {skip=1; next}
    $0 == end {skip=0; next}
    !skip {print}
  ' "$AUTHORIZED_KEYS" | tee "$TMP_AUTHORIZED_KEYS" >/dev/null
fi

{
  cat "$TMP_AUTHORIZED_KEYS"
  printf '%s\n' "$MARKER_BEGIN"
  printf 'command="%s",no-pty,no-agent-forwarding,no-X11-forwarding,no-port-forwarding %s\n' "$FORCED_COMMAND" "$MCP_PUBLIC_KEY"
  printf '%s\n' "$MARKER_END"
} > "$TMP_AUTHORIZED_KEYS.next"

echo "Cleaning previous installation"
sudo rm -f "$INSTALL_DIR/server" "$CONFIG_DIR/commands.json"

echo "Installing under $INSTALL_DIR and $CONFIG_DIR"
sudo install -d -m 0755 "$INSTALL_DIR" "$CONFIG_DIR"
sudo install -m 0755 "$BINARY_FILE" "$INSTALL_DIR/server"
if [[ -d "$SCRIPTS_DIR" ]]; then
  sudo install -d -m 0755 "$INSTALL_DIR/scripts"
  for script in "$SCRIPTS_DIR"/*.sh; do
    [[ -f "$script" ]] || continue
    sudo install -m 0755 "$script" "$INSTALL_DIR/scripts/$(basename "$script")"
  done
fi
sudo install -m "$CONFIG_MODE" "$COMMANDS_FILE" "$CONFIG_DIR/commands.json"
if [[ -f "$TEST_DEPLOY_HELPER" ]]; then
  sudo install -m 0755 -o "$MCP_USER" -g "$MCP_USER" "$TEST_DEPLOY_HELPER" "$MCP_HOME/deploy-server-shell-mcp-test.sh"
fi
sudo install -d -m 0750 -o "$MCP_USER" -g "$MCP_USER" "$MCP_HOME/artifacts" "$MCP_HOME/deployments"
sudo install -m 0600 "$TMP_AUTHORIZED_KEYS.next" "$AUTHORIZED_KEYS"
sudo chown -R "$MCP_USER:$MCP_USER" "$SSH_DIR"
sudo chmod 0700 "$SSH_DIR"
rm -f "$TMP_AUTHORIZED_KEYS" "$TMP_AUTHORIZED_KEYS.next"

echo "Testing MCP initialize response"
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n' | \
  sudo -u "$MCP_USER" "$INSTALL_DIR/server" -commands "$CONFIG_DIR/commands.json"

echo "Installation complete"
echo "MCP user: $MCP_USER"
echo "SSH command: ssh -T $MCP_USER@<server-host>"
echo "MCP command: $FORCED_COMMAND"
