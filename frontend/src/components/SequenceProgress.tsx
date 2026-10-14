import {useQuery} from '@tanstack/react-query'
import {Progress,Table} from 'antd'
import {api} from '../api/client'
import type {SequenceRun} from '../types'
import StatusTag from './StatusTag'
import {ErrorNotice,LoadState} from './Feedback'
import {formatTime} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function SequenceProgress({id}:{id:string}) {
 const query=useQuery({queryKey:['sequence-run',id],queryFn:()=>api.request<SequenceRun>('/api/sequence-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data,completed=run?.steps.filter(s=>['sent','skipped','failed'].includes(s.status)).length??0
 return <article className={styles.panel}><div className={styles.panelTitle}><h2>运行进度 · {id}</h2>{run&&<StatusTag status={run.status}/>}</div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run&&<><p>当前第 {run.currentStepIndex} 步 · 已完成 {completed} / {run.steps.length}</p><Progress percent={run.steps.length?Math.round(completed/run.steps.length*100):0} status={run.status==='failed'?'exception':run.status==='finished'?'success':'normal'}/><Table rowKey="index" pagination={false} dataSource={run.steps} columns={[{title:'步骤',dataIndex:'index'},{title:'状态',dataIndex:'status',render:(s:string)=><StatusTag status={s}/>},{title:'计划时间',dataIndex:'scheduledAt',render:formatTime},{title:'发出 / 跳过',dataIndex:'sentAt',render:formatTime},{title:'变量及来源',render:(_,s)=><pre className={styles.raw}>{JSON.stringify({values:s.resolvedVars,sources:s.varSources},null,2)}</pre>}]}/></>}</article>
}

