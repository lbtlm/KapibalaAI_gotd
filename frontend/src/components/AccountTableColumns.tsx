import { Button, Space, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Account, AccountGroup } from '../types'
import styles from '../layout/MainLayout.module.scss'
import StatusTag from './StatusTag'
import { formatTime } from '../hooks'
export type AccountAction = 'check' | 'disconnected' | 'idle'
export const accountName = (a: Account) => [a.firstName,a.lastName].filter(Boolean).join(' ') || '未获取昵称'
export const buildAccountTableColumns = ({ writable, busy, groups, onAction, onDetail }: { writable: boolean; busy: boolean; groups: AccountGroup[]; onAction: (account: Account, action: AccountAction) => void; onDetail: (account: Account) => void }): ColumnsType<Account> => [
  { title: '手机号 / 账号', key: 'phone', width: 210, render: (_, a) => <div><Typography.Text className={styles.mono} copyable={a.phone?{text:a.phone}:false}>{a.phone || '未获取手机号'}</Typography.Text><div className={styles.meta+' '+styles.accountIdentity}>{a.id}{a.phone&&!a.phoneVerified?' · 待检测核验':''}</div></div> },
  { title: '昵称 / 用户名', key: 'name', width: 180, render: (_,a) => <div>{accountName(a)}<div className={styles.meta}>{a.username?'@'+a.username:a.verificationSource==='telegram'?'未设置用户名':'未获取用户名'}</div></div> },
  { title: 'Telegram ID', dataIndex: 'platformUserId', width: 150, render: (id: string | null) => <Typography.Text className={styles.mono}>{id ?? '未获取'}</Typography.Text> },
  { title: '分组', key: 'group', width: 130, render: (_,a) => groups.find(g=>g.id===a.groupId)?.name ?? '未分组' },
  { title: '连接状态', dataIndex: 'status', width: 120, render: (status: string, a) => <div><StatusTag status={status}/>{a.status==='rate_limited'&&<div className={styles.meta}>至 {formatTime(a.rateLimitedUntil)}</div>}</div> },
  { title: '最近检测', key: 'checked', width: 180, render: (_,a) => <div>{formatTime(a.lastCheckedAt??null)}{a.lastCheckResult&&<div className={a.lastCheckResult==='failed'?styles.checkFailed:styles.meta}>{a.lastCheckResult==='success'?'检测成功':'检测失败'}{a.lastCheckError?' · '+a.lastCheckError:''}</div>}</div> },
  { title: '操作', key: 'actions', width: writable?320:80, fixed:'right', render: (_, a) => <Space size={0}>
    <Button type="text" aria-label="详情" onClick={() => onDetail(a)}>详情</Button>
    {writable&&<>
      {['idle','disconnected','online'].includes(a.status)&&<Button type="text" aria-label={a.status==='online'?'检测':'连接并检测'} disabled={busy} onClick={() => onAction(a, 'check')}>{a.status==='online'?'检测':'连接并检测'}</Button>}
      {(a.status === 'online' || a.status === 'rate_limited') && <Button type="text" aria-label="标记离线" disabled={busy} onClick={() => onAction(a, 'disconnected')}>标记离线</Button>}
      {(a.status === 'online' || a.status === 'disconnected') && <Button type="text" aria-label="释放账号" disabled={busy} onClick={() => onAction(a, 'idle')}>释放账号</Button>}
    </>}
  </Space> }
]
