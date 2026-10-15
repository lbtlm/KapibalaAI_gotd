import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Button,Modal,Progress,Table,Tooltip} from 'antd'
import {api} from '../api/client'
import type {Account,PreviewStep,SequenceRun} from '../types'
import {accountLabel} from './AccountTableColumns'
import VariableValues from './VariableValues'
import StatusTag from './StatusTag'
import {ErrorNotice,LoadState} from './Feedback'
import {formatTime} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function SequenceProgress({id,accounts}:{id:string;accounts:Account[]}){
 const [variables,setVariables]=useState<PreviewStep|null>(null)
 const query=useQuery({queryKey:['sequence-run',id],queryFn:()=>api.request<SequenceRun>('/api/sequence-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data,completed=run?.steps.filter(s=>['sent','skipped','failed'].includes(s.status)).length??0
 const next=run?.steps.find(s=>s.index===run.currentStepIndex),active=run?.status==='running'
 return <article className={styles.panel}><div className={styles.panelTitle}><div><h2>批次明细</h2><span className={styles.meta} title={id}>批次 {id.slice(0,8)} · 延迟从上一条实际发出后计算</span></div>{run&&<StatusTag status={run.status}/>}</div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run&&<><div className={styles.progressHeading}><span>当前第 {run.currentStepIndex} 步 · 已完成 {completed} / {run.steps.length}</span><span className={styles.meta}>{active&&next?(next.deliveryStatus? '当前消息已进入发送队列':next.scheduledAt?'下一条 '+formatTime(next.scheduledAt):'等待上一条确认发出'):'本批次已结束'}</span></div><div className={styles.progressBar}><Progress size="small" showInfo={false} percent={run.steps.length?Math.round(completed/run.steps.length*100):0} status={run.status==='failed'?'exception':run.status==='finished'?'success':'normal'}/></div><div className={styles.tableShell}><Table size="small" rowKey="index" pagination={false} tableLayout="fixed" scroll={{x:1200}} dataSource={run.steps} columns={[
  {title:'步骤',dataIndex:'index',width:56},
  {title:'发送账号',width:200,render:(_,s)=>{const a=accounts.find(a=>a.id===s.accountId);return <Tooltip title={s.accountId}><span>{a?accountLabel(a):s.accountId?'账号资料已移除':'未绑定'}<div className={styles.meta}>{s.accountRole==='admin'?'管理员':'普通成员'}</div></span></Tooltip>}},
  {title:'内容',dataIndex:'text',width:320,render:text=><Tooltip styles={{root:{maxWidth:'min(640px, 85vw)'}}} title={<pre className={styles.jsonTooltip}>{text}</pre>}><span className={styles.cellExcerpt} tabIndex={0}>{text}</span></Tooltip>},
  {title:'状态',width:110,render:(_,s)=><><StatusTag status={['sent','skipped','failed'].includes(s.status)?s.status:s.deliveryStatus??(active?s.status:'cancelled')}/>{s.failCode&&<div className={styles.checkFailed}>{s.failCode}</div>}</>},
  {title:'计划时间',dataIndex:'scheduledAt',width:170,render:formatTime},{title:'发出 / 跳过',dataIndex:'sentAt',width:170,render:formatTime},{title:'变量及来源',width:140,render:(_,s)=>Object.keys(s.resolvedVars??{}).length?<Button size="small" type="link" onClick={()=>setVariables(s)}>查看 {Object.keys(s.resolvedVars).length} 个变量</Button>:<span className={styles.meta}>无变量</span>}
 ]}/></div></>}<Modal title={'第 '+variables?.index+' 步 · 变量及来源'} open={!!variables} onCancel={()=>setVariables(null)} footer={null} width={640} style={{top:24,paddingBottom:0}} styles={{container:{maxHeight:'calc(100dvh - 48px)',display:'flex',flexDirection:'column'},header:{flexShrink:0},body:{minHeight:0,overflow:'auto'}}}>{variables&&<VariableValues values={variables.resolvedVars} sources={variables.varSources}/>}</Modal></article>
}
