import axios, { AxiosError, type AxiosInstance } from 'axios'
export type Role = 'admin' | 'viewer'
export interface Session { accessToken: string; role?: Role }
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details: Record<string, unknown> = {}) { super(message) }
}
export class ApiClient {
  private instance: AxiosInstance
  private session: Session | null = null
  private sessionGeneration = 0
  private refreshPromise: Promise<Session> | null = null
  private listeners = new Set<() => void>()
  constructor(baseURL = '') { this.instance = axios.create({ baseURL, withCredentials: true, timeout: 15000 }) }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  getSession = () => this.session
  setSession(session: Session | null) {
    this.sessionGeneration++
    this.refreshPromise = null
    this.updateSession(session)
  }
  private assertGeneration(generation: number) {
    if (generation !== this.sessionGeneration) throw new ApiError(401, 'SESSION_CHANGED', '会话已变更，旧请求已取消')
  }
  private updateSession(session: Session | null) {
    if (session && !session.role) session.role = 'viewer'
    this.session = session
    this.listeners.forEach(fn => fn())
  }
  async refresh() {
    if (!this.refreshPromise) {
      const generation = this.sessionGeneration
      const pending: Promise<Session> = this.request<Session>('/api/auth/refresh', 'POST', undefined, false)
        .then(session => { this.assertGeneration(generation); this.updateSession(session); return session })
        .catch(error => { this.assertGeneration(generation); this.setSession(null); throw error })
        .finally(() => { if (this.refreshPromise === pending) this.refreshPromise = null })
      this.refreshPromise = pending
    }
    return this.refreshPromise
  }
  async login(username: string, password: string) {
    this.setSession(null)
    const generation = this.sessionGeneration
    const session = await this.request<Session>('/api/auth/login', 'POST', { username, password }, false)
    this.assertGeneration(generation)
    this.updateSession(session)
  }
  async logout() {
    const generation = ++this.sessionGeneration
    this.refreshPromise = null
    try { await this.request('/api/auth/logout', 'POST') } finally {
      if (generation === this.sessionGeneration) this.setSession(null)
    }
  }
  async request<T>(url: string, method = 'GET', data?: unknown, authenticated = true, retry = true): Promise<T> {
    const generation = this.sessionGeneration
    const sentToken = this.session?.accessToken
    try {
      const response = await this.instance.request<T>({ url, method, data, headers: authenticated && sentToken ? { Authorization: 'Bearer ' + sentToken } : {} })
      return response.data
    } catch (error) {
      const e = error as AxiosError<{ error?: Record<string, unknown> }>
      if (e.response?.status === 401 && authenticated) this.assertGeneration(generation)
      if (e.response?.status === 401 && authenticated && retry) {
        if (sentToken === this.session?.accessToken || !this.session) await this.refresh()
        this.assertGeneration(generation)
        return this.request<T>(url, method, data, authenticated, false)
      }
      const detail = e.response?.data?.error ?? {}
      if (e.response?.status === 401 && authenticated) this.setSession(null)
      throw new ApiError(e.response?.status ?? 0, String(detail.code ?? 'NETWORK_ERROR'), String(detail.message ?? '请求失败，请稍后重试'), detail)
    }
  }
}
export const api = new ApiClient()

