import {useParams,Link} from 'react-router-dom'
import {useInfiniteQuery,useQuery} from '@tanstack/react-query'
import {Button} from 'antd'
import {ArrowLeftOutlined} from '@ant-design/icons'
import {api} from '../api/client'
import type {Group,Message,MessagePage,AgentRun,Account} from '../types'
import {formatTime,useSession} from '../hooks'
import StatusTag from '../components/StatusTag'
import {ErrorNotice,LoadState} from '../components/Feedback'
import GroupActions,{GroupComposer} from '../components/GroupActions'
import styles from '../layout/MainLayout.module.scss'
const roleNames={creator:'群主',admin:'管理员',member:'成员'}
export default function GroupDetail(){
 const {id}=useParams(),session=useSession()
 const group=useQuery({queryKey:['group',id],queryFn:()=>api.request<Group>('/api/groups/'+id)})
 const accounts=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const runs=useQuery({queryKey:['agent-runs',id],queryFn:()=>api.request<AgentRun[]>('/api/groups/'+id+'/agent-runs')})
 const messages=useInfiniteQuery({queryKey:['messages',id],initialPageParam:null as string|null,queryFn:({pageParam})=>api.request<MessagePage>('/api/groups/'+id+'/messages?limit=50'+(pageParam?'&before='+encodeURIComponent(pageParam):'')),getNextPageParam:last=>last.nextCursor??undefined})
 const unique=new Map<string,Message>()
 for(const page of messages.data?.pages??[])for(const message of page.items){const key=message.clientMsgId??message.msgId;if(key&&!unique.has(key))unique.set(key,message)}
 const timeline=[...unique.values()].sort((a,b)=>a.sentAt.localeCompare(b.sentAt)||(a.msgId??a.clientMsgId??'').localeCompare(b.msgId??b.clientMsgId??''))
 const g=group.data
 return <section><div className={styles.pageHeading}><div><Link className={styles.backLink} to="/groups"><ArrowLeftOutlined/> 群组列表</Link><div className={styles.headingTitle}><h1>群消息工作区</h1>{g&&<StatusTag status={g.status}/>}</div><p><span className={styles.mono}>{id}</span> · 网关 {g?.gatewayGroupId??'尚未读取'}</p></div><Link to={'/sequences?group='+id}>序列运行</Link></div><ErrorNotice error={group.error??messages.error??runs.error??accounts.error}/><LoadState loading={group.isLoading}/>{g&&<div className={styles.groupGrid}><article className={styles.chatArea}><header className={styles.chatHeading}><h2>消息时间线<small>{timeline.length} 条已加载</small></h2><Button type="text" aria-label="加载更早" onClick={()=>messages.fetchNextPage()} disabled={!messages.hasNextPage} loading={messages.isFetchingNextPage}>加载更早</Button></header><div className={styles.timeline}><LoadState loading={messages.isLoading} empty={!messages.isLoading&&!timeline.length} description="群里的收发消息会实时显示在这里。"/>{timeline.map(m=><div key={m.clientMsgId??m.msgId} className={styles.message+' '+(m.isOwn?styles.messageOwn:'')}><div className={styles.messageMeta}><strong>{m.isOwn?'服务账号':'成员'} {m.senderPlatformUserId}</strong><time>{formatTime(m.sentAt)}</time></div><div className={styles.messageText}><p>{m.text}</p></div>{m.isOwn&&<div className={styles.messageDelivery}>{m.deliveryStatus&&<StatusTag status={m.deliveryStatus}/>}<span className={styles.meta}>{m.failCode}</span></div>}</div>)}</div>{session?.role==='admin'?<GroupComposer group={g} accounts={accounts.data??[]}/>:<div className={styles.composer}><span className={styles.meta}>只读观察员，可查看群消息与运行状态。</span></div>}</article><aside className={styles.diagnostics}><section className={styles.sideSection}><div className={styles.panelTitle}><h2>群成员</h2><span className={styles.meta}>{g.members.length} 位</span></div>{g.members.map(m=><div className={styles.memberRow} key={m.platformUserId}><span className={styles.mono}>{m.accountId??m.platformUserId}</span><span>{roleNames[m.role]}</span></div>)}{!g.members.length&&<LoadState empty description="所有服务账号已离开，或成员尚未同步。"/>}</section><section className={styles.sideSection}><div className={styles.panelTitle}><h2>最近 Agent 运行</h2><span className={styles.meta}>{runs.data?.length??0}</span></div><LoadState loading={runs.isLoading} empty={!runs.isLoading&&!runs.data?.length} description="启用自动响应后，外部消息会触发运行。"/>{runs.data?.map(run=><Link className={styles.run+' '+(run.status==='blocked'?styles.blocked:'')} key={run.id} to={'/agent-runs/'+run.id}><div className={styles.runHeader}><strong>{run.id}</strong><StatusTag status={run.status}/></div><p>{run.endReason??'正在执行'}</p></Link>)}</section>{session?.role==='admin'?<GroupActions group={g}/>:<section className={styles.sideSection}><h2>群策略</h2><p className={styles.meta}>Agent 自动响应：{g.agentEnabled?'已启用':'已关闭'}</p><p className={styles.meta}>自动移除成员：{g.autoKickEnabled?'已启用':'已关闭'}</p></section>}</aside></div>}</section>
}
