import {useParams,Link} from 'react-router-dom'
import {useQuery} from '@tanstack/react-query'
import {Alert} from 'antd'
import {ArrowLeftOutlined} from '@ant-design/icons'
import {api} from '../api/client'
import type {AgentRun as Run} from '../types'
import StatusTag from '../components/StatusTag'
import {ErrorNotice,LoadState} from '../components/Feedback'
import styles from '../layout/MainLayout.module.scss'
export default function AgentRun(){
 const {id}=useParams()
 const query=useQuery({queryKey:['agent-run',id],queryFn:()=>api.request<Run>('/api/agent-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data
 return <section><div className={styles.pageHeading}><div><Link className={styles.backLink} to={run?'/groups/'+run.groupId:'/groups'}><ArrowLeftOutlined/> 返回群组</Link><div className={styles.headingTitle}><h1>Agent 运行追踪</h1>{run&&<StatusTag status={run.status}/>}</div><p className={styles.mono}>{id}</p></div><span className={styles.meta}>{run?.steps?.length??0} 个执行步骤</span></div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run?.status==='blocked'&&<Alert showIcon type="error" title="审计阻塞，工具未执行" description="审计服务未返回明确结论。请查看下方审计步骤与错误信息。"/>}{run&&<><dl className={styles.runSummary}><dt>结束原因</dt><dd className={styles.mono}>{run.endReason??'正在执行'}</dd><dt>运行摘要</dt><dd>{run.summary??'暂无摘要'}</dd></dl><div className={styles.stepList}>{(run.steps??[]).map((step,i)=><article key={i} className={styles.step+' '+(step.isError?styles.stepError:'')}><span className={styles.stepNumber}>{i+1}</span><div><div className={styles.stepHeader}><h2 aria-label={'步骤 '+(i+1)+' '+(step.name??'协议处理')}>{step.name??(step.kind==='final'?'结束运行':'协议处理')}</h2><span className={styles.stepKind}>{step.kind}</span></div><p className={styles.stepResult}>{step.resultSummary}</p><div className={styles.stepMeta}>{step.toolUseId&&<span>调用 <code>{step.toolUseId}</code></span>}<span>审计 <code>{step.auditVerdict??'未审计'}</code></span>{step.errorCode&&<span className={styles.errorCode}>错误 <code className={styles.errorCode}>{step.errorCode}</code></span>}</div><div className={styles.disclosures}>{step.input!==null&&<details><summary>入参</summary><pre className={styles.raw}>{JSON.stringify(step.input,null,2)}</pre></details>}{step.kind==='protocol_error'&&<details><summary>原始响应体</summary><pre className={styles.raw}>{step.rawResponse}</pre></details>}</div></div></article>)}</div><LoadState empty={!run.steps?.length} description="Agent 还没有完成第一个步骤，执行后会自动更新。"/></>}</section>
}
