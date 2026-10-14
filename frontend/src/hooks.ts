import { useSyncExternalStore } from 'react'
import { api } from './api/client'
export const useSession = () => useSyncExternalStore(api.subscribe, api.getSession)
export const formatTime = (value: string | null) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'

