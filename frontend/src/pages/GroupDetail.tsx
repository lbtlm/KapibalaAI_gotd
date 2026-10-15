import {useState} from 'react'
import {useParams,Link} from 'react-router-dom'
import {useInfiniteQuery,useQuery} from '@tanstack/react-query'
import {Alert,Button,Tabs,Table,Typography} from 'antd'
import {ArrowLeftOutlined} from '@ant-design/icons'
import {api} from '../api/client'
import type {Group,Message,MessagePage,AgentRun,Account,Job} from '../types'
import {formatTime,useSession} from '../hooks'
import StatusTag from '../components/StatusTag'
import {ErrorNotice,LoadState} from '../components/Feedback'
import {accountLabel} from '../components/AccountTableColumns'
import GroupActions,{GroupComposer} from '../components/GroupActions'
import GroupMembers from '../components/GroupMembers'
import GroupSequence from '../components/GroupSequence'
import {jobPhaseLabel} from '../components/JobFeedback'
import styles from '../layout/MainLayout.module.scss'

export default function GroupDetail(){
 const {id}=useParams(),session=useSession(),[tab,setTab]=useState('chat')
 const group=useQuery({queryKey:['group',id],queryFn:()=>api.request<Group>('/api/groups/'+id),refetchInterval:5000})
 const accounts=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const runs=useQuery({queryKey:['agent-runs',id],queryFn:()=>api.request<AgentRun[]>('/api/groups/'+id+'/agent-runs')})
 const jobs=useQuery({queryKey:['group-jobs',id],queryFn:()=>api.request<Job[]>('/api/groups/'+id+'/jobs'),refetchInterval:tab==='logs'?2000:5000})
 const messages=useInfiniteQuery({queryKey:['messages',id],initialPageParam:null as string|null,queryFn:({pageParam})=>api.request<MessagePage>('/api/groups/'+id+'/messages?limit=50'+(pageParam?'&before='+encodeURIComponent(pageParam):'')),getNextPageParam:last=>last.nextCursor??undefined})
 const unique=new Map<string,Message>()
 for(const page of messages.data?.pages??[])for(const message of page.items){const key=message.clientMsgId??message.msgId;if(key&&!unique.has(key))unique.set(key,message)}
 const timeline=[...unique.values()].sort((a,b)=>a.sentAt.localeCompare(b.sentAt)||(a.msgId??a.clientMsgId??'').localeCompare(b.msgId??b.clientMsgId??''))
 const g=group.data,writable=session?.role==='admin',external=timeline.filter(m=>!m.isOwn)
 const label=(pid:string)=>{const a=accounts.data?.find(a=>a.platformUserId===pid);return a?accountLabel(a):pid}
 const chat=(onlyExternal:boolean)=><><header className={styles.chatHeading}><h2>{onlyExternal?'外部成员消息':'实时聊天'}<small>{(onlyExternal?external:timeline).length} 条已加载</small></h2><Button type="text" aria-label="加载更早" onClick={()=>messages.fetchNextPage()} disabled={!messages.hasNextPage} loading={messages.isFetchingNextPage}>加载更早</Button></header><div className={styles.timeline}>
  <LoadState loading={messages.isLoading} empty={!messages.isLoading&&!(onlyExternal?external:timeline).length} description="群消息会实时显示在这里。"/>
  {(onlyExternal?external:timeline).map(m=><div key={m.clientMsgId??m.msgId} className={styles.message+' '+(m.isOwn?styles.messageOwn:styles.messageExternal)}><div className={styles.messageMeta}><strong>{m.isOwn?'自有账号':'外部成员'} · {label(m.senderPlatformUserId)}</strong><time>{formatTime(m.sentAt)}</time></div><div className={styles.messageText}><p>{m.text}</p></div>{m.isOwn&&<div className={styles.messageDelivery}>{m.deliveryStatus&&<StatusTag status={m.deliveryStatus}/>}<span className={styles.meta}>{m.failCode}</span></div>}</div>)}
 </div>{g&&writable&&<GroupComposer group={g} accounts={accounts.data??[]}/>}</>
 return <section><div className={styles.pageHeading}><div><Link className={styles.backLink} to="/groups"><ArrowLeftOutlined/> 群组列表</Link><div className={styles.headingTitle}><h1>{g?.title||'群消息工作区'}</h1>{g&&<StatusTag status={g.status}/>}</div><p>{g?.gatewayGroupId??id}</p>{(g?.username||g?.inviteLink)&&<Typography.Text copyable>{g.username?'https://t.me/'+g.username:g.inviteLink}</Typography.Text>}</div></div>
  <ErrorNotice error={group.error??messages.error??runs.error??accounts.error??jobs.error}/><LoadState loading={group.isLoading}/>
  {!!external.length&&<Alert type="warning" showIcon title={'已加载 '+external.length+' 条外部成员消息'} description="仅外部成员消息可触发已启用的 Agent。" action={<Button size="small" onClick={()=>setTab('external')}>单独查看</Button>}/>}
  {g&&<div className={styles.groupGrid}><article className={styles.chatArea}><Tabs activeKey={tab} onChange={setTab} items={[
   {key:'chat',label:'实时聊天',children:chat(false)},
   {key:'external',label:'外部消息 '+external.length,children:chat(true)},
   {key:'script',label:'剧本与账号',children:<div style={{padding:16}}><GroupSequence key={g.id} group={g} accounts={accounts.data??[]} writable={writable}/></div>},
   {key:'logs',label:'操作日志',children:<Table rowKey="id" size="small" dataSource={jobs.data} pagination={false} columns={[{title:'时间',dataIndex:'createdAt',render:formatTime},{title:'任务',dataIndex:'kind',render:k=>({create:'建群',join:'成员加群',promote:'设置管理员',leave:'结束营销'}[k as string]??k)},{title:'状态',dataIndex:'status',render:s=><StatusTag status={s}/>},{title:'阶段',dataIndex:'phase',render:jobPhaseLabel},{title:'结果',render:(_,j)=>j.errors.map(e=>e.step+' · '+e.code).join('；')||'-'}]} expandable={{expandedRowRender:j=><ol>{j.logs?.map((entry,i)=><li key={i}>{formatTime(entry.at)} · {jobPhaseLabel(entry.phase)} · {entry.accountId??entry.position??''} · <StatusTag status={entry.result}/>{entry.code}</li>)}</ol>}}/>}
  ]}/></article><aside className={styles.diagnostics}>
   <GroupMembers group={g} accounts={accounts.data??[]} writable={writable}/>
   <section className={styles.sideSection}><h2>本群 Agent 运行</h2><LoadState loading={runs.isLoading} empty={!runs.isLoading&&!runs.data?.length} description="启用后，外部成员消息会触发本群独立运行。"/>{runs.data?.map(run=><Link className={styles.run} key={run.id} to={'/agent-runs/'+run.id}><div className={styles.runHeader}><strong>{run.id.slice(0,8)}</strong><StatusTag status={run.status}/></div><p>{run.endReason??'正在执行'}</p></Link>)}</section>
   {writable&&<GroupActions group={g}/>}
  </aside></div>}
 </section>
}
