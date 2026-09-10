<script setup lang="ts">
// 发布页（O5 多图版）：多选图片逐个上传（1~9 张，与后端上限一致）→ 预览可删 → 发帖
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import client, { getToken } from '../api/client'

const router = useRouter()
const MAX_IMAGES = 9 // 与后端校验一致

const files = ref<{ url: string; preview: string }[]>([]) // 已上传的图片
const content = ref('')
const tags = ref('')
const error = ref('')
const uploading = ref(0) // 正在上传的数量
const posting = ref(false)

const canSubmit = computed(
  () => files.value.length >= 1 && content.value.trim().length > 0 && uploading.value === 0,
)

async function pickFiles(e: Event) {
  const input = e.target as HTMLInputElement
  const list = Array.from(input.files || [])
  error.value = ''

  for (const f of list) {
    if (files.value.length >= MAX_IMAGES) {
      error.value = `最多 ${MAX_IMAGES} 张图片`
      break
    }
    if (f.size > 5 * 1024 * 1024) {
      error.value = `「${f.name}」超过 5MB，已跳过`
      continue
    }
    uploading.value++
    const preview = URL.createObjectURL(f)
    try {
      const fd = new FormData()
      fd.append('file', f)
      const resp = await fetch('/api/v1/upload', {
        method: 'POST',
        headers: { Authorization: `Bearer ${getToken()}` },
        body: fd,
      })
      const body = await resp.json()
      if (body.code !== 0) throw new Error(body.msg)
      files.value.push({ url: body.data.url, preview })
    } catch (err) {
      error.value = err instanceof Error ? err.message : '上传失败'
      URL.revokeObjectURL(preview)
    } finally {
      uploading.value--
    }
  }
  input.value = '' // 允许重复选择同名文件
}

function removeImage(i: number) {
  URL.revokeObjectURL(files.value[i].preview)
  files.value.splice(i, 1)
}

async function submit() {
  error.value = ''
  posting.value = true
  try {
    const tagList = tags.value
      .split(/[\s,，#]+/)
      .map((t) => t.trim())
      .filter(Boolean)
      .slice(0, 5)
    await client.post('/posts', {
      image_urls: files.value.map((f) => f.url),
      content: content.value.trim(),
      tags: tagList,
    })
    router.push('/')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '发布失败'
  } finally {
    posting.value = false
  }
}
</script>

<template>
  <div class="page" style="max-width: 640px">
    <h1 class="compose-title reveal" style="--i: 0">发布新帖</h1>

    <div class="m-card reveal" style="--i: 1">
      <label class="field">
        <span class="field__label">图片（jpg/png，每张 ≤5MB，1~9 张）</span>
        <input
          class="input"
          type="file"
          accept="image/jpeg,image/png"
          multiple
          :disabled="files.length >= MAX_IMAGES"
          @change="pickFiles"
        />
      </label>

      <!-- 已上传图片：预览 + 单张删除 -->
      <div v-if="files.length" class="previews">
        <div v-for="(f, i) in files" :key="f.url" class="preview">
          <img :src="f.preview" alt="预览" />
          <button class="preview__del" title="移除" @click="removeImage(i)">×</button>
        </div>
        <div v-if="uploading > 0" class="preview preview--uploading">
          <span>上传中…</span>
        </div>
      </div>

      <label class="field">
        <span class="field__label">正文（1~1000 字）</span>
        <textarea v-model="content" class="input" rows="5" placeholder="说点什么…"></textarea>
      </label>

      <label class="field">
        <span class="field__label">标签（空格分隔，最多 5 个，可选）</span>
        <input v-model="tags" class="input" placeholder="日常 代码 随笔" />
      </label>

      <div v-if="error" class="m-alert m-alert--error">{{ error }}</div>

      <button class="btn btn--filled" style="width: 100%" :disabled="!canSubmit || posting" @click="submit">
        {{ posting ? '发布中…' : `发布${files.length ? `（${files.length} 张图）` : ''}` }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.compose-title {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  letter-spacing: var(--tracking-display);
  margin: 0 0 var(--sp-3);
  color: var(--on-surface);
}
.previews {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--sp-2);
  margin-bottom: var(--sp-3);
}
.preview {
  position: relative;
  aspect-ratio: 1;
  border-radius: var(--radius-s);
  overflow: hidden;
  background: var(--surface-variant);
}
.preview img { width: 100%; height: 100%; object-fit: cover; display: block; }
.preview--uploading {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--on-surface-variant);
  font-size: var(--fs-caption);
}
.preview__del {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
}
.preview__del:hover { background: rgba(0, 0, 0, 0.8); }
</style>
