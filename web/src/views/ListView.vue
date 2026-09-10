<script setup lang="ts">
// 热榜 / 关注流：同一个列表形态，数据源不同（/feed/hot vs /feed/following）
import { onMounted, ref } from 'vue'
import client from '../api/client'
import PostCard from '../components/PostCard.vue'

const props = defineProps<{ source: 'hot' | 'following' }>()

const items = ref<any[]>([])
const error = ref('')
const loading = ref(true)

onMounted(async () => {
  try {
    if (props.source === 'hot') {
      const data = await client.get('/feed/hot', { params: { limit: 20 } })
      items.value = data?.items || []
    } else {
      const data = await client.get('/feed/following', { params: { limit: 20 } })
      items.value = data?.items || []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page">
    <h1 class="page-title reveal" style="--i: 0">
      {{ source === 'hot' ? '🔥 热榜' : '👥 关注流' }}
      <span v-if="source === 'hot'" class="m-chip" style="margin-left: 12px">最近 60 分钟热度</span>
    </h1>

    <div v-if="error" class="m-alert m-alert--error">{{ error }}</div>
    <div v-if="loading" class="empty">加载中…</div>
    <div v-else-if="!items.length" class="empty">
      {{ source === 'hot' ? '最近一小时还没有热度数据，去给喜欢的帖子点个赞吧' : '关注一些人，他们的帖子会出现在这里' }}
    </div>

    <div class="grid-posts">
      <div v-for="(p, i) in items" :key="p.id" class="reveal" :style="{ '--i': Math.min(i, 5) }">
        <PostCard :post="p" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-title {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  font-weight: 700;
  letter-spacing: var(--tracking-display);
  margin: 0 0 var(--sp-3);
  color: var(--on-surface);
  display: flex;
  align-items: center;
}
</style>
