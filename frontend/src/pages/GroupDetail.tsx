import {useState} from 'react'
import {useParams,Link} from 'react-router-dom'
import {useInfiniteQuery,useQuery} from '@tanstack/react-query'
import {Button,Tabs,Typography} from 'antd'
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
import ExternalMessageNotice from '../components/ExternalMessageNotice'
import GroupLogs from '../components/GroupLogs'
import MessageTimeline from '../components/MessageTimeline'
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
 const chat=(onlyExternal:boolean)=><div className={styles.chatPanel}><header className={styles.chatHeading}><h2>{onlyExternal?'外部成员消息':'实时聊天'}<small>{(onlyExternal?external:timeline).length} 条已加载</small></h2><Button type="text" aria-label="加载更早" onClick={()=>messages.fetchNextPage()} disabled={!messages.hasNextPage} loading={messages.isFetchingNextPage}>加载更早</Button></header><MessageTimeline active={tab===(onlyExternal?'external':'chat')} latestKey={(onlyExternal?external:timeline).at(-1)?.clientMsgId??(onlyExternal?external:timeline).at(-1)?.msgId??''} oldestKey={(onlyExternal?external:timeline)[0]?.clientMsgId??(onlyExternal?external:timeline)[0]?.msgId??''}>
  <LoadState loading={messages.isLoading} empty={!messages.isLoading&&!(onlyExternal?external:timeline).length} description="群消息会实时显示在这里。"/>
  {(onlyExternal?external:timeline).map(m=><div key={m.clientMsgId??m.msgId} className={styles.message+' '+(m.isOwn?styles.messageOwn:styles.messageExternal)}><div className={styles.messageMeta}><strong>{m.isOwn?'自有账号':'外部成员'} · {label(m.senderPlatformUserId)}</strong><time>{formatTime(m.sentAt)}</time></div><div className={styles.messageText}><p>{m.text}</p></div>{m.isOwn&&<div className={styles.messageDelivery}>{m.deliveryStatus&&<StatusTag status={m.deliveryStatus}/>}<span className={styles.meta}>{m.failCode}</span></div>}</div>)}
 </MessageTimeline>{g&&writable&&<GroupComposer group={g} accounts={accounts.data??[]}/>}</div>
 return <section><div className={styles.pageHeading}><div><Link className={styles.backLink} to="/groups"><ArrowLeftOutlined/> 群组列表</Link><div className={styles.headingTitle}><h1>{g?.title||'群消息工作区'}</h1>{g&&<StatusTag status={g.status==='active'&&g.marketingPaused?'paused':g.status}/>}</div><p>{g?.gatewayGroupId??id}</p>{(g?.username||g?.inviteLink)&&<Typography.Text copyable>{g.username?'https://t.me/'+g.username:g.inviteLink}</Typography.Text>}</div></div>
  <ErrorNotice error={group.error??messages.error??runs.error??accounts.error??jobs.error}/><LoadState loading={group.isLoading}/>
  <ExternalMessageNotice key={id} messages={external} active={tab==='external'} onOpen={()=>setTab('external')}/>
  {g&&<div className={styles.groupGrid}><article className={styles.chatArea}><Tabs className={styles.groupTabs} activeKey={tab} onChange={setTab} items={[
   {key:'chat',label:'实时聊天',children:chat(false)},
   {key:'external',label:'外部消息 '+external.length,children:chat(true)},
   {key:'script',label:'剧本与账号',children:<div style={{padding:16}}><GroupSequence key={g.id} group={g} accounts={accounts.data??[]} writable={writable}/></div>},
   {key:'logs',label:'操作日志',children:<div style={{padding:16}}><GroupLogs jobs={jobs.data??[]} accounts={accounts.data??[]}/></div>}
  ]}/></article><aside className={styles.diagnostics}>
   {writable&&<div className={styles.sideControls}><GroupActions group={g}/></div>}
   <div className={styles.memberSection}><GroupMembers group={g} accounts={accounts.data??[]} writable={writable}/></div>
   <section className={styles.agentSection}><div className={styles.runHeader}><h2>本群专属 Agent</h2><span className={styles.meta}>{g.status==='left'||g.status==='ending'?'已结束':g.marketingPaused?(g.activeAgentRunId?'正在暂停':'已暂停'):!g.agentEnabled?'已关闭':g.activeAgentRunId?'正在处理消息':'持续待命'}</span></div><p className={styles.meta} title={g.agentSessionId}>固定身份 · {g.id.slice(0,8)}<br/>每次处理完成后继续待命，上下文随群保存。</p><h2>消息处理记录</h2><div className={styles.agentRunList}><LoadState loading={runs.isLoading} empty={!runs.isLoading&&!runs.data?.length} description="启用后，外部成员消息会交给本群同一个 Agent。"/>{runs.data?.map(run=><Link className={styles.run} key={run.id} to={'/agent-runs/'+run.id}><div className={styles.runHeader}><strong>{run.id.slice(0,8)}</strong><StatusTag status={run.status}/></div><p>{run.endReason==='final'?'本次处理完成':run.endReason??'正在执行'}</p></Link>)}</div></section>
  </aside></div>}
 </section>
}
