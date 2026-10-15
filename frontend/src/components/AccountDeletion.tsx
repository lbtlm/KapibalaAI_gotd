import {useRef,useState} from 'react'
import {App,Button,Modal,Space,Table,Tooltip} from 'antd'
import {accountErrorMessage} from './AccountFeedback'
import type {Account} from '../types'

export const accountDeletionDisabledReason = (status:string):string|undefined =>
 ['idle','disconnected','session_expired','suspended'].includes(status)?undefined:['online','rate_limited','connecting'].includes(status)?'请先将账号离线，再删除。':'当前状态暂不可删除，请刷新后重试。'

export function AccountDeleteButton({writable,status,busy,onClick}:{writable:boolean;status:string;busy:boolean;onClick:()=>void}){
 if(!writable)return null
 const reason=accountDeletionDisabledReason(status)
 return <Tooltip title={reason}><span><Button type="text" danger aria-label="删除账号" disabled={busy||!!reason} onClick={onClick}>删除</Button></span></Tooltip>
}

// Single and bulk deletion share the same confirmation and guarded API path.
export function DeleteAccountDialog({accounts,onDelete,onComplete,onCancel}:{accounts:Account[];onDelete:(id:string)=>Promise<unknown>;onComplete:(ids:string[])=>Promise<unknown>;onCancel:()=>void}){
 const {message}=App.useApp(),locked=useRef(false),[pending,setPending]=useState(false),[results,setResults]=useState<Record<string,string>>({})
 const eligible=accounts.filter(a=>!accountDeletionDisabledReason(a.status))
 async function confirm(){
  if(locked.current||!eligible.length)return
  locked.current=true;setPending(true);setResults({})
  const deleted:string[]=[],failures:string[]=[]
  let next=0
  try{
   await Promise.all(Array.from({length:Math.min(3,eligible.length)},async()=>{
    while(next<eligible.length){
     const account=eligible[next++]
     try{await onDelete(account.id);deleted.push(account.id);setResults(current=>({...current,[account.id]:'已删除'}))}
     catch(error){const text=accountErrorMessage(error);failures.push(text);setResults(current=>({...current,[account.id]:text}))}
    }
   }))
   if(failures.length)void message.error({content:eligible.length===1?failures[0]:`已删除 ${deleted.length} 个，${failures.length} 个未删除，请查看原因后重试。`,duration:3})
   else void message.success({content:`已删除 ${deleted.length} 个账号`,duration:3})
   await onComplete(deleted)
  }finally{locked.current=false;setPending(false)}
 }
 return <Modal title={accounts.length>1?'批量删除账号':'删除账号？'} open width={720} footer={null} closable={!pending} mask={{closable:!pending}} keyboard={!pending} onCancel={()=>{if(!pending)onCancel()}}>
  <p>已选 {accounts.length} 个账号，可删除 {eligible.length} 个{eligible.length<accounts.length?`，将跳过 ${accounts.length-eligible.length} 个在线或状态不可删除的账号`:''}。后台仍会逐个检查群组、任务及操作占用。</p>
  <p>将从本系统移除账号并清理本系统保存的连接凭据。不会删除 Telegram 账号，也不会退出 Telegram 的其他设备。</p>
  <Table size="small" rowKey="id" pagination={false} scroll={{y:280}} dataSource={accounts} columns={[
   {title:'账号',render:(_,a)=><Tooltip title={a.id}><span>{[a.firstName,a.lastName].filter(Boolean).join(' ')||'未获取昵称'} · {a.phone||'未获取手机号（'+a.id.slice(-6)+'）'}</span></Tooltip>},
   {title:'删除结果 / 限制',render:(_,a)=>results[a.id]??accountDeletionDisabledReason(a.status)??'待确认'}
  ]}/>
  <Space style={{marginTop:16}}><Button aria-label="取消" disabled={pending} onClick={onCancel}>取消</Button><Button type="primary" danger aria-label="确认删除" loading={pending} disabled={pending||!eligible.length} onClick={()=>{void confirm()}}>确认删除 {eligible.length} 个账号</Button></Space>
 </Modal>
}
