import {useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Select,Modal,Alert} from 'antd'
import {api} from '../api/client'
import type {Account} from '../types'
import {ErrorNotice} from './Feedback'
export default function ImportGroup({accounts}:{accounts:Account[]}){
 const cache=useQueryClient(),[open,setOpen]=useState(false),[done,setDone]=useState(false)
 const mutation=useMutation({mutationFn:(data:{gatewayGroupId:string;accountIds:string[]})=>api.request('/api/groups/import','POST',data),onSuccess:()=>{setOpen(false);setDone(true);cache.invalidateQueries({queryKey:['groups']})}})
 return <><Button aria-label="导入已有群" onClick={()=>{setOpen(true);setDone(false)}}>导入已有群</Button>{done&&<Alert type="success" title="群组已导入"/>}<Modal title="导入已有测试群" open={open} onCancel={()=>setOpen(false)} footer={null}><p>输入网关群 ID，选择已经位于该群的服务账号。成员角色由网关读取。</p><Form layout="vertical" onFinish={v=>mutation.mutate(v)}><Form.Item label="网关群 ID" name="gatewayGroupId" rules={[{required:true,message:'请输入群ID'}]}><Input/></Form.Item><Form.Item label="关联账号" name="accountIds" rules={[{required:true,message:'选择至少一个账号'}]}><Select mode="multiple" options={accounts.filter(a=>a.status==='online').map(a=>({value:a.id,label:a.id}))}/></Form.Item><Button aria-label="确认导入" type="primary" htmlType="submit" loading={mutation.isPending}>确认导入</Button></Form><ErrorNotice error={mutation.error}/></Modal></>
}

