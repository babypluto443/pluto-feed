<script setup lang="ts">
// 个人主页：资料 + 关注/粉丝计数 + TA 的帖子（游标分页"加载更多"）
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import client from '../api/client'
import { useAuthStore } from '../stores/auth'
import PostCard from '../components/PostCard.vue'

const route = useRoute()
const auth = useAuthStore()
const uid = Number(route.params.id)

const profile = ref<any>(null)
const items = ref<any[]>([])
const cursor = ref('')
const hasMore = ref(false)
const err = ref('')
const busy = ref(false)
const isFollowing = ref(false)

const isMe = computed(() => auth.uid === uid)

async function load() {
  try {
    profile.value = await client.get(`/users/${uid}/profile`, {
      params: { limit: 10, cursor: cursor.value || undefined },
    })
    items.value.push(...(profile.value?.posts?.items || []))
    cursor.value = profile.value?.posts?.next_cursor || ''
    hasMore.value = !!profile.value?.posts?.has_more
  } catch (e) {
    err.value = e instanceof Error ? e.message : '加载失败'
  }
}
async function more() {
  cursor.value = profile.value?.posts?.next_cursor || ''
  await load()
}

// 关注态探测：M2 未提供 is_following 字段，用"我的关注列表"兜底判断
async function probeFollowing() {
  if (!auth.isLogin || isMe.value) return
  try {
    const list = await client.get(`/users/${auth.uid}/following`, { params: { limit: 50 } })
    isFollowing.value = (list?.items || []).some((u: any) => u.id === uid)
  } catch { /* 忽略：按钮态默认未关注 */ }
}
async function toggleFollow() {
  if (!auth.isLogin) return
  busy.value = true
  try {
    if (isFollowing.value) await client.delete(`/users/${uid}/follow`)
    else await client.post(`/users/${uid}/follow`)
    isFollowing.value = !isFollowing.value
    // 计数是异步的：稍等重拉
    setTimeout(() => { items.value = []; cursor.value = ''; load() }, 1200)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  load()
  probeFollowing()
})
</script>

<template>
  <div class="page" style="max-width: 720px">
    <div v-if="err" class="m-alert m-alert--error">{{ err }}</div>

    <div v-if="profile" class="m-card reveal" style="--i: 0">
      <div class="p-head">
        <span class="avatar" style="width: 64px; height: 64px; font-size: 26px">
          {{ profile.nickname.slice(0, 1) }}
        </span>
        <div style="flex: 1">
          <h1 class="p-name">{{ profile.nickname }}</h1>
          <p class="p-bio">{{ profile.bio || '这个人很懒，什么都没写' }}</p>
        </div>
        <button
          v-if="!isMe"
          class="btn"
          :class="isFollowing ? 'btn--tonal' : 'btn--filled'"
          :disabled="busy"
          @click="toggleFollow"
        >
          {{ isFollowing ? '已关注' : '+ 关注' }}
        </button>
      </div>

      <div class="p-stats">
        <span class="m-chip">关注 {{ profile.following_count }}</span>
        <span class="m-chip">粉丝 {{ profile.follower_count }}</span>
        <span class="m-chip m-chip--primary">帖子 {{ profile.post_count }}</span>
      </div>
    </div>

    <h2 class="p-subtitle">TA 的帖子</h2>
    <div class="grid-posts">
      <div v-for="p in items" :key="p.id">
        <PostCard :post="p" />
      </div>
    </div>
    <div v-if="profile && !items.length" class="empty">还没有发过帖子</div>
    <div style="text-align: center; padding: var(--sp-3)">
      <button v-if="hasMore" class="btn btn--tonal" @click="more">加载更多</button>
    </div>
  </div>
</template>

<style scoped>
.p-head { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-3); }
.p-name {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  letter-spacing: var(--tracking-display);
  margin: 0 0 4px;
  color: var(--on-surface);
}
.p-bio { margin: 0; color: var(--on-surface-variant); font-size: var(--fs-body); }
.p-stats { display: flex; gap: var(--sp-2); flex-wrap: wrap; }
.p-subtitle {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  margin: var(--sp-5) 0 var(--sp-3);
  color: var(--on-surface);
}
</style>
