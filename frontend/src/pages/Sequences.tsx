import {useState} from 'react'
import {Link} from 'react-router-dom'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Button,Modal,Table} from 'antd'
import {api} from '../api/client'
import type {Sequence} from '../types'
import {useSession} from '../hooks'
import {ErrorNotice,LoadState} from '../components/Feedback'
import SequenceEditor,{type GeneratedSequence} from '../components/SequenceEditor'
import SequencePreview from '../components/SequencePreview'
import styles from '../layout/MainLayout.module.scss'

export default function Sequences(){
 const writable=useSession()?.role==='admin',cache=useQueryClient()
 const definitions=useQuery({queryKey:['sequences'],queryFn:()=>api.request<Sequence[]>('/api/sequences')})
 const [editing,setEditing]=useState<Sequence|null|undefined>()
 const save=useMutation({mutationFn:(value:Omit<Sequence,'id'>)=>api.request(editing?'/api/sequences/'+editing.id:'/api/sequences',editing?'PUT':'POST',value),onSuccess:()=>{setEditing(undefined);cache.invalidateQueries({queryKey:['sequences']})}})
 return <section>
  <div className={styles.pageHeading}><div><h1>剧本库</h1><p>用 JSON 或可视化编辑步骤，在每个群内选择剧本及参与账号。</p></div>{writable&&<Button type="primary" onClick={()=>{save.reset();setEditing(null)}}>新增剧本</Button>}</div>
  <ErrorNotice error={definitions.error}/>
  <Table className={styles.tableShell} scroll={{x:720}} rowKey="id" size="small" dataSource={definitions.data} pagination={false} locale={{emptyText:<LoadState loading={definitions.isLoading} empty={!definitions.isLoading} description="添加包含管理员、普通成员与发送延迟的剧本。"/>}} columns={[
   {title:'剧本名称',dataIndex:'name'},{title:'步骤数',render:(_,s)=>s.steps.length},{title:'角色',render:(_,s)=>[...new Set(s.steps.map(step=>step.accountRole==='admin'?'管理员':'普通成员'))].join(' / ')},
   {title:'操作',render:(_,s)=><>{writable&&<Button type="text" onClick={()=>{save.reset();setEditing(s)}}>编辑</Button>}<Link to="/groups">到群内使用</Link></>}
  ]} expandable={{expandedRowRender:s=><div style={{maxWidth:680,margin:'0 auto',padding:16}}><SequencePreview value={s}/></div>}}/>
  <Modal width={1320} style={{top:24,maxWidth:'calc(100vw - 32px)'}} open={editing!==undefined} title={editing?'编辑剧本':'新增剧本'} footer={null} onCancel={()=>!save.isPending&&setEditing(undefined)} closable={!save.isPending} maskClosable={false} destroyOnHidden>{editing!==undefined&&<><ErrorNotice error={save.error}/><SequenceEditor key={editing?.id??'new'} initial={editing??undefined} pending={save.isPending} onSave={v=>save.mutate(v)} onGenerate={input=>api.request<GeneratedSequence>('/api/sequences/generate','POST',input)}/></>}</Modal>
 </section>
}
