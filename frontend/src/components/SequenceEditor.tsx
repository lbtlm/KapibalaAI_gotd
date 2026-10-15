import {useState} from 'react'
import {Alert,Button,Collapse,Input,InputNumber,Select,Space,Tabs} from 'antd'
import {PlusOutlined,ThunderboltOutlined,DeleteOutlined} from '@ant-design/icons'
import type {Sequence} from '../types'
import SequencePreview from './SequencePreview'
import styles from './SequenceEditor.module.scss'

type Definition=Omit<Sequence,'id'>
export type GenerateInput={prompt:string;stepCount:number}
export type GeneratedSequence={sequence:Definition;model:string}
export function parseSequence(text:string,allowIncomplete=false):Definition {
 const value=JSON.parse(text) as Definition
 if(!value||typeof value.name!=='string'||!allowIncomplete&&!value.name.trim()||!Array.isArray(value.steps)||!value.steps.length)throw new Error('填写剧本名称，并添加至少一个步骤')
 value.steps.forEach((s,i)=>{if(!s||s.index!==i+1||!['admin','member'].includes(s.accountRole)||typeof s.text!=='string'||!allowIncomplete&&!s.text.trim()||!Number.isFinite(s.delaySeconds)||s.delaySeconds<0)throw new Error('步骤须连续编号，填写角色、消息及非负延迟')})
 return {name:value.name,steps:value.steps.map(({index,accountRole,text,delaySeconds})=>({index,accountRole,text,delaySeconds}))}
}

