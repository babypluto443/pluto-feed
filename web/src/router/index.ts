import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from '../api/client'

// 路由表：meta.auth = 需要登录（全局守卫统一拦截）
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'feed', component: () => import('../views/FeedView.vue') },
    { path: '/hot', name: 'hot', component: () => import('../views/HotView.vue') },
    { path: '/search', name: 'search', component: () => import('../views/SearchView.vue') },
    { path: '/following', name: 'following', component: () => import('../views/FollowingView.vue'), meta: { auth: true } },
    { path: '/compose', name: 'compose', component: () => import('../views/ComposeView.vue'), meta: { auth: true } },
    { path: '/login', name: 'login', component: () => import('../views/LoginView.vue') },
    { path: '/register', name: 'register', component: () => import('../views/RegisterView.vue') },
    { path: '/posts/:id', name: 'post', component: () => import('../views/PostDetailView.vue') },
    { path: '/users/:id', name: 'profile', component: () => import('../views/ProfileView.vue') },
  ],
})

// 全局守卫：meta.auth 的页面必须已登录，否则跳登录页并带 redirect
router.beforeEach((to) => {
  if (to.meta.auth && !getToken()) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
})

export default router
