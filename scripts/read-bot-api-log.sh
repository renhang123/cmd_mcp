#!/usr/bin/env bash
set -euo pipefail

exec /usr/bin/sudo -n /opt/server-shell-mcp/scripts/read-bot-api-log-inner.sh "$@"