export default function SequenceEditor({initial,onSave,pending,onGenerate}:{initial?:Sequence;onSave:(value:Definition)=>void;pending:boolean;onGenerate?:(input:GenerateInput)=>Promise<GeneratedSequence>}) {
 const [source,setSource]=useState(JSON.stringify(initial??{name:'新剧本',steps:[{index:1,accountRole:'admin',text:'欢迎加入',delaySeconds:0}]},null,2))
 const [expanded,setExpanded]=useState<string[]>(['1']),[tab,setTab]=useState('visual'),[showAI,setShowAI]=useState(false)
 const [prompt,setPrompt]=useState(''),[stepCount,setStepCount]=useState(6),[generating,setGenerating]=useState(false),[generationError,setGenerationError]=useState(''),[candidate,setCandidate]=useState<GeneratedSequence>()
 let value:Definition|undefined,error=''
 try{value=parseSequence(source,true);parseSequence(source)}catch(e){error=e instanceof Error?e.message:'JSON 格式错误'}
 const write=(v:Definition)=>setSource(JSON.stringify(v,null,2))
 const step=(i:number,patch:Partial<Sequence['steps'][number]>)=>{if(value)write({...value,steps:value.steps.map((s,n)=>n===i?{...s,...patch}:s)})}
 async function generate(){
  if(!onGenerate||generating)return
  setGenerating(true);setGenerationError('')
  try {const result=await onGenerate({prompt:prompt.trim(),stepCount});setCandidate({...result,sequence:parseSequence(JSON.stringify(result.sequence))})}
  catch(e){const code=(e as {code?:string}).code;setGenerationError(({AI_NOT_CONFIGURED:'尚未配置真实 AI，请在服务端配置模型和 API Key。',AI_TIMEOUT:'AI 生成超时，请减少步骤后重试。',AI_INVALID_SCRIPT:'AI 返回的剧本格式不合法，当前编辑内容已保留。',AI_PROVIDER_ERROR:'模型服务请求失败，请检查服务端模型配置后重试。'} as Record<string,string>)[code??'']??'生成失败，当前编辑内容已保留，请重试。')}
  finally {setGenerating(false)}
 }
 return <div className={styles.editor}>
  <div className={styles.editorHeading}><label>剧本名称<Input aria-label="剧本名称" value={value?.name??''} disabled={!value||pending} onChange={e=>write({...value!,name:e.target.value})}/></label><Button aria-label="AI 生成" icon={<ThunderboltOutlined/>} aria-expanded={showAI} onClick={()=>setShowAI(!showAI)}>AI 生成</Button></div>
  {showAI&&<div className={styles.aiPanel}>
   <label className={styles.aiPrompt}>生成要求<Input.TextArea aria-label="生成要求" maxLength={4000} autoSize={{minRows:2,maxRows:4}} placeholder="例如：生成读书会活动的测试对话，管理员介绍规则，成员提问，语气自然。" value={prompt} onChange={e=>setPrompt(e.target.value)}/></label>
   <div className={styles.aiActions}><label>步骤数<InputNumber aria-label="生成步骤数" precision={0} min={2} max={30} value={stepCount} onChange={v=>setStepCount(v??6)}/></label><Button loading={generating} disabled={!prompt.trim()||!onGenerate||pending} onClick={()=>void generate()}>生成草稿</Button></div>
   <p className={styles.note}>调用已配置的真实模型。生成结果先预览，采用后可继续编辑。</p>
   {generationError&&<Alert type="error" showIcon title={generationError}/>}
  </div>}
  <div className={styles.workspace}>
   <div className={styles.editPane}>
    <Tabs activeKey={tab} onChange={setTab} items={[
     {key:'visual',label:'步骤编辑',children:value?<>
      <div className={styles.stepToolbar}><span>{value.steps.length} 个步骤</span><Space size={4}><Button type="text" onClick={()=>setExpanded([])}>全部折叠</Button><Button type="text" onClick={()=>setExpanded(value!.steps.map(s=>String(s.index)))}>全部展开</Button></Space></div>
      <Collapse className={styles.steps} destroyOnHidden activeKey={expanded} onChange={keys=>setExpanded(Array.isArray(keys)?keys:[keys])} items={value.steps.map((s,i)=>({key:String(s.index),label:<div className={styles.stepSummary}><span className={styles.stepIndex}>{String(s.index).padStart(2,'0')}</span><strong>{s.accountRole==='admin'?'管理员':'成员'}</strong><span className={styles.excerpt}>{s.text||'尚未填写消息'}</span><span className={styles.delay}>+{s.delaySeconds}s</span></div>,children:<>
       <div className={styles.stepControls}><Select aria-label={'第'+(i+1)+'步角色'} value={s.accountRole} onChange={accountRole=>step(i,{accountRole})} options={[{value:'admin',label:'指定管理员'},{value:'member',label:'普通成员'}]}/><label>延迟<InputNumber aria-label={'第'+(i+1)+'步延迟'} min={0} value={s.delaySeconds} onChange={v=>step(i,{delaySeconds:v??0})}/>秒</label><Button type="text" danger icon={<DeleteOutlined/>} disabled={value!.steps.length===1||pending} onClick={()=>{write({...value!,steps:value!.steps.filter((_,n)=>n!==i).map((item,n)=>({...item,index:n+1}))});setExpanded([])}}>移除步骤</Button></div>
       <Input.TextArea aria-label={'第'+(i+1)+'步文本'} autoSize={{minRows:2,maxRows:6}} value={s.text} onChange={e=>step(i,{text:e.target.value})}/>
      </>}))}/>
      <Button aria-label="添加步骤" className={styles.addStep} icon={<PlusOutlined/>} disabled={pending} onClick={()=>{const index=value!.steps.length+1;write({...value!,steps:[...value!.steps,{index,accountRole:'member',text:'新消息',delaySeconds:5}]});setExpanded([String(index)])}}>添加步骤</Button>
     </>:<Alert type="error" title="请在 JSON 中修正剧本格式"/>},
     {key:'json',label:'JSON',children:<Input.TextArea className={styles.json} aria-label="剧本 JSON" value={source} onChange={e=>setSource(e.target.value)} spellCheck={false}/>}
    ]}/>
   </div>
   <div className={styles.previewPane}>
    {candidate&&<div className={styles.candidate} role="status"><strong>AI 草稿待采用</strong><span>模型 · {candidate.model}</span><Space><Button type="primary" disabled={pending} onClick={()=>{write(candidate.sequence);setCandidate(undefined);setExpanded(['1']);setTab('visual')}}>采用 AI 草稿</Button><Button onClick={()=>setCandidate(undefined)}>放弃草稿</Button></Space></div>}
    {candidate||value?<SequencePreview value={candidate?.sequence??value!}/>:<p className={styles.note}>修正 JSON 后显示预览。</p>}
   </div>
  </div>
  <div className={styles.editorFooter}><div>{error?<span role="alert" className={styles.error}>{error}</span>:<span>支持 {'{变量名}'}；每步延迟从上一条实际发出后开始计时。</span>}</div><Button type="primary" disabled={!value||!!error||!!candidate} loading={pending} onClick={()=>value&&!error&&onSave(value)}>保存剧本</Button></div>
 </div>
}
