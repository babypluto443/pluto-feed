<script setup lang="ts">
// Feed 流视图：游标分页 + IntersectionObserver 无限滚动（对齐后端 cursor 设计）
import { onMounted, onUnmounted, ref } from 'vue'
import client from '../api/client'
import PostCard from '../components/PostCard.vue'

const items = ref<any[]>([])
const cursor = ref('')
const hasMore = ref(true)
const loading = ref(false)
const error = ref('')
const sentinel = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | undefined

async function loadMore() {
  if (loading.value || !hasMore.value) return
  loading.value = true
  error.value = ''
  try {
    const data = await client.get('/feed', {
      params: { limit: 10, cursor: cursor.value || undefined },
    })
    items.value.push(...(data?.items || []))
    cursor.value = data.next_cursor || ''
    hasMore.value = !!data.has_more
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    hasMore.value = false
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadMore()
  // 触底哨兵：进入视口就加载下一页
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting) loadMore()
  })
  if (sentinel.value) observer.observe(sentinel.value)
})
onUnmounted(() => observer?.disconnect())
</script>

<template>
  <div class="page">
    <h1 class="page-title reveal" style="--i: 0">推荐</h1>

    <div v-if="error && !items.length" class="m-alert m-alert--error">{{ error }}</div>
    <div v-if="!loading && !error && !items.length" class="empty">
      还没有帖子 —— 去发布第一篇吧！
    </div>

    <div class="grid-posts">
      <div v-for="(p, i) in items" :key="p.id" class="reveal" :style="{ '--i': Math.min(i, 5) }">
        <PostCard :post="p" />
      </div>
    </div>

    <div ref="sentinel" class="sentinel">
      {{ loading ? '加载中…' : hasMore ? '下滑加载更多' : '—— 到底啦 ——' }}
    </div>
  </div>
</template>

<style scoped>
.page-title {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  font-weight: 700;
  letter-spacing: var(--tracking-display);
  color: var(--on-surface);
  margin: 0 0 var(--sp-3);
}
</style>
