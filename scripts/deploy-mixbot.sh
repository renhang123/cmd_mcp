#!/usr/bin/env bash
set -euo pipefail

exec /usr/bin/sudo -n /opt/server-shell-mcp/scripts/deploy-mixbot-inner.sh "$@"
