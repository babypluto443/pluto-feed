#!/usr/bin/env bash
# pluto_feed · M3 压测脚本（T3.6）：对比"缓存命中路径"与"DB 路径"的吞吐
#
# 用法：
#   1. 启动服务 + Redis：go run ./cmd/server（Redis 用 docker compose up -d redis）
#   2. bash test/m3_bench.sh
#
# 原理：
#   - /api/v1/posts/{id}  → Cache Aside 命中后纯内存/Redis 读（预期高 QPS）
#   - /api/v1/feed        → 每次回源 DB 游标查询（对照组）
#   用 ab（macOS 自带）压 2000 请求 / 50 并发，取 QPS 和 P50/P99 对比。

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
N="${N:-2000}"
C="${C:-50}"

echo "▶ 前置检查"
curl -s -o /dev/null "$BASE/healthz" || { echo "服务未启动"; exit 1; }
which ab > /dev/null || { echo "ab 不存在（macOS 自带，检查 PATH）"; exit 1; }

# 选一个已存在的帖子作为热点 key
PID=$(curl -s "$BASE/api/v1/feed?limit=1" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['items'][0]['id'])")
echo "  热点帖子 id = $PID"

# 预热缓存（连续访问 10 次，确保 post:{id} 已在 Redis）
for i in $(seq 1 10); do curl -s -o /dev/null "$BASE/api/v1/posts/$PID"; done
echo "  缓存已预热"

echo ""
echo "================ A. 帖子详情（缓存命中路径）================"
ab -q -n "$N" -c "$C" "$BASE/api/v1/posts/$PID" | grep -E "Requests per second|Time per request|99%" || true

echo ""
echo "================ B. Feed 列表（DB 游标查询路径，对照组）================"
ab -q -n "$N" -c "$C" "$BASE/api/v1/feed?limit=20" | grep -E "Requests per second|Time per request|99%" || true

echo ""
echo "结论要点（写进开发记录）："
echo "  1. A 的 QPS 应显著高于 B（缓存命中不走 MySQL）"
echo "  2. A 的 P99 应显著低于 B"
echo "  3. 把两组数字记录到 07-M3-开发记录.md §六"
