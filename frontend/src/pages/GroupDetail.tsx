import {useParams,Link} from 'react-router-dom'
import {useInfiniteQuery,useQuery} from '@tanstack/react-query'
import {Button,Table,Tag,Alert,Space} from 'antd'
import {api} from '../api/client'
import type {Group,Message,MessagePage,AgentRun,Account} from '../types'
import {formatTime,useSession} from '../hooks'
import StatusTag from '../components/StatusTag'
import {ErrorNotice,LoadState} from '../components/Feedback'
import GroupActions from '../components/GroupActions'
import styles from '../layout/MainLayout.module.scss'
const roleNames={creator:'群主',admin:'管理员',member:'成员'}
export default function GroupDetail() {
 const {id}=useParams(),session=useSession()
 const group=useQuery({queryKey:['group',id],queryFn:()=>api.request<Group>('/api/groups/'+id)})
 const accounts=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const runs=useQuery({queryKey:['agent-runs',id],queryFn:()=>api.request<AgentRun[]>('/api/groups/'+id+'/agent-runs')})
 const messages=useInfiniteQuery({queryKey:['messages',id],initialPageParam:null as string|null,queryFn:({pageParam})=>api.request<MessagePage>('/api/groups/'+id+'/messages?limit=50'+(pageParam?'&before='+encodeURIComponent(pageParam):'')),getNextPageParam:last=>last.nextCursor??undefined})
 const unique=new Map<string,Message>()
 for(const page of messages.data?.pages??[])for(const message of page.items) {const key=message.clientMsgId??message.msgId;if(key&&!unique.has(key))unique.set(key,message)}
 const timeline=[...unique.values()].sort((a,b)=>a.sentAt.localeCompare(b.sentAt)||(a.msgId??a.clientMsgId??'').localeCompare(b.msgId??b.clientMsgId??''))
 const g=group.data
 return <section><div className={styles.pageHeading}><div><Link to="/groups">← 群组列表</Link><h1>群消息工作区</h1><p>{id} · 网关 {g?.gatewayGroupId??'—'}</p></div><Space>{g&&<StatusTag status={g.status}/>}<Link to={'/sequences?group='+id}>序列运行 →</Link></Space></div><ErrorNotice error={group.error??messages.error??runs.error??accounts.error}/><LoadState loading={group.isLoading}/>{g&&<div className={styles.groupGrid}><div>{session?.role==='admin'&&<GroupActions group={g} accounts={accounts.data??[]}/>}<article className={styles.panel}><div className={styles.panelTitle}><h2>消息时间线</h2><Button aria-label="加载更早" onClick={()=>messages.fetchNextPage()} disabled={!messages.hasNextPage} loading={messages.isFetchingNextPage}>加载更早</Button></div><LoadState loading={messages.isLoading} empty={!messages.isLoading&&!timeline.length}/><div className={styles.timeline}>{timeline.map(m=><div key={m.clientMsgId??m.msgId} className={styles.message+' '+(m.isOwn?styles.messageOwn:'')}><div className={styles.messageMeta}><strong>{m.isOwn?'服务账号':'成员'} · {m.senderPlatformUserId}</strong><time>{formatTime(m.sentAt)}</time>{m.isOwn&&m.deliveryStatus&&<StatusTag status={m.deliveryStatus}/>}</div><p>{m.text}</p>{m.failCode&&<Tag color="red">{m.failCode}</Tag>}</div>)}</div></article></div><aside><article className={styles.panel}><div className={styles.panelTitle}><h2>群成员</h2><Tag>{g.members.length}</Tag></div><Table size="small" rowKey="platformUserId" dataSource={g.members} pagination={false} columns={[{title:'账号 / 用户',render:(_,m)=><span>{m.accountId??m.platformUserId}</span>},{title:'角色',dataIndex:'role',render:(role:keyof typeof roleNames)=>roleNames[role]}]}/></article><article className={styles.panel}><h2>最近 Agent 运行</h2><LoadState loading={runs.isLoading} empty={!runs.isLoading&&!runs.data?.length}/>{runs.data?.map(run=><Link className={styles.run+' '+(run.status==='blocked'?styles.blocked:'')} key={run.id} to={'/agent-runs/'+run.id}><strong>{run.id}</strong><StatusTag status={run.status}/><p>{run.endReason??'正在执行'}</p></Link>)}{g.agentEnabled&&<Alert type="info" title="自动响应已启用"/>}</article></aside></div>}</section>
}

