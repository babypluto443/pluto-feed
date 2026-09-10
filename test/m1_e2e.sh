#!/usr/bin/env bash
# pluto_feed · M1 全链路验收脚本（任务 19）
#
# 用法：
#   1. 先启动服务：go run ./cmd/server
#   2. 再跑本脚本：bash test/m1_e2e.sh
#
# 覆盖链路（对应实施计划任务 19）：
#   注册 A/B → A 上传图片发帖 → B 刷 Feed 看到 A 的帖 → B 点赞（幂等校验）
#   → B 评论 → 计数校验 → B 取消点赞 → 计数回退 → 越权删帖 403
#   → A 删帖 → 详情 404 → Feed 消失 → 异常路径（伪造游标/未登录发帖）
#
# 任何一步失败立即退出（set -e + 显式断言），全绿则输出 PASS。

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
PASS=0

# ---------- 断言工具 ----------

ok() { PASS=$((PASS + 1)); echo "  ✅ $1"; }

fail() { echo "  ❌ $1"; exit 1; }

# assert_eq "描述" "实际值" "期望值"
assert_eq() {
  if [ "$2" = "$3" ]; then ok "$1"; else
    fail "$1（实际: $2, 期望: $3）"
  fi
}

# json 提取：json_get "$json" "data.like_count"
# poll_eq "描述" "URL" "json路径" "期望值"：每 0.5s 轮询，15s 超时（M4 计数异步化后的正确验收姿势）
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

json_get() {
  echo "$1" | python3 -c "
import sys, json
d = json.load(sys.stdin)
cur = d
for key in '$2'.split('.'):
    if isinstance(cur, list):
        cur = cur[int(key)]
    else:
        cur = cur[key]
print(cur)
"
}

# ---------- 前置检查 ----------

echo "▶ 前置检查：服务健康"
if ! curl -s -o /dev/null "$BASE/healthz"; then
  fail "服务未启动：请先 go run ./cmd/server"
fi
ok "healthz 可达"

# 随机昵称保证脚本可重复执行
SUFX=$RANDOM
A="alice_e2e_$SUFX"
B="bob_e2e_$SUFX"

echo "▶ 1. 注册两个用户 A=$A / B=$B"
R=$(curl -s -X POST "$BASE/api/v1/auth/register" -H "Content-Type: application/json" \
  -d "{\"nickname\":\"$A\",\"password\":\"secret123\"}")
assert_eq "A 注册成功" "$(json_get "$R" "code")" "0"
R=$(curl -s -X POST "$BASE/api/v1/auth/register" -H "Content-Type: application/json" \
  -d "{\"nickname\":\"$B\",\"password\":\"secret123\"}")
assert_eq "B 注册成功" "$(json_get "$R" "code")" "0"

echo "▶ 2. 双方登录拿 token"
login() {
  curl -s -X POST "$BASE/api/v1/auth/login" -H "Content-Type: application/json" \
    -d "{\"nickname\":\"$1\",\"password\":\"secret123\"}" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['access_token'])"
}
AT=$(login "$A")
BT=$(login "$B")
[ -n "$AT" ] && [ -n "$BT" ] && ok "双 token 获取成功" || fail "登录失败"

echo "▶ 3. A 上传图片"
# 1x1 真 PNG（魔数嗅探要过检）
PIXEL="/tmp/pixel_m1_${SUFX}.png"
echo "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==" \
  | base64 -d > "$PIXEL"
IMG_URL=$(curl -s -X POST "$BASE/api/v1/upload" -H "Authorization: Bearer $AT" \
  -F "file=@$PIXEL" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['url'])")
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE$IMG_URL")
assert_eq "上传成功且图片可访问" "$CODE" "200"

echo "▶ 4. A 发帖（带图）"
R=$(curl -s -X POST "$BASE/api/v1/posts" -H "Authorization: Bearer $AT" \
  -H "Content-Type: application/json" \
  -d "{\"image_urls\":[\"$IMG_URL\"],\"content\":\"e2e 验收帖 $SUFX\",\"tags\":[\"验收\"]}")
assert_eq "发帖成功" "$(json_get "$R" "code")" "0"
POST_ID=$(json_get "$R" "data.id")
[ -n "$POST_ID" ] && ok "帖子 id = $POST_ID" || fail "未取到帖子 id"

echo "▶ 5. B 刷 Feed 看到 A 的帖子（新→旧，第 1 条就是它）"
R=$(curl -s "$BASE/api/v1/feed?limit=1")
assert_eq "Feed 最新帖即 A 的帖" "$(json_get "$R" "data.items.0.id")" "$POST_ID"

echo "▶ 6. B 点赞（连点两次，幂等）"
curl -s -X POST "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $BT" > /dev/null
curl -s -X POST "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $BT" > /dev/null
poll_eq "连点两次 like_count=1（幂等，Worker 异步消费）" "$BASE/api/v1/posts/$POST_ID" "data.like_count" "1"

echo "▶ 7. B 评论"
R=$(curl -s -X POST "$BASE/api/v1/posts/$POST_ID/comments" -H "Authorization: Bearer $BT" \
  -H "Content-Type: application/json" -d '{"content":"e2e 评论"}')
assert_eq "评论成功" "$(json_get "$R" "code")" "0"
poll_eq "comment_count=1（异步消费）" "$BASE/api/v1/posts/$POST_ID" "data.comment_count" "1"

echo "▶ 8. B 取消点赞，计数回退"
curl -s -X DELETE "$BASE/api/v1/posts/$POST_ID/like" -H "Authorization: Bearer $BT" > /dev/null
poll_eq "取消后 like_count=0（异步消费）" "$BASE/api/v1/posts/$POST_ID" "data.like_count" "0"

echo "▶ 9. 越权删帖：B 删 A 的帖 → 403"
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/api/v1/posts/$POST_ID" \
  -H "Authorization: Bearer $BT")
assert_eq "B 删 A 的帖被拒" "$CODE" "403"

echo "▶ 10. A 删帖（软删除），详情变 404、Feed 消失"
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/api/v1/posts/$POST_ID" \
  -H "Authorization: Bearer $AT")
assert_eq "A 删自己的帖成功" "$CODE" "200"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/v1/posts/$POST_ID")
assert_eq "已删帖详情 404" "$CODE" "404"
FOUND=$(curl -s "$BASE/api/v1/feed?limit=50" | python3 -c "
import sys, json
d = json.load(sys.stdin)['data']
print(1 if any(i['id'] == $POST_ID for i in d['items']) else 0)
")
assert_eq "Feed 中不再出现" "$FOUND" "0"

echo "▶ 11. 异常路径：伪造游标 400 / 未登录发帖 401"
R=$(curl -s "$BASE/api/v1/feed?cursor=garbage!!!")
assert_eq "伪造游标被拒" "$(json_get "$R" "code")" "40005"
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/posts" \
  -H "Content-Type: application/json" -d '{"image_urls":["x"],"content":"y"}')
assert_eq "未登录发帖被拒" "$CODE" "401"

echo ""
echo "=========================================="
echo "🎉 M1 全链路验收 PASS：$PASS 项断言全部通过"
echo "=========================================="
