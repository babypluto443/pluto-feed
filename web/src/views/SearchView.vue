<script setup lang="ts">
// 搜索页：顶栏搜索框跳转 /search?q=xxx，结果复用 PostCard
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '../api/client'
import PostCard from '../components/PostCard.vue'

const route = useRoute()
const router = useRouter()
const q = ref((route.query.q as string) || '')
const items = ref<any[]>([])
const loading = ref(false)
const searched = ref(false)
const error = ref('')

async function doSearch() {
  const kw = q.value.trim()
  if (!kw) return
  router.replace({ path: '/search', query: { q: kw } })
  loading.value = true
  error.value = ''
  try {
    items.value = await client.get('/search', { params: { q: kw } })
    searched.value = true
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (q.value) doSearch()
})
</script>

<template>
  <div class="page">
    <form class="s-bar reveal" style="--i: 0" @submit.prevent="doSearch">
      <input v-model="q" class="input" placeholder="搜索帖子内容…" />
      <button class="btn btn--filled" type="submit">搜索</button>
    </form>

    <div v-if="error" class="m-alert m-alert--error">{{ error }}</div>
    <div v-if="loading" class="empty">搜索中…</div>
    <div v-else-if="searched && !items.length" class="empty">没有找到与「{{ q }}」相关的帖子</div>

    <div class="grid-posts">
      <div v-for="(p, i) in items" :key="p.id" class="reveal" :style="{ '--i': Math.min(i, 5) }">
        <PostCard :post="p" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.s-bar { display: flex; gap: var(--sp-2); margin-bottom: var(--sp-4); }
.s-bar .input { flex: 1; }
</style>
