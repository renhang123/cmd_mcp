#!/usr/bin/env bash
# 在目标机以 root 执行：把某个 deploy profile 装进已存在的 server-shell-mcp。
#
# 用法（目标机上）:
#   sudo ./install-deploy-profile.sh <profile-id> [commands.json]
#   例: sudo ./install-deploy-profile.sh bot-api /tmp/commands.antd-pri-dev.json
#
# 约定: profile `bot-api` 对应 scripts/deploy-bot-api.sh + deploy-bot-api-inner.sh。
# 只安装该 profile 相关文件与 sudoers 规则，不覆盖其它 profile；commands.json 安装前会备份。
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROFILE="${1:-}"
COMMANDS_SRC="${2:-$SRC_DIR/commands.antd-pri-dev.json}"

INSTALL_DIR="${INSTALL_DIR:-/opt/server-shell-mcp}"
CONFIG_DIR="${CONFIG_DIR:-/etc/server-shell-mcp}"
MCP_USER="${MCP_USER:-cmd_mcp}"
SUDOERS_FILE="${SUDOERS_FILE:-/etc/sudoers.d/server-shell-mcp}"

info() { printf '[install] %s\n' "$*"; }
fail() { printf '[install] %s\n' "$*" >&2; exit 1; }

run_as_mcp() {
    if command -v sudo >/dev/null 2>&1; then
        sudo -u "$MCP_USER" "$@"
    else
        runuser -u "$MCP_USER" -- "$@"
    fi
}

[[ "$(id -u)" -eq 0 ]] || fail "需要 root 权限执行"
[[ -n "$PROFILE" ]] || fail "用法: $0 <profile-id> [commands.json]"

WRAPPER="$SRC_DIR/deploy-$PROFILE.sh"
INNER="$SRC_DIR/deploy-$PROFILE-inner.sh"
INNER_NAME="$(basename "$INNER")"

[[ -x "$INSTALL_DIR/server" ]] || fail "未找到 $INSTALL_DIR/server，请先安装 server-shell-mcp"
[[ -f "$COMMANDS_SRC" ]] || fail "未找到配置文件: $COMMANDS_SRC"
[[ -f "$WRAPPER" ]] || fail "缺少文件: $WRAPPER"
[[ -f "$INNER" ]] || fail "缺少文件: $INNER"

if command -v python3 >/dev/null 2>&1; then
    python3 -c '
import json,sys
profile, command = sys.argv[2], sys.argv[3]
d = json.load(open(sys.argv[1]))
assert any(p.get("id") == profile for p in d.get("deploy_profiles", [])), "profile %s missing" % profile
assert any(c.get("id") == command for c in d.get("commands", [])), "command %s missing" % command
' "$COMMANDS_SRC" "$PROFILE" "deploy_${PROFILE//-/_}" || fail "配置文件缺少 profile $PROFILE 或其 deploy 命令"
fi

install -d -m 0755 "$INSTALL_DIR/scripts"
install -m 0755 "$WRAPPER" "$INSTALL_DIR/scripts/$(basename "$WRAPPER")"
install -m 0755 "$INNER" "$INSTALL_DIR/scripts/$INNER_NAME"
info "部署脚本已安装: $INSTALL_DIR/scripts/$(basename "$WRAPPER") + $INNER_NAME"

install -d -m 0755 "$CONFIG_DIR"
TS="$(date +%Y%m%d%H%M%S)"
if [[ -f "$CONFIG_DIR/commands.json" ]]; then
    cp -a "$CONFIG_DIR/commands.json" "$CONFIG_DIR/commands.json.bak-$TS"
    info "已备份旧配置: $CONFIG_DIR/commands.json.bak-$TS"
fi
install -m 0644 "$COMMANDS_SRC" "$CONFIG_DIR/commands.json"
info "commands.json 已更新: $CONFIG_DIR/commands.json"

mkdir -p "$(dirname "$SUDOERS_FILE")"
SUDO_LINE="$MCP_USER ALL=(root) NOPASSWD: $INSTALL_DIR/scripts/$INNER_NAME"
if [[ -f "$SUDOERS_FILE" ]] && grep -qxF "$SUDO_LINE" "$SUDOERS_FILE"; then
    info "sudoers 规则已存在，跳过"
else
    printf '%s\n' "$SUDO_LINE" >> "$SUDOERS_FILE"
    chmod 0440 "$SUDOERS_FILE"
    if command -v visudo >/dev/null 2>&1; then
        visudo -cf "$SUDOERS_FILE" >/dev/null || fail "sudoers 校验失败: $SUDOERS_FILE"
        info "已追加 sudoers 规则并校验通过"
    else
        info "已追加 sudoers 规则（本机没有 visudo，跳过语法校验）"
    fi
fi

info "校验 MCP 工具列表..."
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n' \
    | run_as_mcp "$INSTALL_DIR/server" -commands "$CONFIG_DIR/commands.json" \
    | grep -o '"name":"[a-zA-Z0-9_]*"' | sed 's/"name":"//;s/"//' | sed 's/^/  - /'

echo
info "完成。profile $PROFILE 已可用。"
