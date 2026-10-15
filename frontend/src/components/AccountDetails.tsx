import {useState} from 'react'
import {useInfiniteQuery,useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Alert,Button,Descriptions,Modal,Select,Space,Table,Tabs,Tag,Typography} from 'antd'
import {api} from '../api/client'
import type {Account,AccountGroup,AccountLogPage,JoinedGroups} from '../types'
import {accountName,type AccountAction} from './AccountTableColumns'
import {LoadState} from './Feedback'
import {AccountErrorNotice as ErrorNotice,accountErrorText} from './AccountFeedback'
import StatusTag from './StatusTag'
import {formatTime} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
const actions:Record<string,string>={import:'导入账号',check:'检测账号',connect:'连接账号',disconnect:'标记离线',transition:'变更状态',sync_groups:'同步所在群',group_move:'移动分组',group_rename:'重命名分组',group_delete:'删除分组',connection_ended:'连接已断开'}
const roles={creator:'群主',admin:'管理员',member:'成员',unknown:'角色未知'}
export default function AccountDetails({id,groups,writable,busy,error,onAction,onClose}:{id:string;groups:AccountGroup[];writable:boolean;busy:boolean;error:unknown;onAction:(a:Account,to:AccountAction)=>void;onClose:()=>void}){
 const cache=useQueryClient(),[tab,setTab]=useState('basic'),[actionFilter,setActionFilter]=useState<string>(),[result,setResult]=useState<string>()
 const account=useQuery({queryKey:['account',id],queryFn:()=>api.request<Account>('/api/accounts/'+encodeURIComponent(id))})
 const joined=useQuery({queryKey:['account-joined-groups',id],queryFn:()=>api.request<JoinedGroups>('/api/accounts/'+encodeURIComponent(id)+'/joined-groups'),enabled:tab==='groups'})
 const logs=useInfiniteQuery({queryKey:['account-logs',id,actionFilter,result],initialPageParam:'',queryFn:({pageParam})=>{const params=new URLSearchParams();if(pageParam)params.set('cursor',pageParam);if(actionFilter)params.set('action',actionFilter);if(result)params.set('result',result);return api.request<AccountLogPage>('/api/accounts/'+encodeURIComponent(id)+'/logs?'+params)},getNextPageParam:r=>r.nextCursor??undefined,enabled:tab==='logs'})
 const sync=useMutation({mutationFn:()=>api.request<JoinedGroups>('/api/accounts/'+encodeURIComponent(id)+'/joined-groups/sync','POST'),onSuccess:r=>cache.setQueryData(['account-joined-groups',id],r),onSettled:()=>cache.invalidateQueries({queryKey:['account-logs',id]})})
 const a=account.data,online=a?.status==='online',terminal=a&&['suspended','session_expired'].includes(a.status)
 const logItems=logs.data?.pages.flatMap(p=>p.items)??[]
 return <Modal title={'账号详情 · '+id} open width={1080} footer={null} onCancel={onClose} destroyOnHidden styles={{body:{maxHeight:'72dvh',overflowY:'auto'}}}>
  <ErrorNotice error={account.error??error}/>
  <LoadState loading={account.isLoading}/>
  {account.isError&&<Button onClick={()=>account.refetch()}>重试加载详情</Button>}
  {a&&<><div className={styles.accountDetailHeading}><div><Typography.Title level={4} style={{margin:0}}>{accountName(a)}</Typography.Title><Space><Typography.Text copyable={a.phone?{text:a.phone}:false}>{a.phone??'未获取手机号'}</Typography.Text><StatusTag status={a.status}/></Space></div>{writable&&!terminal&&<Space>{['idle','disconnected','online'].includes(a.status)&&<Button aria-label={online?'检测':'连接并检测'} loading={busy} onClick={()=>onAction(a,'check')}>{online?'检测':'连接并检测'}</Button>}{['online','rate_limited'].includes(a.status)&&<Button disabled={busy} onClick={()=>onAction(a,'disconnected')}>标记离线</Button>}</Space>}</div>
  <Tabs activeKey={tab} onChange={setTab} items={[
   {key:'basic',label:'基础信息',children:<Descriptions bordered size="small" column={2} items={[
    {key:'id',label:'账号 ID',children:a.id},{key:'platform',label:'Telegram ID',children:a.platformUserId??'未获取'},
    {key:'name',label:'昵称',children:accountName(a)},{key:'username',label:'用户名',children:a.username?'@'+a.username:'未获取'},
    {key:'phone',label:'手机号来源',children:a.verificationSource==='simulated'?'模拟资料，未核验手机号':a.phoneVerified?'Telegram 已核验':a.phone?'导入资料，待检测核验':'未获取'},
    {key:'premium',label:'Premium',children:a.premium==null?'未获取':a.premium?'是':'否'},
    {key:'group',label:'账号分组',children:groups.find(g=>g.id===a.groupId)?.name??'未分组'},
    {key:'created',label:'导入时间',children:formatTime(a.createdAt??null)},
    {key:'checked',label:'最近检测',children:formatTime(a.lastCheckedAt??null)},
    {key:'result',label:'检测结果',children:a.verificationSource==='simulated'?'模拟检测'+(a.lastCheckResult==='failed'?'失败':'成功'):a.lastCheckResult==='success'?'检测成功':a.lastCheckResult==='failed'?'检测失败'+(a.lastCheckError?' · '+a.lastCheckError:''):'尚未检测'},
    {key:'until',label:'限流截止',children:formatTime(a.rateLimitedUntil)}
   ]}/>},
   {key:'groups',label:'所在群',children:<><ErrorNotice error={joined.error??sync.error}/><div className={styles.accountDetailToolbar}><span className={styles.meta}>最近同步：{formatTime(joined.data?.lastSyncedAt??null)}</span>{writable&&<Button disabled={!online||busy} loading={sync.isPending} onClick={()=>sync.mutate()}>同步群组</Button>}</div>{!online&&<Alert type="info" title="请先连接并检测账号，再同步所在群。当前显示最近成功缓存。"/>}<p className={styles.meta}>仅展示账号已加入的群，同步不会自动托管群组或启用 AI。</p><Table size="small" rowKey="id" dataSource={joined.data?.items??[]} loading={joined.isLoading} scroll={{x:700,y:320}} pagination={{pageSize:10,showSizeChanger:false}} locale={{emptyText:<LoadState empty description="尚无成功同步的群组快照。"/>}} columns={[
    {title:'群名称',dataIndex:'title'},{title:'群 ID',dataIndex:'id'},{title:'用户名',dataIndex:'username',render:v=>v?'@'+v:'-'},{title:'类型',dataIndex:'type',render:v=>v==='supergroup'?'超级群':'普通群'},{title:'账号角色',dataIndex:'role',render:(v:keyof typeof roles)=><Tag>{roles[v]??roles.unknown}</Tag>}
   ]}/>{joined.isError&&<Button onClick={()=>joined.refetch()}>重试读取缓存</Button>}</>},
   {key:'logs',label:'操作日志',children:<><ErrorNotice error={logs.error}/><Space className={styles.accountDetailToolbar}><Select aria-label="日志操作" placeholder="全部操作" allowClear value={actionFilter} onChange={setActionFilter} className={styles.accountSelect} options={Object.entries(actions).map(([value,label])=>({value,label}))}/><Select aria-label="日志结果" placeholder="全部结果" allowClear value={result} onChange={setResult} className={styles.accountSelect} options={[{value:'success',label:'成功'},{value:'failed',label:'失败'}]}/></Space><Table size="small" rowKey="id" dataSource={logItems} loading={logs.isLoading} pagination={false} scroll={{x:700,y:320}} locale={{emptyText:<LoadState empty description="尚无账号操作记录。"/>}} columns={[
    {title:'时间',dataIndex:'createdAt',render:formatTime},{title:'操作人',dataIndex:'actor'},{title:'操作',dataIndex:'action',render:v=>actions[v]??v},{title:'结果',dataIndex:'result',render:v=><Tag color={v==='success'?'success':'error'}>{v==='success'?'成功':'失败'}</Tag>},{title:'错误摘要',dataIndex:'errorCode',render:v=>v?<span>{accountErrorText(v)}<div className={styles.meta}>{v}</div></span>:'-'}
   ]}/>{logs.hasNextPage&&<Button loading={logs.isFetchingNextPage} onClick={()=>logs.fetchNextPage()}>加载更多日志</Button>}{logs.isError&&<Button onClick={()=>logs.refetch()}>重试读取日志</Button>}</>}
  ]}/></>}
 </Modal>
}
