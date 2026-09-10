// 统一 API 客户端：自动携带 Bearer token；401 时清登录态跳登录页。
import axios from 'axios'

export const TOKEN_KEY = 'pf_access'
export const getToken = () => localStorage.getItem(TOKEN_KEY) || ''
export const setToken = (t: string) => localStorage.setItem(TOKEN_KEY, t)
export const clearToken = () => localStorage.removeItem(TOKEN_KEY)

const raw = axios.create({ baseURL: '/api/v1', timeout: 8000 })

// 请求拦截：有 token 就带上
raw.interceptors.request.use((cfg) => {
  const t = getToken()
  if (t) cfg.headers.Authorization = `Bearer ${t}`
  return cfg
})

// 响应拦截：解包 {code, msg, data}；401 → 清态并跳登录
raw.interceptors.response.use(
  (resp) => {
    const body = resp.data
    if (body && typeof body.code === 'number' && body.code !== 0) {
      return Promise.reject(new Error(body.msg || '请求失败'))
    }
    return body?.data !== undefined ? body.data : body
  },
  (err) => {
    if (err?.response?.status === 401 && location.pathname !== '/login') {
      clearToken()
      localStorage.removeItem('pf_uid')
      location.href = '/login'
    }
    return Promise.reject(err)
  },
)

// 类型层面声明"返回值已是解包后的业务数据"（与拦截器行为一致）
const client = raw as unknown as {
  get<T = any>(url: string, config?: Record<string, unknown>): Promise<T>
  post<T = any>(url: string, body?: unknown, config?: Record<string, unknown>): Promise<T>
  delete<T = any>(url: string, config?: Record<string, unknown>): Promise<T>
}
export default client
