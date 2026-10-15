import {useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Select,Space,Tag} from 'antd'
import {api} from '../api/client'
import type {Account,Group} from '../types'
import {accountLabel} from './AccountTableColumns'
import {ErrorNotice} from './Feedback'
import JobFeedback from './JobFeedback'
import styles from '../layout/MainLayout.module.scss'

export default function GroupMembers({group,accounts,writable}:{group:Group;accounts:Account[];writable:boolean}) {
 const cache=useQueryClient(),[selected,setSelected]=useState<string[]>([]),[job,setJob]=useState('')
 const refresh=()=>{cache.invalidateQueries({queryKey:['group',group.id]});cache.invalidateQueries({queryKey:['group-jobs',group.id]})}
 const join=useMutation({mutationFn:()=>api.request<{jobId:string}>('/api/groups/'+group.id+'/members','POST',{accountIds:selected}),onSuccess:r=>{setJob(r.jobId);setSelected([]);refresh()}})
 const admin=useMutation({mutationFn:async(id:string)=>{const member=group.members.find(m=>m.accountId===id);if(member?.role==='member'){const result=await api.request<{jobId:string}>('/api/groups/'+group.id+'/administrator','POST',{accountId:id});setJob(result.jobId)}else await api.request('/api/groups/'+group.id,'PATCH',{adminAccountId:id})},onSuccess:refresh})
 const ids=new Set(group.members.map(m=>m.accountId)),busy=!!group.activeAgentRunId||!!group.activeSequenceRunId||group.status!=='active'
 const label=(id:string|null,pid:string)=>{const account=accounts.find(a=>a.id===id);return account?accountLabel(account):pid}
 return <section className={styles.sideSection}><h2>群成员 · {group.members.length}</h2><ErrorNotice error={join.error??admin.error}/>
  {group.members.map(m=><div className={styles.memberRow} key={m.platformUserId}><span>{label(m.accountId,m.platformUserId)}<div><Tag>{m.role==='creator'?'群主':m.role==='admin'?'管理员':'成员'}</Tag>{m.accountId===group.adminAccountId&&<Tag color="blue">Agent 身份</Tag>}</div></span>{writable&&m.accountId&&m.accountId!==group.adminAccountId&&<Button size="small" type="text" disabled={busy||admin.isPending} onClick={()=>admin.mutate(m.accountId!)}>{m.role==='member'?'设为管理员':'绑定 Agent'}</Button>}</div>)}
  {writable&&<><Select mode="multiple" aria-label="新增群成员" placeholder="选择要加入的在线账号" style={{width:'100%'}} value={selected} onChange={setSelected} options={accounts.filter(a=>a.status==='online'&&!ids.has(a.id)).map(a=>({value:a.id,label:accountLabel(a)}))}/><Space style={{marginTop:8}}><Button disabled={!selected.length||group.status!=='active'} loading={join.isPending} onClick={()=>join.mutate()}>加入本群</Button></Space></>}
  {job&&<JobFeedback id={job} onFinished={refresh}/>}
 </section>
}
