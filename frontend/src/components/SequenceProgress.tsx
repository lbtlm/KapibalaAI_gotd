import {useQuery} from '@tanstack/react-query'
import {Progress,Table} from 'antd'
import {api} from '../api/client'
import type {SequenceRun} from '../types'
import VariableValues from './VariableValues'
import StatusTag from './StatusTag'
import {ErrorNotice,LoadState} from './Feedback'
import {formatTime} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function SequenceProgress({id}:{id:string}){
 const query=useQuery({queryKey:['sequence-run',id],queryFn:()=>api.request<SequenceRun>('/api/sequence-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data,completed=run?.steps.filter(s=>['sent','skipped','failed'].includes(s.status)).length??0
 return <article className={styles.panel}><div className={styles.panelTitle}><div><h2>运行进度</h2><span className={styles.mono}>{id}</span></div>{run&&<StatusTag status={run.status}/>}</div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run&&<><div className={styles.progressHeading}><span>当前第 {run.currentStepIndex} 步 · 已完成 {completed} / {run.steps.length}</span><span className={styles.meta}>{run.steps.find(s=>s.status==='pending')?.scheduledAt?'下一条 '+formatTime(run.steps.find(s=>s.status==='pending')!.scheduledAt):'没有待发送步骤'}</span></div><div className={styles.progressBar}><Progress size="small" showInfo={false} percent={run.steps.length?Math.round(completed/run.steps.length*100):0} status={run.status==='failed'?'exception':run.status==='finished'?'success':'normal'}/></div><div className={styles.tableShell}><Table size="small" rowKey="index" pagination={false} dataSource={run.steps} columns={[{title:'步骤',dataIndex:'index',width:56},{title:'发送账号',render:(_,s)=><span>{s.accountRole==='admin'?'管理员':'普通成员'}<div className={styles.meta}>{s.accountId??'未绑定'}</div></span>},{title:'内容',dataIndex:'text'},{title:'状态',dataIndex:'status',render:(s:string)=><StatusTag status={s}/>},{title:'计划时间',dataIndex:'scheduledAt',render:formatTime},{title:'发出 / 跳过',dataIndex:'sentAt',render:formatTime},{title:'变量及来源',render:(_,s)=><details><summary>查看 {Object.keys(s.resolvedVars??{}).length} 个变量</summary><VariableValues values={s.resolvedVars} sources={s.varSources}/></details>}]}/></div></>}</article>
}
