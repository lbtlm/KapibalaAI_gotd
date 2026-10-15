import {useRef,useState} from 'react'
import {Link} from 'react-router-dom'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Table,Modal,Space} from 'antd'
import {api} from '../api/client'
import type {Account,Group,Sequence,SequenceInput,PreviewStep} from '../types'
import {accountLabel} from './AccountTableColumns'
import {ErrorNotice} from './Feedback'
import SequenceProgress from './SequenceProgress'
import VariableValues from './VariableValues'

function stringMap(value:unknown):value is Record<string,string>{return !!value&&typeof value==='object'&&!Array.isArray(value)&&Object.values(value).every(v=>typeof v==='string')}
export default function GroupSequence({group,accounts,writable}:{group:Group;accounts:Account[];writable:boolean}) {
 const cache=useQueryClient(),[form]=Form.useForm(),revision=useRef(0)
 const [error,setError]=useState<unknown>(),[preview,setPreview]=useState<{input:SequenceInput;steps:PreviewStep[]}|null>(null),[lastRun,setLastRun]=useState('')
 const definitions=useQuery({queryKey:['sequences'],queryFn:()=>api.request<Sequence[]>('/api/sequences')})
 const inspect=useMutation({mutationFn:({input}:{input:SequenceInput;revision:number})=>api.request<{steps:PreviewStep[]}>('/api/groups/'+group.id+'/sequence-runs/preview','POST',input),onSuccess:(r,v)=>{if(v.revision===revision.current)setPreview({input:v.input,steps:r.steps})}})
 const start=useMutation({mutationFn:(input:SequenceInput)=>api.request<{runId:string}>('/api/groups/'+group.id+'/sequence-runs','POST',input),onSuccess:r=>{setLastRun(r.runId);setPreview(null);cache.invalidateQueries({queryKey:['group',group.id]})}})
 const saved=group.sequenceConfig,members=new Set(group.members.filter(m=>m.role==='member').map(m=>m.accountId))
 const label=(id:string|null|undefined)=>{const a=accounts.find(a=>a.id===id);return a?accountLabel(a):id??'未分配'}
 function preflight(v:{sequenceId:string;vars:string;stepVars:string;memberAccountIds:string[]}) {
  setError(undefined);setPreview(null);const current=++revision.current
  try {const vars:unknown=JSON.parse(v.vars),overrides:unknown=JSON.parse(v.stepVars);if(!stringMap(vars)||!overrides||typeof overrides!=='object'||Array.isArray(overrides)||!Object.entries(overrides).every(([k,v])=>/^\d+$/.test(k)&&stringMap(v)))throw new Error('变量必须为字符串值的 JSON 对象');inspect.mutate({revision:current,input:{sequenceId:v.sequenceId,memberAccountIds:v.memberAccountIds,vars,stepVars:overrides as SequenceInput['stepVars']}})}catch(e){setError(e)}
 }
 return <section><Space style={{marginBottom:16}}><h2>本群剧本</h2><Link to="/sequences">管理剧本库</Link></Space><p>管理员：{label(group.adminAccountId)}。普通成员按所选顺序轮流发送。</p>
  <ErrorNotice error={error??definitions.error??inspect.error??start.error}/>
  {writable&&<Form form={form} layout="vertical" initialValues={{sequenceId:saved?.sequenceId,memberAccountIds:saved?.memberAccountIds??accounts.filter(a=>members.has(a.id)).map(a=>a.id),vars:JSON.stringify(saved?.vars??{},null,2),stepVars:JSON.stringify(saved?.stepVars??{},null,2)}} onValuesChange={()=>{revision.current++;setPreview(null)}} onFinish={preflight}>
   <Form.Item label="选择剧本" name="sequenceId" rules={[{required:true,message:'选择剧本'}]}><Select options={definitions.data?.map(s=>({value:s.id,label:s.name}))}/></Form.Item>
   <Form.Item label="参与的普通成员" name="memberAccountIds"><Select mode="multiple" options={accounts.filter(a=>members.has(a.id)).map(a=>({value:a.id,label:accountLabel(a)}))}/></Form.Item>
   <Space align="start" style={{width:'100%'}}><Form.Item label="默认变量 vars" name="vars"><Input.TextArea rows={3} aria-label="默认变量 vars"/></Form.Item><Form.Item label="逐步覆盖 stepVars" name="stepVars"><Input.TextArea rows={3} aria-label="逐步覆盖 stepVars"/></Form.Item></Space>
   <Button type="primary" htmlType="submit" disabled={group.status!=='active'||!!group.activeSequenceRunId} loading={inspect.isPending}>预览并启动</Button>
  </Form>}
  {(group.activeSequenceRunId||lastRun)&&<SequenceProgress id={group.activeSequenceRunId||lastRun}/>}
  <Modal width={1000} title="剧本执行预览" open={!!preview} onCancel={()=>setPreview(null)} footer={<Button type="primary" disabled={group.status!=='active'||!!group.activeSequenceRunId} loading={start.isPending} onClick={()=>preview&&start.mutate(preview.input)}>确认启动</Button>}><ErrorNotice error={start.error}/><Table rowKey="index" size="small" pagination={false} dataSource={preview?.steps} columns={[{title:'步骤',dataIndex:'index'},{title:'角色',dataIndex:'accountRole',render:v=>v==='admin'?'管理员':'普通成员'},{title:'实际账号',dataIndex:'accountId',render:label},{title:'延迟',dataIndex:'delaySeconds',render:v=>v+' 秒'},{title:'最终文本',dataIndex:'text'},{title:'变量',render:(_,s)=><VariableValues values={s.resolvedVars} sources={s.varSources}/>} ]}/></Modal>
 </section>
}
