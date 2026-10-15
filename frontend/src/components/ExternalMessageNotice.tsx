import {useEffect,useState} from 'react'
import {Button} from 'antd'
import type {Message} from '../types'
import styles from '../layout/MainLayout.module.scss'

const key=(m:Message)=>m.sentAt+'|'+(m.msgId??m.clientMsgId??'')
export default function ExternalMessageNotice({messages,active,onOpen}:{messages:Message[];active:boolean;onOpen:()=>void}){
 const [seen,setSeen]=useState('')
 const latest=messages.reduce((last,m)=>key(m)>last?key(m):last,'')
 useEffect(()=>{if(active)setSeen(latest)},[active,latest])
 const unread=messages.filter(m=>key(m)>seen).length
 if(active||!unread)return null
 return <div role="status" className={styles.externalNotice}><span><strong>{unread} 条待查看外部消息</strong><small>仅外部成员消息触发已启用的 Agent</small></span><Button type="text" size="small" onClick={onOpen}>查看消息</Button></div>
}
