<script setup lang="ts">
// 登录/注册共用表单逻辑抽在此处两个视图里保持简单直接
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const props = withDefaults(defineProps<{ mode?: 'login' | 'register' }>(), { mode: 'login' })
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const nickname = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  if (!nickname.value.trim() || password.value.length < 6) {
    error.value = '昵称不能为空，密码至少 6 位'
    return
  }
  busy.value = true
  try {
    if (props.mode === 'login') await auth.login(nickname.value, password.value)
    else await auth.register(nickname.value, password.value)
    router.push((route.query.redirect as string) || '/')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-wrap">
    <div class="m-card auth-card reveal" style="--i: 0">
      <h1 class="auth-title">{{ mode === 'login' ? '欢迎回来' : '加入 pluto_feed' }}</h1>
      <p class="auth-sub">{{ mode === 'login' ? '登录你的账号' : '注册一个新账号，注册即登录' }}</p>

      <form @submit.prevent="submit">
        <label class="field">
          <span class="field__label">昵称</span>
          <input v-model="nickname" class="input" autocomplete="username" placeholder="nickname" />
        </label>
        <label class="field">
          <span class="field__label">密码（至少 6 位）</span>
          <input v-model="password" class="input" type="password" autocomplete="current-password" placeholder="••••••" />
        </label>

        <div v-if="error" class="m-alert m-alert--error">{{ error }}</div>

        <button class="btn btn--filled" style="width: 100%" :disabled="busy" type="submit">
          {{ busy ? '提交中…' : mode === 'login' ? '登录' : '注册' }}
        </button>
      </form>

      <p class="auth-switch">
        <template v-if="mode === 'login'">
          还没有账号？<RouterLink to="/register">去注册</RouterLink>
        </template>
        <template v-else>
          已有账号？<RouterLink to="/login">去登录</RouterLink>
        </template>
      </p>
    </div>
  </div>
</template>

<style scoped>
.auth-wrap { max-width: 420px; margin: var(--sp-6) auto; padding: 0 var(--sp-4); }
.auth-title {
  font-family: var(--font-display);
  font-size: 28px;
  letter-spacing: var(--tracking-display);
  margin: 0 0 4px;
  color: var(--on-surface);
}
.auth-sub { color: var(--on-surface-variant); margin: 0 0 var(--sp-4); font-size: var(--fs-body); }
.auth-switch { text-align: center; font-size: var(--fs-body); color: var(--on-surface-variant); }
.auth-switch a { color: var(--primary); font-weight: 500; }
</style>
