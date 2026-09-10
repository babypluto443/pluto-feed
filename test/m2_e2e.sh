#!/usr/bin/env bash
# pluto_feed · M2 全链路验收脚本（社交闭环：关注体系与双流）
#
# 用法：
#   1. 先启动服务：go run ./cmd/server
#   2. 再跑本脚本：bash test/m2_e2e.sh
#
# 覆盖链路（对应实施计划任务 8）：
#   A/B 注册 → B 发帖 → A 关注 B（幂等+计数校验）→ A 关注列表含 B
#   → A 刷关注流看到 B 的帖 → A 访问 B 的 profile → A 取关（幂等+计数回退+关注流为空）
#   → 异常路径（自关 400 / 关注不存在 404 / 未登录关注流 401）

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
PASS=0

ok() { PASS=$((PASS + 1)); echo "  ✅ $1"; }
fail() { echo "  ❌ $1"; exit 1; }
assert_eq() {
  if [ "$2" = "$3" ]; then ok "$1"; else fail "$1（实际: $2, 期望: $3）"; fi
}
json_get() {
  echo "$1" | python3 -c "
import sys, json
cur = json.load(sys.stdin)
for key in '$2'.split('.'):
    cur = cur[int(key)] if isinstance(cur, list) else cur[key]
print(cur)
"
}

echo "▶ 前置检查：服务健康"
curl -s -o /dev/null "$BASE/healthz" || fail "服务未启动：请先 go run ./cmd/server"
ok "healthz 可达"

SUFX=$RANDOM
A="alice_m2_$SUFX"
B="bob_m2_$SUFX"

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

echo "▶ 2. B 上传图片并发帖"
PIXEL="/tmp/pixel_m2_${SUFX}.png"
echo "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==" \
  | base64 -d > "$PIXEL"
IMG_URL=$(curl -s -X POST "$BASE/api/v1/upload" -H "Authorization: Bearer $BT" \
  -F "file=@$PIXEL" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['url'])")
R=$(curl -s -X POST "$BASE/api/v1/posts" -H "Authorization: Bearer $BT" \
  -H "Content-Type: application/json" \
  -d "{\"image_urls\":[\"$IMG_URL\"],\"content\":\"M2 验收帖 $SUFX\"}")
assert_eq "B 发帖成功" "$(json_get "$R" "code")" "0"
POST_ID=$(json_get "$R" "data.id")
B_ID=$(json_get "$R" "data.author.id")
A_ID=$((B_ID - 1)) # 注册顺序 A 在 B 前（验证用，非强依赖）
echo "  - B id=$B_ID, A id=$A_ID, post id=$POST_ID"

echo "▶ 3. A 关注 B（幂等 + 计数校验）"
R=$(curl -s -X POST "$BASE/api/v1/users/$B_ID/follow" -H "Authorization: Bearer $AT")
assert_eq "关注成功" "$(json_get "$R" "code")" "0"
R=$(curl -s -X POST "$BASE/api/v1/users/$B_ID/follow" -H "Authorization: Bearer $AT")
assert_eq "重复关注幂等" "$(json_get "$R" "code")" "0"
R=$(curl -s "$BASE/api/v1/users/$B_ID/profile")
assert_eq "B 的 follower_count=1" "$(json_get "$R" "data.follower_count")" "1"
R=$(curl -s "$BASE/api/v1/users/$A_ID/profile")
assert_eq "A 的 following_count=1" "$(json_get "$R" "data.following_count")" "1"

echo "▶ 4. A 的 following 列表含 B（1 条，新→旧）"
R=$(curl -s "$BASE/api/v1/users/$A_ID/following")
COUNT=$(echo "$R" | python3 -c "import sys,json;print(len(json.load(sys.stdin)['data']['items']))")
assert_eq "列表条数=1" "$COUNT" "1"
NICK=$(echo "$R" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['items'][0]['nickname'])")
assert_eq "列表首条是 B" "$NICK" "$B"

echo "▶ 5. A 刷关注流（必须登录），看到 B 的帖子"
R=$(curl -s "$BASE/api/v1/feed/following" -H "Authorization: Bearer $AT")
assert_eq "关注流含 B 的帖" "$(json_get "$R" "data.items.0.id")" "$POST_ID"

echo "▶ 6. A 访问 B 的 profile（资料+计数+帖子列表）"
R=$(curl -s "$BASE/api/v1/users/$B_ID/profile")
assert_eq "profile 帖子列表含验收帖" "$(json_get "$R" "data.posts.items.0.id")" "$POST_ID"
assert_eq "profile post_count=1" "$(json_get "$R" "data.post_count")" "1"

echo "▶ 7. A 取关：计数回退、关注流为空"
curl -s -X DELETE "$BASE/api/v1/users/$B_ID/follow" -H "Authorization: Bearer $AT" > /dev/null
curl -s -X DELETE "$BASE/api/v1/users/$B_ID/follow" -H "Authorization: Bearer $AT" > /dev/null
R=$(curl -s "$BASE/api/v1/users/$B_ID/profile")
assert_eq "B 的 follower_count=0（幂等取关）" "$(json_get "$R" "data.follower_count")" "0"
R=$(curl -s "$BASE/api/v1/feed/following" -H "Authorization: Bearer $AT")
HAS_ITEMS=$(echo "$R" | python3 -c "import sys,json;print(len(json.load(sys.stdin)['data']['items']))")
assert_eq "关注流为空页" "$HAS_ITEMS" "0"

echo "▶ 8. 异常路径：自关 400 / 关注不存在 404 / 未登录关注流 401"
R=$(curl -s -X POST "$BASE/api/v1/users/$B_ID/follow" -H "Authorization: Bearer $BT")
assert_eq "B 关注自己被拒" "$(json_get "$R" "code")" "40030"
R=$(curl -s -X POST "$BASE/api/v1/users/99999/follow" -H "Authorization: Bearer $AT")
assert_eq "关注不存在用户 404" "$(json_get "$R" "code")" "40400"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/v1/feed/following")
assert_eq "未登录访问关注流 401" "$CODE" "401"

echo ""
echo "=========================================="
echo "🎉 M2 全链路验收 PASS：$PASS 项断言全部通过"
echo "=========================================="
