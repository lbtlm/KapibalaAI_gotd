import {useState} from 'react'
import {Input} from 'antd'
import type {Sequence} from '../types'
import {variableKeys} from './sequenceDraft'
import styles from './SequenceEditor.module.scss'

export default function SequencePreview({value,editableVariables=true,actors={}}:{value:Omit<Sequence,'id'>;editableVariables?:boolean;actors?:Record<number,string>}){
 const [vars,setVars]=useState<Record<string,string>>({})
 const keys=variableKeys(value)
 let elapsed=0
 return <section className={styles.preview} aria-label="消息预览">
  <div className={styles.previewHeading}><h2>消息预览</h2><span>{value.steps.length} 条消息</span></div>
  <p className={styles.note}>按角色预览对话；实际账号在群内分配，时间为不含网络等待的估算。</p>
  {editableVariables&&!!keys.length&&<details className={styles.variables} open><summary>预览变量 · {keys.length} 项</summary>{keys.map(key=><label key={key}>{'{'+key+'}'}<Input aria-label={'预览变量 '+key} placeholder="仅用于预览" value={vars[key]??''} onChange={e=>setVars({...vars,[key]:e.target.value})}/></label>)}</details>}
  <ol className={styles.messages}>{value.steps.map(step=>{elapsed+=step.delaySeconds;return <li key={step.index} className={step.accountRole==='admin'?styles.adminMessage:styles.memberMessage}>
   <div className={styles.messageMeta}><strong>{step.accountRole==='admin'?'管理员':'普通成员'}</strong><span>预计 +{Number(elapsed.toFixed(2))} 秒</span></div>
   <p>{step.text.replace(/\{([A-Za-z0-9_]+)\}/g,(all,key:string)=>vars[key]||all)||'尚未填写消息'}</p><small>第 {step.index} 步{actors[step.index]&&' · '+actors[step.index]}</small>
  </li>})}</ol>
 </section>
}
