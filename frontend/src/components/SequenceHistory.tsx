import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Button,Space,Table,Tooltip} from 'antd'
import {api} from '../api/client'
import type {Account,SequenceBatchPage} from '../types'
import {formatTime} from '../hooks'
import {ErrorNotice,LoadState} from './Feedback'
import StatusTag from './StatusTag'
import SequenceProgress from './SequenceProgress'
import styles from '../layout/MainLayout.module.scss'

export default function SequenceHistory({groupId,accounts,initialRunId}:{groupId:string;accounts:Account[];initialRunId?:string|null}){
 const [cursors,setCursors]=useState<string[]>(['']),[selected,setSelected]=useState(initialRunId??'')
 const before=cursors.at(-1)??''
 const query=useQuery({queryKey:['sequence-history',groupId,before],queryFn:()=>api.request<SequenceBatchPage>('/api/groups/'+groupId+'/sequence-runs?limit=10'+(before?'&before='+encodeURIComponent(before):'')),refetchInterval:2000})
 const current=selected||query.data?.items[0]?.id
 return <section className={styles.historyPanel}><div className={styles.panelTitle}><div><h2>剧本发送记录</h2><p className={styles.meta}>每次启动都是独立批次。选择批次查看账号、消息和实际发送状态。</p></div><Space><Button disabled={cursors.length===1} onClick={()=>setCursors(c=>c.slice(0,-1))}>较新批次</Button><Button disabled={!query.data?.nextCursor} onClick={()=>query.data?.nextCursor&&setCursors(c=>[...c,query.data!.nextCursor!])}>较早批次</Button></Space></div>
  <ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>
  <div className={styles.tableShell}><Table size="small" rowKey="id" pagination={false} dataSource={query.data?.items??[]} locale={{emptyText:'尚无发送记录，请先在“剧本与账号”配置并启动。'}} columns={[
   {title:'启动时间 / 批次',width:200,render:(_,b)=><Tooltip title={b.id}><span>{formatTime(b.createdAt)}<div className={styles.meta}>{b.id.slice(0,8)}</div></span></Tooltip>},
   {title:'剧本',dataIndex:'sequenceName'},{title:'状态',dataIndex:'status',width:110,render:v=><StatusTag status={v}/>},{title:'进度',width:100,render:(_,b)=>`${b.completedSteps} / ${b.totalSteps}`},
   {title:'操作',width:110,render:(_,b)=><Button type="link" onClick={()=>setSelected(b.id)} disabled={b.id===current}>{b.id===current?'正在查看':'查看明细'}</Button>}
  ]}/></div>{current&&<SequenceProgress key={current} id={current} accounts={accounts}/>}
 </section>
}
