import {useState} from 'react'
import {Link} from 'react-router-dom'
import {useQuery,useMutation,useQueryClient} from '@tanstack/react-query'
import {Table,Button,Form,Input,Select,Modal,Space,Checkbox} from 'antd'
import {api} from '../api/client'
import type {Account,Group} from '../types'
import {useSession} from '../hooks'
import StatusTag from '../components/StatusTag'
import {accountLabel} from '../components/AccountTableColumns'
import {ErrorNotice,LoadState} from '../components/Feedback'
import ImportGroup from '../components/ImportGroup'
import JobFeedback from '../components/JobFeedback'
import styles from '../layout/MainLayout.module.scss'

export default function Groups() {
 const session=useSession(),cache=useQueryClient(),[open,setOpen]=useState(false),[job,setJob]=useState('')
 const groups=useQuery({queryKey:['groups'],queryFn:()=>api.request<Group[]>('/api/groups'),refetchInterval:5000})
 const accounts=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const [form]=Form.useForm(),creator=Form.useWatch('creatorAccountId',form),members:string[]=Form.useWatch('memberAccountIds',form)??[]
 const create=useMutation({mutationFn:(data:unknown)=>api.request<{jobId:string}>('/api/groups','POST',data),onSuccess:r=>{setJob(r.jobId);setOpen(false);cache.invalidateQueries({queryKey:['groups']})}})
 const online=(accounts.data??[]).filter(a=>a.status==='online')
 const label=(id:string|null|undefined)=>{const a=accounts.data?.find(a=>a.id===id);return a?accountLabel(a):id??'未指定'}
 return <section>
  <div className={styles.pageHeading}><div><h1>群组工作区</h1><p>创建群组、分配成员与剧本，查看实时消息和执行记录。</p></div>{session?.role==='admin'&&<Space><ImportGroup accounts={accounts.data??[]}/><Button type="primary" onClick={()=>setOpen(true)}>创建群组</Button></Space>}</div>
  <ErrorNotice error={groups.error??create.error}/>
  {job&&<JobFeedback id={job} onFinished={()=>cache.invalidateQueries({queryKey:['groups']})}/>}
  <div className={styles.tableShell}><Table size="small" rowKey="id" locale={{emptyText:<LoadState loading={groups.isLoading} empty={!groups.isLoading} description="创建或导入群组后，进入群内配置剧本与 Agent。"/>}} dataSource={groups.data} pagination={false} columns={[
   {title:'群组',render:(_,g)=><Link to={'/groups/'+g.id}>{g.title||g.gatewayGroupId||g.id}{g.username&&<div className={styles.meta}>@{g.username}</div>}</Link>},
   {title:'状态',dataIndex:'status',render:s=><StatusTag status={s}/>},
   {title:'群主',render:(_,g)=>label(g.creatorAccountId)},
   {title:'管理员 / Agent 身份',render:(_,g)=>label(g.adminAccountId)},
   {title:'成员',render:(_,g)=>g.members.length},
   {title:'Agent',render:(_,g)=>g.agentEnabled?'已启用':'已关闭'},
   {title:'运行',render:(_,g)=><Space>{g.activeSequenceRunId&&<StatusTag status="running"/>}{g.activeAgentRunId?'Agent 执行中':!g.activeSequenceRunId?'空闲':''}</Space>}
  ]}/></div>
  <Modal title="创建群组" open={open} footer={null} onCancel={()=>setOpen(false)}>
   <Form form={form} layout="vertical" initialValues={{privateOnConflict:true}} onFinish={v=>create.mutate(v)}>
    <Form.Item label="群名称" name="title" rules={[{required:true,message:'填写群名称'},{max:128}]}><Input maxLength={128}/></Form.Item>
    <Form.Item label="公开链接（选填）" name="username" extra="留空使用私密邀请链接；填写 t.me/ 后面的名称。" rules={[{pattern:/^[A-Za-z][A-Za-z0-9_]{4,31}$/,message:'使用 5–32 位字母、数字或下划线，以字母开头'}]}><Input addonBefore="t.me/"/></Form.Item>
    <Form.Item name="privateOnConflict" valuePropName="checked"><Checkbox>公开链接被占用时，保留群并回退为私密邀请链接</Checkbox></Form.Item>
    <Form.Item label="群主账号" name="creatorAccountId" rules={[{required:true,message:'选择群主'}]}><Select options={online.map(a=>({value:a.id,label:accountLabel(a)}))} onChange={()=>form.setFieldsValue({memberAccountIds:[],adminAccountId:undefined})}/></Form.Item>
    <Form.Item label="成员账号" name="memberAccountIds" rules={[{required:true,message:'至少选择一位成员'}]}><Select mode="multiple" options={online.filter(a=>a.id!==creator).map(a=>({value:a.id,label:accountLabel(a)}))} onChange={ids=>{if(!ids.includes(form.getFieldValue('adminAccountId')))form.setFieldValue('adminAccountId',ids[0])}}/></Form.Item>
    <Form.Item label="管理员 / Agent 执行账号" name="adminAccountId" extra="默认选择首位成员，可以从已选成员中替换。" rules={[{required:true,message:'选择管理员'}]}><Select options={online.filter(a=>members.includes(a.id)).map(a=>({value:a.id,label:accountLabel(a)}))}/></Form.Item>
    <Button type="primary" htmlType="submit" loading={create.isPending}>提交建群任务</Button>
   </Form><ErrorNotice error={create.error}/>
  </Modal>
 </section>
}
