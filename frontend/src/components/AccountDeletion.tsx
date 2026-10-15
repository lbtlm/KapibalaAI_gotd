import {useRef,useState} from 'react'
import {Button,Modal,Space,Tooltip,Typography} from 'antd'
import {AccountErrorNotice} from './AccountFeedback'

export const accountDeletionDisabledReason = (status:string):string|undefined =>
 ['idle','disconnected','session_expired','suspended'].includes(status)?undefined:['online','rate_limited','connecting'].includes(status)?'请先将账号离线，再删除。':'当前状态暂不可删除，请刷新后重试。'

export function AccountDeleteButton({writable,status,busy,onClick}:{writable:boolean;status:string;busy:boolean;onClick:()=>void}){
 if(!writable)return null
 const reason=accountDeletionDisabledReason(status)
 return <Tooltip title={reason}><span><Button type="text" danger aria-label="删除账号" disabled={busy||!!reason} onClick={onClick}>删除</Button></span></Tooltip>
}

export function DeleteAccountDialog({id,phone,name,busy,error,disabledReason,onConfirm,onCancel}:{id:string;phone?:string;name?:string;busy:boolean;error:unknown;disabledReason?:string;onConfirm:()=>Promise<unknown>;onCancel:()=>void}){
 const locked=useRef(false),[pending,setPending]=useState(false),submitting=busy||pending
 async function confirm(){
  if(locked.current||submitting||disabledReason)return
  locked.current=true;setPending(true)
  try{await onConfirm()}catch{/* The mutation error remains visible through the error prop. */}finally{locked.current=false;setPending(false)}
 }
 return <Modal title="删除账号？" open footer={null} closable={!submitting} mask={{closable:!submitting}} keyboard={!submitting} onCancel={()=>{if(!submitting)onCancel()}}>
  {phone&&<p>手机号：<Typography.Text>{phone}</Typography.Text></p>}
  {name&&<p>昵称：<Typography.Text>{name}</Typography.Text></p>}
  <p>账号 ID：<Typography.Text code>{id}</Typography.Text></p>
  <p>将从本系统移除此账号并清理本系统保存的连接凭据。不会删除 Telegram 账号，也不会退出 Telegram 的其他设备。</p>
  <AccountErrorNotice error={error}/>{disabledReason&&<p>{disabledReason}</p>}
  <Space><Button aria-label="取消" disabled={submitting} onClick={onCancel}>取消</Button><Button type="primary" danger aria-label="确认删除" loading={submitting} disabled={submitting||!!disabledReason} onClick={()=>{void confirm()}}>确认删除</Button></Space>
 </Modal>
}
