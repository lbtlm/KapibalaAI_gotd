import {useState} from 'react'
import {Link} from 'react-router-dom'
import {useQuery,useMutation,useQueryClient} from '@tanstack/react-query'
import {Table,Button,Form,Select,Modal,Tag,Space} from 'antd'
import {api} from '../api/client'
import type {Account,Group} from '../types'
import {useSession} from '../hooks'
import StatusTag from '../components/StatusTag'
import {ErrorNotice} from '../components/Feedback'
import ImportGroup from '../components/ImportGroup'
import JobFeedback from '../components/JobFeedback'
import styles from '../layout/MainLayout.module.scss'
export default function Groups() {
 const session=useSession(),cache=useQueryClient(),[open,setOpen]=useState(false),[job,setJob]=useState('')
 const groups=useQuery({queryKey:['groups'],queryFn:()=>api.request<Group[]>('/api/groups')})
 const accounts=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const [form]=Form.useForm(),creator=Form.useWatch('creatorAccountId',form)
 const create=useMutation({mutationFn:(data:{creatorAccountId:string;memberAccountIds:string[]})=>api.request<{jobId:string}>('/api/groups','POST',data),onSuccess:r=>{setJob(r.jobId);setOpen(false)}})
 const online=(accounts.data??[]).filter(a=>a.status==='online')
 return <section><div className={styles.pageHeading}><div><span>群组 / GROUP OPERATIONS</span><h1>群组工作区</h1><p>查看消息、成员和自动响应，任务结果与实际状态同步。</p></div>{session?.role==='admin'&&<Space><ImportGroup accounts={accounts.data??[]}/><Button type="primary" aria-label="创建群组" onClick={()=>setOpen(true)}>创建群组</Button></Space>}</div><ErrorNotice error={groups.error??create.error}/>{job&&<JobFeedback id={job} onFinished={()=>cache.invalidateQueries({queryKey:['groups']})}/>}<Table rowKey="id" loading={groups.isLoading} dataSource={groups.data} pagination={false} columns={[{title:'群组',dataIndex:'id',render:(id:string)=><Link to={'/groups/'+id}>{id}</Link>},{title:'状态',dataIndex:'status',render:(s:string)=><StatusTag status={s}/>},{title:'成员',render:(_,g)=>g.members.length},{title:'Agent',render:(_,g)=><Tag>{g.agentEnabled?'已启用':'已关闭'}</Tag>},{title:'活动运行',render:(_,g)=>g.activeAgentRunId??g.activeSequenceRunId??'—'}]}/><Modal title="创建群组" open={open} footer={null} onCancel={()=>setOpen(false)}><p>选择在线账号。首位成员将被提升为管理员。</p><Form form={form} layout="vertical" onFinish={v=>create.mutate(v)}><Form.Item label="群主账号" name="creatorAccountId" rules={[{required:true,message:'选择群主'}]}><Select options={online.map(a=>({value:a.id,label:a.id}))} onChange={()=>form.setFieldValue('memberAccountIds',[])}/></Form.Item><Form.Item label="成员账号（第一位为管理员）" name="memberAccountIds" rules={[{required:true,message:'至少选择一位成员'}]}><Select mode="multiple" options={online.filter(a=>a.id!==creator).map(a=>({value:a.id,label:a.id}))}/></Form.Item><Button type="primary" htmlType="submit" loading={create.isPending}>提交建群任务</Button></Form><ErrorNotice error={create.error}/></Modal></section>
}


