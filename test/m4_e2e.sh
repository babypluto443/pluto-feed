#!/usr/bin/env bash
# pluto_feed · M4 全链路验收脚本（异步化：API/Worker 双进程 + RabbitMQ）
#
# 前置：server（8080）与 worker 两个进程都在运行。
# 用法：bash test/m4_e2e.sh
#
# 覆盖链路：
#   点赞 → 轮询计数到位（outbox→MQ→Worker 三跳的最终一致）
#   重复点赞 → 计数不变（源头幂等 + 消费幂等双重防线）
#   评论/删评论 → 轮询计数增减
#   查库：outbox 全部 processed / processed_events 有行（消费发生）
#   异常路径：未登录点赞 401

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
PASS=0
ok() { PASS=$((PASS + 1)); echo "  ✅ $1"; }
fail() { echo "  ❌ $1"; exit 1; }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1"; else fail "$1（实际: $2, 期望: $3）"; fi; }
json_get() {
  echo "$1" | python3 -c "
import sys, json
cur = json.load(sys.stdin)
for key in '$2'.split('.'):
    cur = cur[int(key)] if isinstance(cur, list) else cur[key]
print(cur)
"
}
# poll_eq：轮询断言（最终一致性的正确验收姿势）
poll_eq() {
  local V=""
  for i in $(seq 1 30); do
    R=$(curl -s "$2")
    V=$(json_get "$R" "$3")
    if [ "$V" = "$4" ]; then ok "$1 (poll #$i)"; return 0; fi
    sleep 0.5
  done
  fail "$1 [poll 15s timeout, last="$V"]"
}

echo "▶ 前置检查：服务健康"
curl -s -o /dev/null "$BASE/healthz" || fail "server 未启动：go run ./cmd/server"
ok "healthz 可达"

SUFX=$RANDOM
A="alice_m4_$SUFX"
B="bob_m4_$SUFX"

echo "▶ 1. 注册 A/B 并登录"
for N in "$A" "$B"; do
  R=$(curl -s -X POST "$BASE/api/v1/auth/register" -H "Content-Type: application/json" \
    -d "{\"nickname\":\"$N\",\"password\":\"secret123\"}")
  assert_eq "$N 注册" "$(json_get "$R" "code")" "0"
done
login() {
  curl -s -X POST "$BASE/api/v1/auth/login" -H "Content-Type: application/json" \
    -d "{\"nickname\":\"$1\",\"password\":\"secret123\"}" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['access_token'])"
}
AT=$(login "$A"); BT=$(login "$B")
[ -n "$AT" ] && [ -n "$BT" ] && ok "双 token 获取成功" || fail "登录失败"

echo "▶ 2. B 发帖"
PIXEL="/tmp/pixel_m4_${SUFX}.png"
echo "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==" \
  | base64 -d > "$PIXEL"
IMG_URL=$(curl -s -X POST "$BASE/api/v1/upload" -H "Authorization: Bearer $BT" \
  -F "file=@$PIXEL" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['url'])")
R=$(curl -s -X POST "$BASE/api/v1/posts" -H "Authorization: Bearer $BT" \
  -H "Content-Type: application/json" \
  -d "{\"image_urls\":[\"$IMG_URL\"],\"content\":\"M4 验收帖 $SUFX\"}")
assert_eq "B 发帖成功" "$(json_get "$R" "code")" "0"
POST_ID=$(json_get "$R" "data.id")
echo "  - post id=$POST_ID"

echo "▶ 3. A 点赞 → 轮询计数到位（outbox→MQ→Worker 三跳）"
R=$(curl -s -X POST "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $AT")
assert_eq "点赞响应成功（API 不等计数）" "$(json_get "$R" "code")" "0"
poll_eq "like_count=1（Worker 异步消费）" "$BASE/api/v1/posts/$POST_ID" "data.like_count" "1"

echo "▶ 4. 重复点赞：源头幂等（无新事件）+ 计数稳定"
curl -s -X POST "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $AT" > /dev/null
sleep 2
poll_eq "重复点赞后仍为 1" "$BASE/api/v1/posts/$POST_ID" "data.like_count" "1"

echo "▶ 5. A 评论 → 轮询 comment_count=1"
curl -s -X POST "$BASE/api/v1/posts/$POST_ID/comments" -H "Authorization: Bearer $AT" \
  -H "Content-Type: application/json" -d '{"content":"M4 验证评论"}' > /dev/null
poll_eq "comment_count=1（异步消费）" "$BASE/api/v1/posts/$POST_ID" "data.comment_count" "1"

echo "▶ 6. A 取消点赞 + 删评论 → 轮询回落到 0"
curl -s -X DELETE "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $AT" > /dev/null
COMMENT_ID=$(curl -s "$BASE/api/v1/posts/$POST_ID/comments" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['items'][0]['id'])")
curl -s -X DELETE "$BASE/api/v1/comments/$COMMENT_ID" -H "Authorization: Bearer $AT" > /dev/null
poll_eq "like_count 回落 0" "$BASE/api/v1/posts/$POST_ID" "data.like_count" "0"
poll_eq "comment_count 回落 0" "$BASE/api/v1/posts/$POST_ID" "data.comment_count" "0"

echo "▶ 7. 查库证据：outbox 全部投递、processed_events 有消费记录"
PENDING=$(docker exec pluto_feed-mysql mysql -uroot -p123456 pluto_feed -N -e \
  "SELECT COUNT(*) FROM outbox WHERE processed_at IS NULL" 2>/dev/null | tr -d ' ')
assert_eq "outbox 无滞留事件" "$PENDING" "0"
CONSUMED=$(docker exec pluto_feed-mysql mysql -uroot -p123456 pluto_feed -N -e \
  "SELECT COUNT(*) FROM processed_events" 2>/dev/null | tr -d ' ')
if [ "$CONSUMED" -ge 4 ]; then ok "processed_events = $CONSUMED 行（消费发生）"; else fail "processed_events = $CONSUMED, 期望 >= 4"; fi

echo "▶ 8. 异常路径：未登录点赞 401"
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/posts/$POST_ID/like")
assert_eq "未登录点赞被拒" "$CODE" "401"

echo ""
echo "=========================================="
echo "🎉 M4 全链路验收 PASS：$PASS 项断言全部通过"
echo "=========================================="
