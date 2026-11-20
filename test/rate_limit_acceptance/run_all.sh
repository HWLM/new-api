#!/usr/bin/env bash
# 一条命令跑完 §21 验收：空库实例 setup -> 建 token -> 开自用模式 -> 跑 RL1–RL7
#
#   ./run_all.sh [BASE_URL] [SQLITE_PATH] [ENV_OUT]
#
# 默认 3102 / new-api-verify/rl3102/data/rl.db，可用参数覆盖。
set -uo pipefail
cd "$(dirname "$0")"

BASE="${1:-http://127.0.0.1:3102}"
DB="${2:-C:/Users/xiang/.dsh/bot/workspace/new-api-verify/rl3102/data/rl.db}"
OUT="${3:-C:/Users/xiang/.dsh/bot/workspace/new-api-verify/rl3102/env_rl.json}"
HERE="$(pwd -W 2>/dev/null || pwd)"   # 给原生 python 用的 Windows 路径

LOG="run-$(date +%m%d-%H%M).log"
exec > >(tee "$LOG") 2>&1     # 整段输出都进日志，含版本头

echo "== 1. 实例初始化（已初始化则只刷新 token）=="
BASE="$BASE" DISTILL_SQLITE="$DB" DISTILL_ENV_OUT="$OUT" \
  python "$HERE/../distill_acceptance/setup3101.py" || { echo "!! setup 失败"; exit 1; }

echo "== 2. 组装环境变量 =="
eval "$(python - "$OUT" <<'PY'
import json, sys
e = json.load(open(sys.argv[1]))
print(''.join([
    "export RL_BASE_URL=%s\n" % e["base_url"],
    "export RL_ADMIN_TOKEN=%s\n" % e["admin_token"],
    "export RL_RELAY_KEY=%s\n" % e["relay_key"],
    "export RL_USER_ID=%s\n" % e["user_id"],
    "export RL_MODEL=rl-model\n",
    "export RL_UPSTREAM_PORT=18110\n",
    "export RL_SLOW_MS=2500\n",
    "export RL_REDIS_ADDR=127.0.0.1:6380\n",
    "export RL_REDIS_DB=0\n",
]))
PY
)"

echo "== 3. 版本 =="
curl -s -m 5 -D - -o /dev/null "$BASE/api/status" | grep -i "x-new-api-version" | tr -d '\r'

echo "== 4. 跑全部用例 =="
python rate_limit_acceptance.py "${@:4}"
rc=$?
echo "日志: $HERE/$LOG"
exit $rc
