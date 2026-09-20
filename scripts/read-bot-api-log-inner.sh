#!/usr/bin/env bash
# bot_api log reader inner script. Runs as root via the pinned sudoers rule:
#   cmd_mcp ALL=(root) NOPASSWD: /opt/server-shell-mcp/scripts/read-bot-api-log-inner.sh
#
# Usage: read-bot-api-log-inner.sh <action> <file> <lines> [-e <keyword>]...
#   action:
#     list - list files under $LOG_ROOT (mtime, size, path), newest first.
#     tail - read the last <lines> lines of <file>; optional -e keywords act as
#            a case-insensitive fixed-string OR filter on the tailed output.
#   file  : path under $LOG_ROOT. list ignores it (pass the root itself).
#   lines : 1-2000, tail only.
#
# Security: even though MCP config validation already ran, every argument is
# re-validated here. Paths are canonicalized with readlink -f and must stay
# under the real log root, so a symlink inside the log dir cannot escape it.
set -euo pipefail

LOG_ROOT="/home/api/mixbot_api/Log"
MAX_LINES=2000
LIST_LIMIT=100
KEYWORD_MAX_LENGTH=128

fail() {
  echo "read-bot-api-log: $1" >&2
  exit 2
}

ACTION="${1:-}"
FILE_ARG="${2:-}"
LINES_ARG="${3:-}"

[[ "$ACTION" == "list" || "$ACTION" == "tail" ]] || fail "action must be list or tail"
[[ -n "$FILE_ARG" ]] || fail "file argument is required"
case "$LINES_ARG" in
  ''|*[!0-9]*) fail "lines must be a positive integer" ;;
esac
[[ "$LINES_ARG" -ge 1 && "$LINES_ARG" -le "$MAX_LINES" ]] || fail "lines must be between 1 and $MAX_LINES"

[[ -d "$LOG_ROOT" ]] || fail "log root not found: $LOG_ROOT"
LOG_ROOT_REAL="$(readlink -f -- "$LOG_ROOT")" || fail "cannot resolve log root: $LOG_ROOT"

# readlink -f 在中间目录不存在时会直接失败，这里兜住，保证拒绝路径也走统一的
# rc=2 诊断输出，而不是 set -e 静默退出。
TARGET_REAL="$(readlink -f -- "$FILE_ARG")" || fail "cannot resolve path: $FILE_ARG"
case "$TARGET_REAL" in
  "$LOG_ROOT_REAL"|"$LOG_ROOT_REAL"/*) ;;
  *) fail "path is outside log root: $FILE_ARG" ;;
esac

# Keywords arrive as -e <keyword> pairs rendered by the argv template.
KEYWORDS=()
if [[ "$#" -gt 3 ]]; then
  [[ "$ACTION" == "tail" ]] || fail "keywords are only valid with action=tail"
  remaining=$(( $# - 3 ))
  [[ $((remaining % 2)) -eq 0 ]] || fail "keywords must come as -e <keyword> pairs"
  while [[ "$#" -gt 3 ]]; do
    [[ "$4" == "-e" ]] || fail "unexpected argument: $4"
    keyword="$5"
    [[ -n "$keyword" ]] || fail "keyword must not be empty"
    [[ "${#keyword}" -le "$KEYWORD_MAX_LENGTH" ]] || fail "keyword is too long"
    KEYWORDS+=("$keyword")
    shift 2
  done
fi

if [[ "$ACTION" == "list" ]]; then
  entries="$(find "$LOG_ROOT_REAL" -type f -printf '%T@\t%TY-%Tm-%Td %TH:%TM\t%s\t%p\n' \
    | sort -rn | head -n "$LIST_LIMIT" | cut -f2-)"
  if [[ -z "$entries" ]]; then
    echo "(no log files found under $LOG_ROOT)"
    exit 0
  fi
  printf '%s\n' "$entries"
  exit 0
fi

[[ -f "$TARGET_REAL" ]] || fail "not a regular file (use action=list to see files): $FILE_ARG"

output="$(tail -n "$LINES_ARG" -- "$TARGET_REAL")" || fail "cannot read log file: $FILE_ARG"

if [[ "${#KEYWORDS[@]}" -eq 0 ]]; then
  if [[ -z "$output" ]]; then
    echo "(log file is empty)"
  else
    printf '%s\n' "$output"
  fi
  exit 0
fi

rc=0
# 每个关键词都必须用 -e 声明：grep 的位置参数只有第一个是模式，其余会被当成文件名
# （`grep -F -- a b` 会去找名为 b 的文件）。-e 之后的参数总按模式解析，无需 --，
# 也不能加 --，否则 -e 自身会被当作模式。
GREP_ARGS=()
for keyword in "${KEYWORDS[@]}"; do
  GREP_ARGS+=("-e" "$keyword")
done
matched="$(printf '%s\n' "$output" | grep -i -F "${GREP_ARGS[@]}")" || rc=$?
if [[ "$rc" -eq 1 ]]; then
  echo "(no lines matched keywords in the last $LINES_ARG lines)"
  exit 0
fi
[[ "$rc" -eq 0 ]] || fail "keyword filter failed (rc=$rc)"
printf '%s\n' "$matched"
