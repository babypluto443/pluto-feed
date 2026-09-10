<script setup lang="ts">
// 帖子卡片：封面 + 作者 + 交互行（点赞/评论/删除）。
// 点赞是乐观更新：UI 先行，失败回滚——体验优先，最终一致由后端保证。
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import client from '../api/client'
import { useAuthStore } from '../stores/auth'

const props = defineProps<{
  post: {
    id: number
    user_id: number
    author: { id: number; nickname: string; avatar_url: string }
    image_urls: string[]
    content: string
    like_count: number
    comment_count: number
    liked?: boolean
  }
}>()

const auth = useAuthStore()
const router = useRouter()

const liked = ref(props.post.liked ?? false)
const likeCount = ref(props.post.like_count)
const busy = ref(false)
const deleted = ref(false)

// 服务端返回的 liked 是唯一事实源：feed 重新拉取时同步（修复"切页丢点赞态"）
watch(
  () => [props.post.liked, props.post.like_count] as const,
  ([l, c]) => {
    liked.value = !!l
    likeCount.value = c
  },
)

const isMine = computed(() => auth.uid === props.post.user_id)
const cover = computed(() => props.post.image_urls?.[0] || '')

async function toggleLike() {
  if (!auth.isLogin) return router.push('/login')
  if (busy.value) return
  busy.value = true
  // 乐观更新
  liked.value = !liked.value
  likeCount.value += liked.value ? 1 : -1
  try {
    if (liked.value) await client.post(`/posts/${props.post.id}/like`)
    else await client.delete(`/posts/${props.post.id}/like`)
  } catch {
    liked.value = !liked.value // 失败回滚
    likeCount.value += liked.value ? 1 : -1
  } finally {
    busy.value = false
  }
}

async function removePost() {
  if (!confirm('确定删除这篇帖子吗？')) return
  await client.delete(`/posts/${props.post.id}`)
  deleted.value = true
}
</script>

<template>
  <article v-if="!deleted" class="m-card m-card--hover post">
    <RouterLink :to="`/posts/${post.id}`" style="text-decoration: none">
      <img v-if="cover" class="post-cover" :src="cover" alt="帖子封面" loading="lazy" />
    </RouterLink>
    <div style="padding-top: var(--sp-2)">
      <RouterLink :to="`/posts/${post.id}`" class="post__content">
        {{ post.content }}
      </RouterLink>

      <div class="post__meta">
        <RouterLink :to="`/users/${post.author.id}`" class="post__author">
          <span class="avatar" style="width: 28px; height: 28px; font-size: 12px">
            {{ post.author.nickname.slice(0, 1) }}
          </span>
          <span>{{ post.author.nickname }}</span>
        </RouterLink>
        <button
          class="action"
          :class="{ 'action--liked': liked }"
          :title="auth.isLogin ? '' : '登录后可点赞'"
          @click="toggleLike"
        >
          <span aria-hidden="true">{{ liked ? '❤️' : '🤍' }}</span>
          {{ likeCount }}
        </button>
        <RouterLink :to="`/posts/${post.id}`" class="action" style="text-decoration: none">
          💬 {{ post.comment_count }}
        </RouterLink>
        <button v-if="isMine" class="action action--danger-text" @click="removePost">删除</button>
      </div>
    </div>
  </article>
</template>

<style scoped>
.post__content {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  color: var(--on-surface);
  text-decoration: none;
  font-size: var(--fs-body);
  font-weight: 500;
  line-height: 1.6;
  min-height: 2.9em;
}
.post__content:hover { color: var(--primary); }
.post__meta {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  border-top: 1px solid var(--outline);
  margin-top: var(--sp-2);
  padding-top: var(--sp-2);
}
.post__author {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--on-surface-variant);
  text-decoration: none;
  font-size: var(--fs-caption);
  margin-right: auto;
  min-width: 0;
}
.post__author:hover { color: var(--primary); }
.action--danger-text:hover { color: var(--danger); background: #fce8e6; }
</style>
