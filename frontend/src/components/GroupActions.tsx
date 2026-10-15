import {useEffect,useRef,useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Switch,message} from 'antd'
import type {TextAreaRef} from 'antd/es/input/TextArea'
import type {Account,Group} from '../types'
import {api} from '../api/client'
import {ErrorNotice} from './Feedback'
import JobFeedback from './JobFeedback'
import EndMarketingButton from './EndMarketingButton'
import AgentPreview,{useAgentStatus} from './AgentPreview'
import MarketingButton from './MarketingButton'
import {accountLabel} from './AccountTableColumns'
import styles from '../layout/MainLayout.module.scss'
export interface ReplyTarget {key:string;sender:string;text:string}
export function GroupComposer({group,accounts,replyTo,onClearReply}:{group:Group;accounts:Account[];replyTo?:ReplyTarget|null;onClearReply?:()=>void}){
 const cache=useQueryClient(),[form]=Form.useForm(),[notice,holder]=message.useMessage(),input=useRef<TextAreaRef>(null)
 const send=useMutation({mutationFn:(data:{accountId:string;text:string})=>api.request('/api/groups/'+group.id+'/send','POST',data),onSuccess:()=>{form.resetFields(['text']);onClearReply?.();cache.invalidateQueries({queryKey:['messages',group.id]});notice.success('消息已加入发送队列')},onError:(e:Error)=>notice.error(e.message,5)})
 const memberIds=new Set(group.members.map(m=>m.accountId))
 const available=accounts.filter(a=>memberIds.has(a.id)&&['online','rate_limited'].includes(a.status))
 useEffect(()=>{if(replyTo)input.current?.focus()},[replyTo])
 useEffect(()=>{if(!form.getFieldValue('accountId')&&available.some(a=>a.id===group.adminAccountId))form.setFieldValue('accountId',group.adminAccountId)},[accounts,group.adminAccountId,group.members,form])
 return <div className={styles.composer}>{holder}{replyTo&&<div className={styles.replyContext}><div><strong>回复 {replyTo.sender}</strong><p title={replyTo.text}>{replyTo.text}</p><small>将引用这段文字发送</small></div><Button type="text" onClick={onClearReply}>取消回复</Button></div>}<Form form={form} layout="vertical" onFinish={data=>send.mutate({...data,text:replyTo?`回复 ${replyTo.sender}：\n> ${replyTo.text.slice(0,300).replace(/\n/g,'\n> ')}\n\n${data.text}`:data.text})}><div className={styles.composeAccount}><Form.Item label="发送账号" name="accountId" rules={[{required:true,message:'请选择群内可用账号'}]}><Select showSearch optionFilterProp="label" placeholder="选择群成员" options={available.map(a=>({value:a.id,label:accountLabel(a)+(a.status==='rate_limited'?'（限流等待）':''),title:accountLabel(a)+' · '+a.id}))}/></Form.Item><span className={styles.meta}>以所选账号发送 · 自有消息不触发 Agent</span></div><div className={styles.composeGrid}><div className={styles.composeText}><Form.Item label="消息内容" name="text" rules={[{required:true,message:'请输入消息'},{validator:(_,value)=>value?.trim()?Promise.resolve():Promise.reject(new Error('消息不能为空'))}]}><Input.TextArea ref={input} rows={2}/></Form.Item></div><Button aria-label="发送消息" type="primary" htmlType="submit" loading={send.isPending} disabled={group.status!=='active'}>发送消息</Button></div></Form></div>
}
export default function GroupActions({group}:{group:Group}){
 const cache=useQueryClient(),[job,setJob]=useState(''),agentStatus=useAgentStatus()
 const patch=useMutation({mutationFn:(data:{agentEnabled?:boolean;autoKickEnabled?:boolean})=>api.request('/api/groups/'+group.id,'PATCH',data),onSuccess:()=>cache.invalidateQueries({queryKey:['group',group.id]})})
 return <div className={styles.sideSection}><h2>群组操作</h2><ErrorNotice error={patch.error}/><p className={styles.meta}>{agentStatus.isError?'AI 模式读取失败':agentStatus.data?.mode==='openai'?'真实 AI · '+agentStatus.data.model:agentStatus.data?.mode==='scripted'?'当前为固定回复模式':'正在读取 AI 模式'}</p><AgentPreview context={group.title?'群名称：'+group.title:''}/><div className={styles.settingRow}><label htmlFor="agent-switch">Agent 自动响应</label><Switch id="agent-switch" aria-label="Agent 自动响应" checked={group.agentEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={agentEnabled=>patch.mutate({agentEnabled})}/></div><div className={styles.settingRow}><label htmlFor="kick-switch">允许自动移除成员</label><Switch id="kick-switch" aria-label="允许自动移除成员" checked={group.autoKickEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={autoKickEnabled=>patch.mutate({autoKickEnabled})}/></div><div className={styles.marketingButtons}><MarketingButton group={group}/><EndMarketingButton group={group} onStarted={setJob}/></div>{job&&<JobFeedback id={job} onFinished={()=>cache.invalidateQueries({queryKey:['group',group.id]})}/>}</div>
}
