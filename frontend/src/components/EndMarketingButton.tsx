import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Popconfirm,message} from 'antd'
import {api} from '../api/client'
import type {Group} from '../types'

export default function EndMarketingButton({group,onStarted,compact=false}:{group:Group;onStarted:(id:string)=>void;compact?:boolean}){
 const cache=useQueryClient(),[notice,holder]=message.useMessage()
 const leave=useMutation({mutationFn:()=>api.request<{jobId:string}>('/api/groups/'+group.id+'/leave-all','POST'),onSuccess:r=>{onStarted(r.jobId);cache.invalidateQueries({queryKey:['group',group.id]});cache.invalidateQueries({queryKey:['groups']})},onError:(e:Error)=>notice.error(e.message,3)})
 return <>{holder}<Popconfirm title={'结束「'+(group.title||'本群')+'」营销？'} description="停止 Agent 和剧本，等待已发起的操作确认后依次退群；任何成员失败时保留群主。" okText="确认结束" cancelText="取消" onConfirm={()=>leave.mutateAsync()}><Button type={compact?'link':'default'} danger disabled={group.status==='left'||group.status==='ending'||!!group.activeJobId} loading={leave.isPending}>结束群组营销</Button></Popconfirm></>
}
