import {useState} from 'react'
import {Alert,Button,Input,InputNumber,Select,Space,Tabs} from 'antd'
import type {Sequence} from '../types'

export function parseSequence(text:string,allowIncomplete=false):Omit<Sequence,'id'> {
 const value=JSON.parse(text) as Omit<Sequence,'id'>
 if(!value||typeof value.name!=='string'||!allowIncomplete&&!value.name.trim()||!Array.isArray(value.steps)||!value.steps.length)throw new Error('填写剧本名称，并添加至少一个步骤')
 value.steps.forEach((s,i)=>{if(!s||s.index!==i+1||!['admin','member'].includes(s.accountRole)||typeof s.text!=='string'||!allowIncomplete&&!s.text.trim()||!Number.isFinite(s.delaySeconds)||s.delaySeconds<0)throw new Error('步骤须连续编号，填写角色、消息及非负延迟')})
 return value
}

export default function SequenceEditor({initial,onSave,pending}:{initial?:Sequence;onSave:(value:Omit<Sequence,'id'>)=>void;pending:boolean}) {
 const [source,setSource]=useState(JSON.stringify(initial??{name:'新剧本',steps:[{index:1,accountRole:'admin',text:'欢迎加入',delaySeconds:0}]},null,2))
 let value:Omit<Sequence,'id'>|undefined,error=''
 try{value=parseSequence(source,true);parseSequence(source)}catch(e){error=e instanceof Error?e.message:'JSON 格式错误'}
 const write=(v:Omit<Sequence,'id'>)=>setSource(JSON.stringify(v,null,2))
 const step=(i:number,patch:Partial<Sequence['steps'][number]>)=>{if(value)write({...value,steps:value.steps.map((s,n)=>n===i?{...s,...patch}:s)})}
 return <><Tabs items={[
  {key:'visual',label:'可视化编辑',children:value?<div>
   <label>剧本名称<Input aria-label="剧本名称" value={value.name} onChange={e=>write({...value!,name:e.target.value})}/></label>
   {value.steps.map((s,i)=><div key={i} style={{padding:'16px 0',borderBottom:'1px solid #dfe4e8'}}>
    <Space wrap><strong>第 {i+1} 步</strong><Select aria-label={'第'+(i+1)+'步角色'} value={s.accountRole} onChange={accountRole=>step(i,{accountRole})} options={[{value:'admin',label:'指定管理员'},{value:'member',label:'普通成员'}]}/><label>延迟 <InputNumber min={0} value={s.delaySeconds} onChange={v=>step(i,{delaySeconds:v??0})}/> 秒</label><Button disabled={value!.steps.length===1} onClick={()=>write({...value!,steps:value!.steps.filter((_,n)=>n!==i).map((s,n)=>({...s,index:n+1}))})}>移除步骤</Button></Space>
    <Input.TextArea aria-label={'第'+(i+1)+'步文本'} rows={3} style={{marginTop:8}} value={s.text} onChange={e=>step(i,{text:e.target.value})}/>
   </div>)}
   <Button style={{marginTop:12}} onClick={()=>write({...value!,steps:[...value!.steps,{index:value!.steps.length+1,accountRole:'member',text:'新消息',delaySeconds:5}]})}>添加步骤</Button>
  </div>:<Alert type="error" title={error} description="请在 JSON 中修正后继续可视化编辑。"/>},
  {key:'json',label:'JSON',children:<Input.TextArea aria-label="剧本 JSON" rows={16} value={source} onChange={e=>setSource(e.target.value)}/>}
 ]}/><p>可使用 {'{变量名}'}；每步延迟从上一条实际发出后开始计时。</p>{error&&<Alert type="error" title={error}/>}<Button type="primary" disabled={!value||!!error} loading={pending} onClick={()=>value&&!error&&onSave(value)}>保存剧本</Button></>
}
