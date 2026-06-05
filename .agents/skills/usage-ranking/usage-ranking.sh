#!/bin/bash
# usage-ranking.sh — 查询用量排名并发送飞书卡片
# 用法: usage-ranking.sh <time_range>
# time_range: yesterday | today | last_week | week | month | year | all

set -euo pipefail

TIME_RANGE="${1:-yesterday}"
CHAT_ID="oc_3b1ef174c26d2b2bfac488571b89ce21"
export PATH="/opt/data/home/bin:/opt/hermes/.venv/bin:/usr/local/bin:$PATH"
export HOME="/opt/data/home"
export HERMES_HOME="/opt/data"

run_sql() {
  docker exec postgres psql -U root -d new-api -t -A -F '|' -c "$1"
}

# --- Period label with dates ---
START_DATE=""
END_DATE=""
case "$TIME_RANGE" in
  yesterday)
    START_DATE=$(date -d 'yesterday 00:00:00' '+%Y年%m月%d日')
    END_DATE="$START_DATE"
    PERIOD_LABEL="${START_DATE}(00:00:00-23:59:59)"
    ;;
  today)
    START_DATE=$(date -d 'today 00:00:00' '+%Y年%m月%d日')
    END_DATE="$START_DATE"
    PERIOD_LABEL="${START_DATE}(00:00:00-23:59:59)"
    ;;
  last_week)
    START_DATE=$(date -d '7 days ago 00:00:00' '+%Y年%m月%d日')
    END_DATE=$(date -d 'yesterday 00:00:00' '+%Y年%m月%d日')
    PERIOD_LABEL="${START_DATE}-${END_DATE}"
    ;;
  week)
    START_DATE=$(date -d '7 days ago 00:00:00' '+%Y年%m月%d日')
    END_DATE=$(date '+%Y年%m月%d日')
    PERIOD_LABEL="${START_DATE}-${END_DATE}"
    ;;
  month)
    START_DATE=$(date -d '30 days ago 00:00:00' '+%Y年%m月%d日')
    END_DATE=$(date '+%Y年%m月%d日')
    PERIOD_LABEL="${START_DATE}-${END_DATE}"
    ;;
  year)
    START_DATE=$(date -d '365 days ago 00:00:00' '+%Y年%m月%d日')
    END_DATE=$(date '+%Y年%m月%d日')
    PERIOD_LABEL="${START_DATE}-${END_DATE}"
    ;;
  all)
    PERIOD_LABEL="全部时间"
    ;;
  *)
    PERIOD_LABEL="$TIME_RANGE"
    ;;
esac

# --- Compute timestamps ---
case "$TIME_RANGE" in
  yesterday)
    START_TS=$(date -d 'yesterday 00:00:00' +%s)
    END_TS=$(date -d 'today 00:00:00' +%s)
    TIME_COND="AND l.created_at >= ${START_TS} AND l.created_at < ${END_TS}"
    ;;
  today)
    START_TS=$(date -d 'today 00:00:00' +%s)
    TIME_COND="AND l.created_at >= ${START_TS}"
    ;;
  last_week)
    START_TS=$(date -d '7 days ago 00:00:00' +%s)
    END_TS=$(date -d 'today 00:00:00' +%s)
    TIME_COND="AND l.created_at >= ${START_TS} AND l.created_at < ${END_TS}"
    ;;
  week)   START_TS=$(( $(date +%s) - 7*86400 ));  TIME_COND="AND l.created_at >= ${START_TS}" ;;
  month)  START_TS=$(( $(date +%s) - 30*86400 )); TIME_COND="AND l.created_at >= ${START_TS}" ;;
  year)   START_TS=$(( $(date +%s) - 365*86400 )); TIME_COND="AND l.created_at >= ${START_TS}" ;;
  all)    TIME_COND="" ;;
  *)      START_TS=$(( $(date +%s) - 7*86400 )); TIME_COND="AND l.created_at >= ${START_TS}" ;;
esac

# --- Read system config ---
CONFIG=$(run_sql "SELECT key, value FROM options WHERE key IN ('QuotaPerUnit','USDExchangeRate','general_setting.quota_display_type','general_setting.custom_currency_exchange_rate','general_setting.custom_currency_symbol');")

QUOTA_PER_UNIT=500000
EXCHANGE_RATE=7.0
DISPLAY_TYPE="CNY"
CUSTOM_RATE=1.0
CUSTOM_SYMBOL=""

while IFS='|' read -r key value; do
  case "$key" in
    QuotaPerUnit) QUOTA_PER_UNIT="$value" ;;
    USDExchangeRate) EXCHANGE_RATE="$value" ;;
    general_setting.quota_display_type) DISPLAY_TYPE="$value" ;;
    general_setting.custom_currency_exchange_rate) CUSTOM_RATE="$value" ;;
    general_setting.custom_currency_symbol) CUSTOM_SYMBOL="$value" ;;
  esac
done <<< "$CONFIG"

case "$DISPLAY_TYPE" in
  USD)   RATE=1;           CURRENCY="\$" ;;
  CNY)   RATE="$EXCHANGE_RATE"; CURRENCY="¥" ;;
  CUSTOM) RATE="$CUSTOM_RATE"; CURRENCY="${CUSTOM_SYMBOL:-¥}" ;;
  TOKENS) RATE=1;           CURRENCY="tokens" ;;
  *)      RATE="$EXCHANGE_RATE"; CURRENCY="¥" ;;
