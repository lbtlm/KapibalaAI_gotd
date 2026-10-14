import {useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Switch,Popconfirm} from 'antd'
import type {Account,Group} from '../types'
import {api} from '../api/client'
import {ErrorNotice} from './Feedback'
import JobFeedback from './JobFeedback'
import styles from '../layout/MainLayout.module.scss'
export function GroupComposer({group,accounts}:{group:Group;accounts:Account[]}){
 const cache=useQueryClient(),[form]=Form.useForm()
 const send=useMutation({mutationFn:(data:{accountId:string;text:string})=>api.request('/api/groups/'+group.id+'/send','POST',data),onSuccess:()=>{form.resetFields(['text']);cache.invalidateQueries({queryKey:['messages',group.id]})}})
 const memberIds=new Set(group.members.map(m=>m.accountId))
 return <div className={styles.composer}><ErrorNotice error={send.error}/><Form form={form} layout="vertical" onFinish={data=>send.mutate(data)}><div className={styles.composeAccount}><Form.Item label="发送账号" name="accountId" rules={[{required:true,message:'请选择群内可用账号'}]}><Select placeholder="选择群成员" options={accounts.filter(a=>memberIds.has(a.id)&&['online','rate_limited'].includes(a.status)).map(a=>({value:a.id,label:a.id+(a.status==='rate_limited'?'（限流等待）':'')}))}/></Form.Item><span className={styles.meta}>以所选服务账号发送</span></div><div className={styles.composeGrid}><div className={styles.composeText}><Form.Item label="消息内容" name="text" rules={[{required:true,message:'请输入消息'},{validator:(_,value)=>value?.trim()?Promise.resolve():Promise.reject(new Error('消息不能为空'))}]}><Input.TextArea rows={2}/></Form.Item></div><Button aria-label="发送消息" type="primary" htmlType="submit" loading={send.isPending} disabled={group.status!=='active'}>发送消息</Button></div></Form></div>
}
export default function GroupActions({group}:{group:Group}){
 const cache=useQueryClient(),[job,setJob]=useState('')
 const patch=useMutation({mutationFn:(data:{agentEnabled?:boolean;autoKickEnabled?:boolean})=>api.request('/api/groups/'+group.id,'PATCH',data),onSuccess:()=>cache.invalidateQueries({queryKey:['group',group.id]})})
 const leave=useMutation({mutationFn:()=>api.request<{jobId:string}>('/api/groups/'+group.id+'/leave-all','POST'),onSuccess:r=>setJob(r.jobId)})
 return <div className={styles.sideSection}><h2>群组操作</h2><ErrorNotice error={patch.error??leave.error}/><div className={styles.settingRow}><label htmlFor="agent-switch">Agent 自动响应</label><Switch id="agent-switch" aria-label="Agent 自动响应" checked={group.agentEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={agentEnabled=>patch.mutate({agentEnabled})}/></div><div className={styles.settingRow}><label htmlFor="kick-switch">允许自动移除成员</label><Switch id="kick-switch" aria-label="允许自动移除成员" checked={group.autoKickEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={autoKickEnabled=>patch.mutate({autoKickEnabled})}/></div><Popconfirm title="确认所有服务账号退群？" description="群主会在其余账号成功退出后离开。" onConfirm={()=>leave.mutate()}><Button className={styles.leaveButton} danger disabled={group.status==='left'} loading={leave.isPending}>全部退群</Button></Popconfirm>{job&&<JobFeedback id={job} onFinished={()=>cache.invalidateQueries({queryKey:['group',group.id]})}/>}</div>
}
