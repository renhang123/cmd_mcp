#!/usr/bin/env bash
# 在目标机以 root 执行：把 ari-bot 部署 profile 装进已存在的 server-shell-mcp。
#
# 用法（目标机上）:
#   sudo ./install-ari-bot-profile.sh [commands.json]
#
# 默认使用同目录的 commands.antd-pri-dev.json（含 antd-pri-dev/mixbot/mixchat/ari-bot 四个 profile）。
# 只安装/更新 ari-bot 相关文件，不动其它 profile；commands.json 安装前会备份。
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/opt/server-shell-mcp}"
CONFIG_DIR="${CONFIG_DIR:-/etc/server-shell-mcp}"
MCP_USER="${MCP_USER:-cmd_mcp}"
SUDOERS_FILE="${SUDOERS_FILE:-/etc/sudoers.d/server-shell-mcp}"
COMMANDS_SRC="${1:-$SRC_DIR/commands.antd-pri-dev.json}"

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

[[ -x "$INSTALL_DIR/server" ]] || fail "未找到 $INSTALL_DIR/server，请先安装 server-shell-mcp"
[[ -f "$COMMANDS_SRC" ]] || fail "未找到配置文件: $COMMANDS_SRC"
for f in deploy-ari-bot.sh deploy-ari-bot-inner.sh; do
    [[ -f "$SRC_DIR/$f" ]] || fail "缺少文件: $SRC_DIR/$f"
done

if command -v python3 >/dev/null 2>&1; then
    python3 -c '
import json,sys
d=json.load(open(sys.argv[1]))
assert any(p.get("id")=="ari-bot" for p in d.get("deploy_profiles",[])), "ari-bot profile missing"
assert any(c.get("id")=="deploy_ari_bot" for c in d.get("commands",[])), "deploy_ari_bot command missing"
' "$COMMANDS_SRC" || fail "配置文件缺少 ari-bot profile 或 deploy_ari_bot 命令"
fi

install -d -m 0755 "$INSTALL_DIR/scripts"
install -m 0755 "$SRC_DIR/deploy-ari-bot.sh" "$INSTALL_DIR/scripts/deploy-ari-bot.sh"
install -m 0755 "$SRC_DIR/deploy-ari-bot-inner.sh" "$INSTALL_DIR/scripts/deploy-ari-bot-inner.sh"
info "部署脚本已安装到 $INSTALL_DIR/scripts/"

install -d -m 0755 "$CONFIG_DIR"
TS="$(date +%Y%m%d%H%M%S)"
if [[ -f "$CONFIG_DIR/commands.json" ]]; then
    cp -a "$CONFIG_DIR/commands.json" "$CONFIG_DIR/commands.json.bak-$TS"
    info "已备份旧配置: $CONFIG_DIR/commands.json.bak-$TS"
fi
install -m 0644 "$COMMANDS_SRC" "$CONFIG_DIR/commands.json"
info "commands.json 已更新: $CONFIG_DIR/commands.json"

mkdir -p "$(dirname "$SUDOERS_FILE")"
SUDO_LINE="$MCP_USER ALL=(root) NOPASSWD: $INSTALL_DIR/scripts/deploy-ari-bot-inner.sh"
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
info "完成。ari-bot profile 已可用："
echo "  deploy_profile: ari-bot  (artifact 名称需匹配 ^ari-bot-.*\\.(tar\\.gz|tgz)\$)"
echo "  部署命令:       deploy_ari_bot → $INSTALL_DIR/scripts/deploy-ari-bot.sh"
echo "  部署目标:       /var/www/html/ari-bot/ari-bot，重启方式 manage.sh restart"
