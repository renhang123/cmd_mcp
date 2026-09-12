#!/usr/bin/env bash
# 兼容入口（历史名称）：等价于
#   ./install-deploy-profile.sh ari-bot [commands.json]
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SRC_DIR/install-deploy-profile.sh" ari-bot "${1:-$SRC_DIR/commands.antd-pri-dev.json}"
