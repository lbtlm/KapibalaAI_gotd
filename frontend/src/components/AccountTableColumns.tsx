import { Button, Space, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Account } from '../types'
import styles from '../layout/MainLayout.module.scss'
import StatusTag from './StatusTag'
import { formatTime } from '../hooks'
export const buildAccountTableColumns = ({ writable, busy, onAction }: { writable: boolean; busy: boolean; onAction: (account: Account, action: 'connect' | 'disconnected' | 'idle') => void }): ColumnsType<Account> => [
  { title: '账号', dataIndex: 'id', render: (id: string) => <Typography.Text className={styles.mono} strong>{id}</Typography.Text> },
  { title: '平台用户 ID', dataIndex: 'platformUserId', render: (id: string | null) => <Typography.Text className={styles.mono} type="secondary">{id ?? '尚未连接'}</Typography.Text> },
  { title: '状态', dataIndex: 'status', render: (status: string) => <StatusTag status={status}/> },
  { title: '限流截止', dataIndex: 'rateLimitedUntil', render: formatTime },
  ...(writable ? [{ title: '操作', key: 'actions', render: (_: unknown, a: Account) => <Space>
    {(a.status === 'idle' || a.status === 'disconnected') && <Button type="text" aria-label="重连" loading={busy} onClick={() => onAction(a, 'connect')}>重连</Button>}
    {(a.status === 'online' || a.status === 'rate_limited') && <Button type="text" aria-label="标记离线" disabled={busy} onClick={() => onAction(a, 'disconnected')}>标记离线</Button>}
    {(a.status === 'online' || a.status === 'disconnected') && <Button type="text" aria-label="释放账号" disabled={busy} onClick={() => onAction(a, 'idle')}>释放账号</Button>}
  </Space> }] : [])
]
