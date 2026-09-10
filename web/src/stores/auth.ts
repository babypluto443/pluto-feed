// 登录态 store：token + 当前用户，localStorage 持久化（刷新不丢）。
import { defineStore } from 'pinia'
import client, { getToken, setToken, clearToken } from '../api/client'

interface Me {
  id: number
  nickname: string
  avatar_url: string
  bio: string
  following_count: number
  follower_count: number
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    uid: Number(localStorage.getItem('pf_uid')) || 0,
    me: null as Me | null,
  }),
  getters: {
    isLogin: (s) => !!getToken() && s.uid > 0,
  },
  actions: {
    async login(nickname: string, password: string) {
      const data = await client.post('/auth/login', { nickname, password })
      setToken(data.access_token)
      await this.fetchMe()
    },
    async register(nickname: string, password: string) {
      await client.post('/auth/register', { nickname, password })
      await this.login(nickname, password) // 注册即登录，体验顺滑
    },
    async fetchMe() {
      if (!getToken()) return
      const me = await client.get('/me')
      this.me = me
      this.uid = me.id
      localStorage.setItem('pf_uid', String(me.id))
    },
    logout() {
      clearToken()
      localStorage.removeItem('pf_uid')
      this.uid = 0
      this.me = null
    },
  },
})
