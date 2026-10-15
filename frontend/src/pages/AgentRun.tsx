import {useParams,Link} from 'react-router-dom'
import {useQuery} from '@tanstack/react-query'
import {Alert,Table,Space} from 'antd'
import {ArrowLeftOutlined} from '@ant-design/icons'
import {api} from '../api/client'
import {useSession} from '../hooks'
import type {AgentRun as Run,AgentStep} from '../types'
import StatusTag from '../components/StatusTag'
import AgentPreview from '../components/AgentPreview'
import {ErrorNotice,LoadState} from '../components/Feedback'
import styles from '../layout/MainLayout.module.scss'

const actions:Record<string,string>={get_recent_messages:'读取最近消息',send_message:'发送回复',kick_user:'移除成员',finish:'完成运行'}
function resultText(step:AgentStep){
 if(step.name==='send_message'&&step.input&&typeof step.input==='object'&&'text' in step.input&&typeof step.input.text==='string')return step.input.text
 try{const result=JSON.parse(step.resultSummary);if(Array.isArray(result.messages))return `读取 ${result.messages.length} 条消息`;if(result.ok===true)return '执行成功';if(result.deliveryStatus)return '发送状态：'+result.deliveryStatus}catch{/* Plain result summaries are valid. */}
 return step.resultSummary||'—'
}
export default function AgentRun(){
 const {id}=useParams(),session=useSession()
 const query=useQuery({queryKey:['agent-run',id],queryFn:()=>api.request<Run>('/api/agent-runs/'+id),refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const run=query.data
 return <section><div className={styles.pageHeading}><div><Link className={styles.backLink} to={run?'/groups/'+run.groupId:'/groups'}><ArrowLeftOutlined/> 返回群组</Link><div className={styles.headingTitle}><h1>Agent 运行追踪</h1>{run&&<StatusTag status={run.status}/>}</div><p className={styles.mono}>{id}</p></div><Space><span className={styles.meta}>{run?.steps?.length??0} 个执行步骤</span>{session?.role==='admin'&&<AgentPreview/>}</Space></div><ErrorNotice error={query.error}/><LoadState loading={query.isLoading}/>{run?.status==='blocked'&&<Alert showIcon type="error" title="审计阻塞，工具未执行" description="审计服务未返回明确结论。请查看下方审计步骤与错误信息。"/>}{run&&<><dl className={styles.runSummary}><dt>结束原因</dt><dd>{run.endReason==='final'?'正常完成':run.endReason??'正在执行'}</dd><dt>运行摘要</dt><dd>{run.summary==='Scripted response completed'?'历史固定回复记录（当时未启用真实 AI）':run.summary??'暂无摘要'}</dd></dl><div className={styles.tableShell}><Table size="small" rowKey="index" pagination={false} scroll={{x:800}} dataSource={(run.steps??[]).map((step,index)=>({...step,index:index+1}))} columns={[
  {title:'步骤',dataIndex:'index',width:60},{title:'操作',width:170,render:(_,s)=><><strong>{actions[s.name??'']??s.name??(s.kind==='final'?'结束运行':'协议处理')}</strong>{s.toolUseId&&<div className={styles.meta}>{s.toolUseId}</div>}</>},
  {title:'回复 / 结果',render:(_,s)=><div style={{whiteSpace:'pre-wrap',overflowWrap:'anywhere',maxWidth:800}}>{resultText(s)}</div>},{title:'状态',width:90,render:(_,s)=><StatusTag status={s.isError?'failed':'success'}/>},
  {title:'审计',dataIndex:'auditVerdict',width:100,render:v=>({pass:'通过',block:'阻止',deny:'拒绝'}[v as string]??v??'不涉及')},{title:'错误码',dataIndex:'errorCode',width:140,render:v=>v||'—'},
  {title:'详情',width:220,render:(_,s)=><div className={styles.disclosures}>{s.input!=null&&<details><summary>入参</summary><pre className={styles.raw}>{JSON.stringify(s.input,null,2)}</pre></details>}{s.kind==='protocol_error'?<details><summary>原始响应体</summary><pre className={styles.raw}>{s.rawResponse}</pre></details>:<details><summary>完整结果</summary><pre className={styles.raw}>{s.resultSummary}</pre></details>}</div>}
 ]}/></div></>}</section>
}
