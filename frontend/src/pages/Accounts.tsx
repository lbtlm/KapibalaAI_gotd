import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Table, Typography } from 'antd'
import { api } from '../api/client'
import type { Account } from '../types'
import { buildAccountTableColumns } from '../components/AccountTableColumns'
import { ErrorNotice } from '../components/Feedback'
import { useSession } from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function Accounts() {
 const session = useSession()
 const cache = useQueryClient()
 const query = useQuery({ queryKey: ['accounts'], queryFn: () => api.request<Account[]>('/api/accounts') })
 const action = useMutation({ mutationFn: ({ account, to }: { account: Account; to: 'connect' | 'disconnected' | 'idle' }) => to === 'connect' ? api.request('/api/accounts/' + account.id + '/connect', 'POST') : api.request('/api/accounts/' + account.id + '/transition', 'POST', { to, expectedFrom: account.status }), onSuccess: () => cache.invalidateQueries({queryKey:['accounts']}) })
 return <section><div className={styles.pageHeading}><div><Typography.Text type="secondary">连接资源 / ACCOUNT POOL</Typography.Text><h1>服务账号</h1><p>仅在合法状态下执行操作。限流等待与终态会实时同步。</p></div><span className={styles.metric}>{query.data?.filter(a=>a.status==='online').length ?? '—'}<small>在线账号</small></span></div><ErrorNotice error={query.error ?? action.error}/><Table rowKey="id" loading={query.isLoading} dataSource={query.data} columns={buildAccountTableColumns({writable:session?.role==='admin',busy:action.isPending,onAction:(account,to)=>action.mutate({account,to})})} pagination={false}/></section>
}

