<script setup lang="ts">
// App = Material 3 顶栏（四色点阵 logo + 导航 + 登录态）+ 路由出口
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import './styles/tokens.css'
import './styles/components.css'
import { useAuthStore } from './stores/auth'

const auth = useAuthStore()
const router = useRouter()

onMounted(() => auth.fetchMe()) // 刷新后恢复登录态

function logout() {
  auth.logout()
  router.push('/')
}
</script>

<template>
  <header class="app-bar">
    <div class="app-bar__brand">
      <span class="dots" aria-hidden="true">
        <i class="dots__dot dots__dot--blue" />
        <i class="dots__dot dots__dot--red" />
        <i class="dots__dot dots__dot--yellow" />
        <i class="dots__dot dots__dot--green" />
      </span>
      <RouterLink to="/" class="app-bar__title" style="text-decoration: none">pluto_feed</RouterLink>
    </div>

    <nav class="nav">
      <RouterLink class="nav__link" to="/">推荐</RouterLink>
      <RouterLink class="nav__link" to="/hot">热榜</RouterLink>
      <RouterLink class="nav__link" to="/search">搜索</RouterLink>
      <RouterLink v-if="auth.isLogin" class="nav__link" to="/following">关注流</RouterLink>
      <RouterLink v-if="auth.isLogin" class="nav__link" to="/compose">发布</RouterLink>

      <template v-if="auth.isLogin">
        <RouterLink :to="`/users/${auth.uid}`" class="userbox" style="text-decoration: none">
          <span class="avatar" style="width: 32px; height: 32px; font-size: 13px">
            {{ (auth.me?.nickname || '我').slice(0, 1) }}
          </span>
          <span class="nav__link" style="padding: 4px 8px">{{ auth.me?.nickname || '我' }}</span>
        </RouterLink>
        <button class="btn btn--text" style="min-height: 36px" @click="logout">退出</button>
      </template>
      <template v-else>
        <RouterLink class="nav__link" to="/login">登录</RouterLink>
        <RouterLink class="btn btn--filled" style="min-height: 36px; padding: 6px 18px" to="/register">注册</RouterLink>
      </template>
    </nav>
  </header>

  <main>
    <RouterView />
  </main>
</template>

<style scoped>
.app-bar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding: var(--sp-2) var(--sp-5);
  background: color-mix(in srgb, var(--surface) 82%, transparent);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--outline);
}
.app-bar__brand { display: flex; align-items: center; gap: var(--sp-3); }
.app-bar__title {
  font-family: var(--font-display);
  font-size: var(--fs-title);
  font-weight: 700;
  letter-spacing: var(--tracking-display);
  color: var(--on-surface);
}

.dots { display: inline-flex; gap: 5px; }
.dots__dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  animation: breathe 2.4s var(--ease-out) infinite;
}
.dots__dot--blue   { background: var(--g-blue); }
.dots__dot--red    { background: var(--g-red);   animation-delay: 200ms; }
.dots__dot--yellow { background: var(--g-yellow); animation-delay: 400ms; }
.dots__dot--green  { background: var(--g-green);  animation-delay: 600ms; }
@keyframes breathe {
  0%, 100% { transform: scale(1);   opacity: 1; }
  50%      { transform: scale(0.7); opacity: 0.55; }
}
@media (prefers-reduced-motion: reduce) {
  .dots__dot { animation: none; }
}

main {
  background:
    radial-gradient(60rem 40rem at 8% -10%, rgba(66, 133, 244, 0.10), transparent 60%),
    radial-gradient(50rem 36rem at 100% 0%, rgba(52, 168, 83, 0.08), transparent 55%),
    var(--surface);
  min-height: calc(100vh - 61px);
}

@media (max-width: 860px) {
  .app-bar { flex-wrap: wrap; }
  .nav { margin-left: 0; width: 100%; overflow-x: auto; }
}
</style>
