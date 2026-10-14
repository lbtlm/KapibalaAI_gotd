import {useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Switch,Space,Popconfirm} from 'antd'
import type {Account,Group} from '../types'
import {api} from '../api/client'
import {ErrorNotice} from './Feedback'
import JobFeedback from './JobFeedback'
import styles from '../layout/MainLayout.module.scss'
export default function GroupActions({group,accounts}:{group:Group;accounts:Account[]}) {
 const cache=useQueryClient(),[job,setJob]=useState('')
 const [form]=Form.useForm()
 const patch=useMutation({mutationFn:(data:{agentEnabled?:boolean;autoKickEnabled?:boolean})=>api.request('/api/groups/'+group.id,'PATCH',data),onSuccess:()=>cache.invalidateQueries({queryKey:['group',group.id]})})
 const send=useMutation({mutationFn:(data:{accountId:string;text:string})=>api.request('/api/groups/'+group.id+'/send','POST',data),onSuccess:()=>{form.resetFields(['text']);cache.invalidateQueries({queryKey:['messages',group.id]})}})
 const leave=useMutation({mutationFn:()=>api.request<{jobId:string}>('/api/groups/'+group.id+'/leave-all','POST'),onSuccess:r=>setJob(r.jobId)})
 const memberIds=new Set(group.members.map(m=>m.accountId))
 return <div className={styles.panel}><div className={styles.panelTitle}><h2>群组操作</h2><Popconfirm title="确认所有服务账号退群？" description="群主会在其余账号成功退出后离开。" onConfirm={()=>leave.mutate()}><Button danger disabled={group.status==='left'} loading={leave.isPending}>全部退群</Button></Popconfirm></div><ErrorNotice error={patch.error??send.error??leave.error}/><Space size={24}><label>Agent 自动响应 <Switch aria-label="Agent 自动响应" checked={group.agentEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={agentEnabled=>patch.mutate({agentEnabled})}/></label><label>允许自动移除成员 <Switch aria-label="允许自动移除成员" checked={group.autoKickEnabled} loading={patch.isPending} disabled={group.status!=='active'} onChange={autoKickEnabled=>patch.mutate({autoKickEnabled})}/></label></Space><Form form={form} layout="vertical" onFinish={data=>send.mutate(data)}><Form.Item label="发送账号" name="accountId" rules={[{required:true,message:'请选择群内可用账号'}]}><Select placeholder="选择群成员" options={accounts.filter(a=>memberIds.has(a.id)&&['online','rate_limited'].includes(a.status)).map(a=>({value:a.id,label:a.id+(a.status==='rate_limited'?'（限流等待）':'')}))}/></Form.Item><Form.Item label="消息内容" name="text" rules={[{required:true,message:'请输入消息'},{validator:(_,value)=>value?.trim()?Promise.resolve():Promise.reject(new Error('消息不能为空'))}]}><Input.TextArea rows={3}/></Form.Item><Button aria-label="发送消息" type="primary" htmlType="submit" loading={send.isPending} disabled={group.status!=='active'}>发送消息</Button></Form>{job&&<JobFeedback id={job} onFinished={()=>cache.invalidateQueries({queryKey:['group',group.id]})}/>}</div>
}

