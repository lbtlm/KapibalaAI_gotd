import {useState} from 'react'
import {Link} from 'react-router-dom'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Button,Modal,Table} from 'antd'
import {api} from '../api/client'
import type {Sequence} from '../types'
import {useSession} from '../hooks'
import {ErrorNotice,LoadState} from '../components/Feedback'
import SequenceEditor from '../components/SequenceEditor'
import styles from '../layout/MainLayout.module.scss'

export default function Sequences(){
 const writable=useSession()?.role==='admin',cache=useQueryClient()
 const definitions=useQuery({queryKey:['sequences'],queryFn:()=>api.request<Sequence[]>('/api/sequences')})
 const [editing,setEditing]=useState<Sequence|null|undefined>()
 const save=useMutation({mutationFn:(value:Omit<Sequence,'id'>)=>api.request(editing?'/api/sequences/'+editing.id:'/api/sequences',editing?'PUT':'POST',value),onSuccess:()=>{setEditing(undefined);cache.invalidateQueries({queryKey:['sequences']})}})
 return <section>
  <div className={styles.pageHeading}><div><h1>剧本库</h1><p>用 JSON 或可视化编辑步骤，在每个群内选择剧本及参与账号。</p></div>{writable&&<Button type="primary" onClick={()=>{save.reset();setEditing(null)}}>新增剧本</Button>}</div>
  <ErrorNotice error={definitions.error}/>
  <Table rowKey="id" size="small" dataSource={definitions.data} pagination={false} locale={{emptyText:<LoadState loading={definitions.isLoading} empty={!definitions.isLoading} description="添加包含管理员、普通成员与发送延迟的剧本。"/>}} columns={[
   {title:'剧本名称',dataIndex:'name'},{title:'步骤数',render:(_,s)=>s.steps.length},{title:'角色',render:(_,s)=>[...new Set(s.steps.map(step=>step.accountRole==='admin'?'管理员':'普通成员'))].join(' / ')},
   {title:'操作',render:(_,s)=><>{writable&&<Button type="text" onClick={()=>{save.reset();setEditing(s)}}>编辑</Button>}<Link to="/groups">到群内使用</Link></>}
  ]} expandable={{expandedRowRender:s=><ol>{s.steps.map(step=><li key={step.index}><strong>{step.accountRole==='admin'?'管理员':'普通成员'} · 延迟 {step.delaySeconds}s</strong><p style={{whiteSpace:'pre-wrap'}}>{step.text}</p></li>)}</ol>}}/>
  <Modal width={880} open={editing!==undefined} title={editing?'编辑剧本':'新增剧本'} footer={null} onCancel={()=>setEditing(undefined)} destroyOnHidden>{editing!==undefined&&<><ErrorNotice error={save.error}/><SequenceEditor key={editing?.id??'new'} initial={editing??undefined} pending={save.isPending} onSave={v=>save.mutate(v)}/></>}</Modal>
 </section>
}
