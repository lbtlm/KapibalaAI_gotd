import {useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Modal,Popconfirm,Space,Table} from 'antd'
import {api} from '../api/client'
import type {AccountGroup} from '../types'
import {LoadState} from './Feedback'
import {AccountErrorNotice as ErrorNotice} from './AccountFeedback'
import styles from '../layout/MainLayout.module.scss'
export default function AccountGroupManager({groups,error,onClose}:{groups:AccountGroup[];error:unknown;onClose:()=>void}){
 const cache=useQueryClient(),[form]=Form.useForm(),[edit,setEdit]=useState<string>(),[name,setName]=useState('')
 const change=useMutation({mutationFn:({operation,id,name}:{operation:'create'|'rename'|'delete';id?:string;name?:string})=>api.request('/api/account-groups'+(id?'/'+encodeURIComponent(id):''),operation==='create'?'POST':operation==='rename'?'PATCH':'DELETE',name?{name}:undefined),onSuccess:async(_,v)=>{if(v.operation==='create')form.resetFields();if(v.operation==='rename')setEdit(undefined);await Promise.all([cache.invalidateQueries({queryKey:['account-groups']}),cache.invalidateQueries({queryKey:['accounts']}),cache.invalidateQueries({queryKey:['account']}),cache.invalidateQueries({queryKey:['account-logs']})])}})
 return <Modal title="管理分组" open width={680} footer={null} onCancel={onClose}>
  <p>每个账号属于一个分组；删除分组会保留账号。</p><ErrorNotice error={error??change.error}/>
  <Form form={form} layout="inline" onFinish={v=>change.mutate({operation:'create',name:v.name.trim()})} className={styles.groupCreateForm}><Form.Item label="新分组名称" name="name" rules={[{required:true,whitespace:true,message:'请输入分组名称'},{max:100,message:'分组名称不超过100字'}]}><Input aria-label="新分组名称" maxLength={100}/></Form.Item><Button type="primary" htmlType="submit" loading={change.isPending}>创建分组</Button></Form>
  <Table size="small" rowKey="id" dataSource={groups} pagination={false} scroll={{y:360}} locale={{emptyText:<LoadState empty description="创建分组后，可批量移动账号。"/>}} columns={[
   {title:'分组名称',key:'name',render:(_,g)=>edit===g.id?<Input aria-label="分组名称" value={name} onChange={e=>setName(e.target.value)} maxLength={100}/>:g.name},
   {title:'账号数',dataIndex:'accountCount',width:90},
   {title:'操作',key:'actions',width:220,render:(_,g)=><Space size={0}>{edit===g.id?<><Button type="text" loading={change.isPending} disabled={!name.trim()} onClick={()=>change.mutate({operation:'rename',id:g.id,name:name.trim()})} aria-label="保存">保存</Button><Button type="text" onClick={()=>setEdit(undefined)}>取消</Button></>:<Button type="text" disabled={change.isPending} onClick={()=>{setEdit(g.id);setName(g.name);change.reset()}}>重命名</Button>}<Popconfirm title="删除后账号将移至未分组，不会删除账号。" okText="确认删除" cancelText="取消" onConfirm={()=>change.mutateAsync({operation:'delete',id:g.id})}><Button type="text" danger disabled={change.isPending} aria-label="删除">删除</Button></Popconfirm></Space>}
  ]}/>
 </Modal>
}
