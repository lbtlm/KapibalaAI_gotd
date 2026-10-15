import {useRef,useState} from 'react'
import {Link} from 'react-router-dom'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Table,Modal,message} from 'antd'
import {api,ApiError} from '../api/client'
import type {Account,Group,Sequence,SequenceInput,PreviewStep} from '../types'
import {accountLabel} from './AccountTableColumns'
import {ErrorNotice} from './Feedback'
import SequencePreview from './SequencePreview'
import {parseStepVars,resolveDraft,variableKeys} from './sequenceDraft'
import styles from '../layout/MainLayout.module.scss'

export function sequenceError(error:unknown){
 const e=error as ApiError
 if(e.code==='UNRESOLVED_PLACEHOLDER')return `第 ${e.details?.stepIndex??'?'} 步缺少变量 ${e.details?.key??''}，请填写同名变量后重新检查。`
 return ({MARKETING_PAUSED:'本群已暂停营销，请先恢复营销后再启动剧本。',GROUP_IN_USE:'群组操作正在进行，请等待完成后再启动。',ADMIN_REQUIRED:'请先在右侧群成员中指定管理员，再检查剧本。',MEMBER_REQUIRED:'此剧本需要普通成员，请至少选择一个参与账号。',MEMBER_NOT_IN_GROUP:'所选账号已不在本群，请重新选择参与成员。',ACCOUNT_NOT_ONLINE:'发送账号尚未在线，请到账号管理连接并检测后重试。',SEQUENCE_ALREADY_RUNNING:'本群已有剧本运行，请等待完成或停止后再启动。'} as Record<string,string>)[e.code]??`检查未通过：${e.code??e.message??'请求失败'}。请检查剧本、成员和变量后重试。`
}
const variableNames:Record<string,string>={event:'活动名称',time:'活动时间',location:'活动地点'}
const examples:Record<string,string>={event:'例如：周末读书会',time:'例如：周六 15:00',location:'例如：线上群内'}
export function SequenceSetup({group,accounts,definitions,pending,onPreview,onChange}:{group:Group;accounts:Account[];definitions:Sequence[];pending:boolean;onPreview:(input:SequenceInput)=>void;onChange:()=>void}){
 const [form]=Form.useForm(),saved=group.sequenceConfig,[notice,holder]=message.useMessage()
 const sequenceId=Form.useWatch('sequenceId',form),vars:Record<string,string>=Form.useWatch(values=>values.vars,{form,preserve:true})??saved?.vars??{},memberIds:string[]=Form.useWatch('memberAccountIds',form)??saved?.memberAccountIds??group.members.filter(m=>m.role==='member'&&m.accountId).map(m=>m.accountId!),stepText=Form.useWatch('stepVars',form)??JSON.stringify(saved?.stepVars??{},null,2)
 const definition=definitions.find(s=>s.id===(sequenceId??saved?.sequenceId)),members=new Set(group.members.filter(m=>m.role==='member').map(m=>m.accountId))
 const label=(id:string|null|undefined)=>{const a=accounts.find(a=>a.id===id);return a?accountLabel(a):id??'未分配管理员'}
 let overrides:SequenceInput['stepVars']={},parseError=''
 try{overrides=parseStepVars(stepText)}catch{parseError='逐步变量格式错误，请按示例填写 JSON，例如 {"2":{"event":"自由交流"}}。'}
 const draft=definition?resolveDraft(definition,vars,overrides):null,actors:Record<number,string>={};let memberIndex=0
 for(const step of definition?.steps??[])actors[step.index]=step.accountRole==='admin'?label(group.adminAccountId):memberIds.length?label(memberIds[memberIndex++%memberIds.length]):'尚未选择普通成员'
 function preflight(){
  if(!definition)return
  if(parseError){notice.error(parseError,4);return}
  if(draft?.missing.length){const first=draft.missing[0];form.setFields([{name:['vars',first.key],errors:[`第 ${first.index} 步需要填写 ${first.key}`]}]);notice.error(`请填写变量 ${first.key}，它用于第 ${first.index} 步。`,4);return}
  if(definition.steps.some(s=>s.accountRole==='admin')&&!group.adminAccountId){notice.error('请先在右侧群成员中指定管理员。',4);return}
  if(definition.steps.some(s=>s.accountRole==='member')&&!memberIds.length){form.setFields([{name:'memberAccountIds',errors:['至少选择一个普通成员用于发送消息']}]);return}
  onPreview({sequenceId:definition.id,memberAccountIds:memberIds,vars,stepVars:overrides})
 }
 return <div className={styles.sequenceSetup}>{holder}<div><h2>1. 配置本群剧本</h2><p className={styles.meta}>选择剧本和发送账号，填写变量，再检查最终消息。</p>
  <Form form={form} layout="vertical" initialValues={{sequenceId:saved?.sequenceId,vars:saved?.vars??{},memberAccountIds:memberIds,stepVars:stepText}} onValuesChange={onChange} onFinish={preflight}>
   <Form.Item label="选择剧本" name="sequenceId" rules={[{required:true,message:'请选择一个剧本；还没有剧本时先前往剧本库创建。'}]}><Select placeholder="选择一个已保存的剧本" options={definitions.map(s=>({value:s.id,label:s.name}))}/></Form.Item>
   <p className={styles.meta}>管理员发送账号：{label(group.adminAccountId)}</p>
   <Form.Item label="参与发送的普通成员" name="memberAccountIds" extra="按所选顺序轮流发送“普通成员”步骤；管理员步骤由上述管理员发送。"><Select mode="multiple" placeholder="选择本群普通成员" options={accounts.filter(a=>members.has(a.id)).map(a=>({value:a.id,label:accountLabel(a)}))}/></Form.Item>
   {!!definition&&<div className={styles.scriptVariables}><h2>填写消息变量</h2>{variableKeys(definition).length?variableKeys(definition).map(key=><Form.Item key={key} label={variableNames[key]?`${variableNames[key]}（${key}）`:key} name={['vars',key]} extra={'替换消息中的 {'+key+'}'}><Input placeholder={examples[key]??'输入替换文字'}/></Form.Item>):<p className={styles.meta}>此剧本没有变量，可直接检查预览。</p>}</div>}
   <details className={styles.advancedVariables}><summary>高级：在指定步骤更换变量</summary><p className={styles.meta}>例如 {'{"2":{"event":"自由交流"}}'}，从第 2 步起更换活动名称，后续步骤继续使用新值。</p><Form.Item name="stepVars" label="逐步变量 JSON" validateStatus={parseError?'error':undefined} help={parseError||undefined}><Input.TextArea rows={3} spellCheck={false}/></Form.Item></details>
   <Button type="primary" htmlType="submit" disabled={group.status!=='active'||group.marketingPaused||!!group.activeSequenceRunId} loading={pending}>检查并预览</Button>
   <p className={styles.meta}>{group.marketingPaused?'营销已暂停。恢复后可沿用此配置，从第 1 步重新启动。':'检查不会发送消息；确认最终预览后才启动发送。'}</p>
  </Form>
 </div><div className={styles.sequencePreview}><h2>2. 查看对话效果</h2>{draft?<SequencePreview value={draft.value} editableVariables={false} actors={actors}/>:<p className={styles.meta}>先选择剧本，这里将展示每一步的内容、角色、账号和预计时间。</p>}</div></div>
}