esac

# --- Query account ranking ---
ACCT_SQL="SELECT u.username, COALESCE(u.display_name, '') AS display_name, COUNT(l.id) AS call_count, ROUND(COALESCE(SUM(l.prompt_tokens + l.completion_tokens), 0)::numeric / 1000000.0, 2) AS tokens_m, ROUND(COALESCE(SUM(l.quota), 0)::numeric / ${QUOTA_PER_UNIT} * ${RATE}, 2) AS cost FROM users u LEFT JOIN logs l ON l.user_id = u.id AND l.type = 2 ${TIME_COND} WHERE u.role < 10 GROUP BY u.id, u.username, u.display_name ORDER BY COALESCE(SUM(l.prompt_tokens + l.completion_tokens), 0) DESC;"

ACCT_DATA=$(run_sql "$ACCT_SQL")

# --- Query model ranking ---
MODEL_SQL="SELECT l.model_name, COUNT(*) AS call_count, ROUND((SUM(l.prompt_tokens + l.completion_tokens))::numeric / 1000000.0, 2) AS tokens_m, ROUND((SUM(l.quota))::numeric / ${QUOTA_PER_UNIT} * ${RATE}, 2) AS cost FROM logs l JOIN users u ON l.user_id = u.id WHERE u.role < 10 AND l.type = 2 ${TIME_COND} GROUP BY l.model_name ORDER BY SUM(l.prompt_tokens + l.completion_tokens) DESC;"

MODEL_DATA=$(run_sql "$MODEL_SQL")

# --- Build account rows JSON ---
ACCT_ROWS="[]"
if [ -n "$ACCT_DATA" ]; then
  ACCT_ROWS="["
  RANK=0
  FIRST=true
  while IFS='|' read -r username display_name call_count tokens_m cost; do
    RANK=$((RANK + 1))
    [ "$FIRST" = true ] && FIRST=false || ACCT_ROWS="${ACCT_ROWS},"
    ACCT_ROWS="${ACCT_ROWS}{\"rank\":\"${RANK}\",\"account\":\"${username}\",\"name\":\"${display_name}\",\"calls\":${call_count},\"tokens\":${tokens_m},\"cost\":${cost}}"
  done <<< "$ACCT_DATA"
  ACCT_ROWS="${ACCT_ROWS}]"
fi

# --- Build model rows JSON ---
MODEL_ROWS="[]"
if [ -n "$MODEL_DATA" ]; then
  MODEL_ROWS="["
  RANK=0
  FIRST=true
  while IFS='|' read -r model_name call_count tokens_m cost; do
    RANK=$((RANK + 1))
    [ "${model_name}" = "" ] && model_name="(unknown)"
    [ "$FIRST" = true ] && FIRST=false || MODEL_ROWS="${MODEL_ROWS},"
    MODEL_ROWS="${MODEL_ROWS}{\"rank\":\"${RANK}\",\"model\":\"${model_name}\",\"calls\":${call_count},\"tokens\":${tokens_m},\"cost\":${cost}}"
  done <<< "$MODEL_DATA"
  MODEL_ROWS="${MODEL_ROWS}]"
fi

# --- Build card JSON ---
CARD=$(cat <<CARDEOF
{"schema":"2.0","config":{"update_multi":true,"width_mode":"fill"},"header":{"title":{"tag":"plain_text","content":"📊 用量排名报告 — ${PERIOD_LABEL}"},"template":"indigo"},"body":{"direction":"vertical","elements":[{"tag":"markdown","content":"**账户用量排名**"},{"tag":"table","page_size":100,"header_style":{"background_style":"grey","bold":true},"columns":[{"name":"rank","display_name":"排名","data_type":"text"},{"name":"account","display_name":"账户名","data_type":"text"},{"name":"name","display_name":"姓名","data_type":"text"},{"name":"calls","display_name":"调用次数","data_type":"number","format":{"separator":true}},{"name":"tokens","display_name":"Token(M)","data_type":"number","format":{"precision":2}},{"name":"cost","display_name":"消耗(${CURRENCY})","data_type":"number","format":{"precision":2,"separator":true}}],"rows":${ACCT_ROWS}},{"tag":"hr"},{"tag":"markdown","content":"**模型用量排名**"},{"tag":"table","page_size":100,"header_style":{"background_style":"grey","bold":true},"columns":[{"name":"rank","display_name":"排名","data_type":"text"},{"name":"model","display_name":"模型名称","data_type":"text"},{"name":"calls","display_name":"调用次数","data_type":"number","format":{"separator":true}},{"name":"tokens","display_name":"Token(M)","data_type":"number","format":{"precision":2}},{"name":"cost","display_name":"消耗(${CURRENCY})","data_type":"number","format":{"precision":2,"separator":true}}],"rows":${MODEL_ROWS}}]}}
CARDEOF
)

# --- Send card to Feishu ---
RESULT=$(lark-cli im +messages-send --as bot --chat-id "$CHAT_ID" --msg-type interactive --content "$CARD" 2>&1) || {
  # Fallback: send as plain text
  FALLBACK="📊 用量排名报告 — ${PERIOD_LABEL}\n\n账户用量排名:\n${ACCT_DATA}\n\n模型用量排名:\n${MODEL_DATA}"
  lark-cli im +messages-send --as bot --chat-id "$CHAT_ID" --text "$(echo -e "$FALLBACK")" 2>&1
  exit 0
}

echo "$RESULT"
