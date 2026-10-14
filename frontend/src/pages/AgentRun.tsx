import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Alert, Descriptions, Tag } from 'antd'
import { api } from '../api/client'
import type { AgentRun as Run } from '../types'
import StatusTag from '../components/StatusTag'
import { ErrorNotice, LoadState } from '../components/Feedback'
import styles from '../layout/MainLayout.module.scss'
export default function AgentRun() {
 const {id} = useParams()
 const query=useQuery({queryKey:['agent-run',id],queryFn:()=>api.request<Run>('/api/agent-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data
 return <section><div className={styles.pageHeading}><div><Link to={run?'/groups/'+run.groupId:'/groups'}>← 返回群组</Link><h1>Agent 运行追踪</h1><p>{id}</p></div>{run&&<StatusTag status={run.status}/>}</div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run?.status==='blocked'&&<Alert showIcon type="error" title="审计阻塞，工具未执行" description="审计服务未返回明确结论。请查看下方审计步骤与错误信息。"/>}{run&&<><p>结束原因：{run.endReason??'正在执行'} · {run.summary??'暂无摘要'}</p>{(run.steps??[]).map((step,i)=><article key={i} className={styles.panel}><div className={styles.panelTitle}><h2>步骤 {i+1} · {step.name??'协议处理'}</h2><Tag color={step.isError?'red':'default'}>{step.kind}</Tag></div><Descriptions column={2} items={[{key:'tool',label:'工具调用 ID',children:step.toolUseId??'—'},{key:'audit',label:'审计结论',children:step.auditVerdict??'—'},{key:'result',label:'结果摘要',children:step.resultSummary},{key:'error',label:'错误码',children:step.errorCode??'—'}]}/>{step.input!==null&&<><p>入参</p><pre className={styles.raw}>{JSON.stringify(step.input,null,2)}</pre></>}{step.kind==='protocol_error'&&<details open><summary>原始响应体</summary><pre className={styles.raw}>{step.rawResponse}</pre></details>}</article>)}</>}</section>
}