export default function GroupSequence({group,accounts,writable,onStarted}:{group:Group;accounts:Account[];writable:boolean;onStarted:(id:string)=>void}){
 const cache=useQueryClient(),revision=useRef(0),[notice,holder]=message.useMessage()
 const [preview,setPreview]=useState<{input:SequenceInput;steps:PreviewStep[]}|null>(null)
 const definitions=useQuery({queryKey:['sequences'],queryFn:()=>api.request<Sequence[]>('/api/sequences')})
 const inspect=useMutation({mutationFn:({input}:{input:SequenceInput;revision:number})=>api.request<{steps:PreviewStep[]}>('/api/groups/'+group.id+'/sequence-runs/preview','POST',input),onSuccess:(r,v)=>{if(v.revision===revision.current)setPreview({input:v.input,steps:r.steps})},onError:(e,v)=>{if(v.revision===revision.current)notice.error(sequenceError(e),5)}})
 const start=useMutation({mutationFn:(input:SequenceInput)=>api.request<{runId:string}>('/api/groups/'+group.id+'/sequence-runs','POST',input),onSuccess:r=>{setPreview(null);cache.invalidateQueries({queryKey:['group',group.id]});cache.invalidateQueries({queryKey:['sequence-history',group.id]});onStarted(r.runId)},onError:e=>notice.error(sequenceError(e),5)})
 const label=(id:string|null|undefined)=>{const a=accounts.find(a=>a.id===id);return a?accountLabel(a):id??'未分配'}
 return <section>{holder}<div className={styles.panelTitle}><h2>本群剧本</h2><Link to="/sequences">创建 / 编辑剧本库</Link></div><ErrorNotice error={definitions.error}/>
  {writable?<SequenceSetup group={group} accounts={accounts} definitions={definitions.data??[]} pending={inspect.isPending} onChange={()=>{revision.current++;setPreview(null)}} onPreview={input=>{setPreview(null);inspect.mutate({input,revision:++revision.current})}}/>:<p>请管理员配置本群剧本与发送账号。</p>}
  <Modal width={1000} style={{top:24,paddingBottom:0}} styles={{container:{maxHeight:'calc(100dvh - 48px)',display:'flex',flexDirection:'column'},header:{flexShrink:0},body:{minHeight:0,overflow:'auto'},footer:{flexShrink:0}}} title="3. 确认最终发送内容" open={!!preview} onCancel={()=>setPreview(null)} footer={<Button type="primary" disabled={group.status!=='active'||group.marketingPaused||!!group.activeSequenceRunId} loading={start.isPending} onClick={()=>preview&&start.mutate(preview.input)}>确认启动发送</Button>}><p>以下是后台校验后的实际账号与消息。确认后将按顺序向本群发送。</p><Table rowKey="index" size="small" pagination={false} scroll={{x:650}} dataSource={preview?.steps} columns={[{title:'步骤',dataIndex:'index',width:60},{title:'角色',dataIndex:'accountRole',width:100,render:v=>v==='admin'?'管理员':'普通成员'},{title:'实际账号',dataIndex:'accountId',render:label},{title:'等待',dataIndex:'delaySeconds',width:80,render:v=>v+' 秒'},{title:'最终消息',dataIndex:'text',render:v=><span style={{whiteSpace:'pre-wrap'}}>{v}</span>}]}/></Modal>
 </section>
}
