import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Table} from 'antd'
import {api} from '../api/client'
import type {Account} from '../types'
import {buildAccountTableColumns} from '../components/AccountTableColumns'
import {ErrorNotice,LoadState} from '../components/Feedback'
import {useSession} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function Accounts(){
 const session=useSession(),cache=useQueryClient()
 const query=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const action=useMutation({mutationFn:({account,to}:{account:Account;to:'connect'|'disconnected'|'idle'})=>to==='connect'?api.request('/api/accounts/'+account.id+'/connect','POST'):api.request('/api/accounts/'+account.id+'/transition','POST',{to,expectedFrom:account.status}),onSuccess:()=>cache.invalidateQueries({queryKey:['accounts']})})
 const accounts=query.data??[]
 return <section><div className={styles.pageHeading}><div><h1>服务账号</h1><p>查看连接状态与限流时间，按当前状态操作。</p></div></div><ErrorNotice error={query.error??action.error}/><div className={styles.toolbar}><div className={styles.summary}><span><strong>{accounts.length}</strong>个账号</span><span><strong>{accounts.filter(a=>a.status==='online').length}</strong>在线</span><span><strong>{accounts.filter(a=>a.status==='rate_limited').length}</strong>限流中</span><span><strong>{accounts.filter(a=>['suspended','session_expired'].includes(a.status)).length}</strong>不可用</span></div><span className={styles.meta}>{session?.role==='viewer'?'只读模式':'终态账号不可重连'}</span></div><div className={styles.tableShell}><Table size="small" rowKey="id" locale={{emptyText:<LoadState loading={query.isLoading} empty={!query.isLoading} description="服务账号由迁移预置，连接后可管理群组。"/>}} dataSource={query.data} columns={buildAccountTableColumns({writable:session?.role==='admin',busy:action.isPending,onAction:(account,to)=>action.mutate({account,to})})} pagination={false}/></div></section>
}
