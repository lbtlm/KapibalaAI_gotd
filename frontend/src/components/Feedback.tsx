import { Alert, Empty, Spin } from 'antd'
import { ApiError } from '../api/client'
export function ErrorNotice({ error }: { error: unknown }) { if (!error) return null; const e = error as ApiError; return <Alert type="error" showIcon title={e.status === 403 ? '没有操作权限' : e.message ?? '加载失败'} description={e.code ? e.code + (e.details?.stepIndex ? ' · 第 ' + e.details.stepIndex + ' 步 · 变量 ' + e.details.key : '') : undefined} /> }
export function LoadState({ loading, empty }: { loading?: boolean; empty?: boolean }) { return loading ? <Spin aria-label="加载中" /> : empty ? <Empty description="暂无数据" /> : null }

