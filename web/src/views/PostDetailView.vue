<script setup lang="ts">
// 帖子详情：正文大图 + 点赞 + 评论（列表/发布/删除，评论计数异步轮询）
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '../api/client'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const postId = Number(route.params.id)
const post = ref<any>(null)
const comments = ref<any[]>([])
const cursor = ref('')
const hasMore = ref(false)
const notFound = ref(false)

const newComment = ref('')
const commentErr = ref('')
const posting = ref(false)
const liked = ref(false)
const likeCount = ref(0)

const isMine = computed(() => post.value && auth.uid === post.value.user_id)

async function load() {
  try {
    post.value = await client.get(`/posts/${postId}`)
    liked.value = !!post.value.liked // 服务端已是唯一事实源
    likeCount.value = post.value.like_count
    await loadComments()
  } catch {
    notFound.value = true
  }
}
async function loadComments() {
  const data = await client.get(`/posts/${postId}/comments`, {
    params: { limit: 20, cursor: cursor.value || undefined },
  })
  comments.value.push(...(data?.items || []))
  cursor.value = data.next_cursor || ''
  hasMore.value = !!data.has_more
}

async function toggleLike() {
  if (!auth.isLogin) return router.push('/login')
  liked.value = !liked.value
  likeCount.value += liked.value ? 1 : -1
  try {
    if (liked.value) await client.post(`/posts/${postId}/like`)
    else await client.delete(`/posts/${postId}/like`)
  } catch {
    liked.value = !liked.value
    likeCount.value += liked.value ? 1 : -1
  }
}

async function addComment() {
  commentErr.value = ''
  const text = newComment.value.trim()
  if (!text) return
  posting.value = true
  try {
    const view = await client.post(`/posts/${postId}/comments`, { content: text })
    comments.value.unshift(view) // 乐观插入列表顶部（列表是时间倒序）
    newComment.value = ''
    // 计数是异步的：等一小会重拉详情刷新计数
    setTimeout(load, 1200)
  } catch (e) {
    commentErr.value = e instanceof Error ? e.message : '评论失败'
  } finally {
    posting.value = false
  }
}

async function removeComment(id: number) {
  if (!confirm('删除这条评论？')) return
  await client.delete(`/comments/${id}`)
  comments.value = comments.value.filter((c) => c.id !== id)
  setTimeout(load, 1200)
}

async function removePost() {
  if (!confirm('确定删除这篇帖子吗？')) return
  await client.delete(`/posts/${postId}`)
  router.push('/')
}

onMounted(load)
</script>

<template>
  <div class="page" style="max-width: 720px">
    <div v-if="notFound" class="empty">帖子不存在或已被删除</div>

    <template v-else-if="post">
      <article class="m-card reveal" style="--i: 0">
        <img v-if="post.image_urls?.[0]" class="post-cover" :src="post.image_urls[0]" alt="帖子图片" />
        <div style="padding-top: var(--sp-3)">
          <div class="d-head">
            <RouterLink :to="`/users/${post.author.id}`" class="post__author" style="text-decoration: none">
              <span class="avatar">{{ post.author.nickname.slice(0, 1) }}</span>
              <span>
                <b style="color: var(--on-surface)">{{ post.author.nickname }}</b><br />
                <span style="font-size: var(--fs-caption)">发布于 {{ new Date(post.created_at).toLocaleString('zh-CN') }}</span>
              </span>
            </RouterLink>
            <button v-if="isMine" class="btn btn--danger" style="min-height: 36px" @click="removePost">删除帖子</button>
          </div>

          <p class="d-content">{{ post.content }}</p>
          <div v-if="post.tags?.length" style="margin-bottom: var(--sp-3)">
            <span v-for="t in post.tags" :key="t" class="m-chip m-chip--primary" style="margin-right: 8px"># {{ t }}</span>
          </div>

          <div class="post-actions" style="border-top: none; padding-top: 0">
            <button class="action" :class="{ 'action--liked': liked }" @click="toggleLike">
              <span aria-hidden="true">{{ liked ? '❤️' : '🤍' }}</span>
              {{ likeCount }} 点赞
            </button>
            <span class="action" style="cursor: default">💬 {{ post.comment_count }} 评论</span>
          </div>
        </div>
      </article>

      <!-- 评论 -->
      <section class="m-card reveal" style="--i: 1; margin-top: var(--sp-3)">
        <h2 class="c-title">评论（{{ post.comment_count }}）</h2>

        <div v-if="auth.isLogin" style="margin-bottom: var(--sp-3)">
          <textarea v-model="newComment" class="input" rows="2" placeholder="友善评论…（≤500 字）"></textarea>
          <div v-if="commentErr" class="m-alert m-alert--error" style="margin-top: 8px">{{ commentErr }}</div>
          <button class="btn btn--filled" style="margin-top: 8px" :disabled="posting || !newComment.trim()" @click="addComment">
            {{ posting ? '发送中…' : '发表评论' }}
          </button>
        </div>
        <div v-else class="m-alert m-alert--info">
          <RouterLink to="/login" style="color: inherit; font-weight: 700">登录</RouterLink> 后参与评论
        </div>

        <div v-if="!comments.length && !hasMore" class="empty">还没有评论，坐个沙发？</div>
        <ul class="c-list">
          <li v-for="cm in comments" :key="cm.id" class="c-item">
            <span class="avatar" style="width: 32px; height: 32px; font-size: 13px">
              {{ cm.author.nickname.slice(0, 1) }}
            </span>
            <div style="flex: 1; min-width: 0">
              <b style="font-size: var(--fs-caption); color: var(--on-surface)">{{ cm.author.nickname }}</b>
              <p style="margin: 2px 0 0; font-size: var(--fs-body); color: var(--on-surface); line-height: 1.6">
                {{ cm.content }}
              </p>
            </div>
            <button
              v-if="auth.uid === cm.author.id"
              class="action"
              style="min-height: 28px; font-size: var(--fs-caption)"
              @click="removeComment(cm.id)"
            >删除</button>
          </li>
        </ul>
        <button v-if="hasMore" class="btn btn--text" @click="loadComments">加载更多评论</button>
      </section>
    </template>
  </div>
</template>

<style scoped>
.d-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-2); }
.post__author { display: inline-flex; align-items: center; gap: 10px; }
.d-content {
  font-size: 16px;
  line-height: var(--lh-body);
  color: var(--on-surface);
  margin: var(--sp-3) 0;
  white-space: pre-wrap;
}
.c-title {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  margin: 0 0 var(--sp-3);
  color: var(--on-surface);
}
.c-list { list-style: none; margin: 0; padding: 0; }
.c-item {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: var(--sp-2) 0;
  border-bottom: 1px solid var(--outline);
}
.c-item:last-child { border-bottom: none; }
</style>
