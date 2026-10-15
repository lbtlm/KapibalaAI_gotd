import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Button,Popconfirm,message} from 'antd'
import {api,ApiError} from '../api/client'
import type {Group} from '../types'

export default function MarketingButton({group}:{group:Group}){
 const cache=useQueryClient(),[notice,holder]=message.useMessage(),paused=group.marketingPaused
 const mutation=useMutation({mutationFn:()=>api.request('/api/groups/'+group.id+'/marketing/'+(paused?'resume':'pause'),'POST'),onSuccess:()=>{for(const key of [['group',group.id],['groups'],['group-jobs',group.id],['agent-runs',group.id]])cache.invalidateQueries({queryKey:key});notice.success(paused?'已恢复 Agent 待命。可在“剧本与账号”中重新启动剧本。':'已暂停营销，保留群组、成员与剧本配置。',4)},onError:(e:ApiError)=>notice.error(e.code==='MARKETING_DRAINING'?'上次请求仍在确认结果，请稍后恢复。':e.code==='GROUP_IN_USE'?'群组任务正在运行，请等待任务完成后重试。':e.message,4)})
 return <>{holder}<Popconfirm title={paused?'恢复本群营销？':'暂停本群营销？'} description={paused?'恢复原 Agent 的待命状态；剧本保留配置，由你预览确认后从第 1 步重新发送。':'停止本次剧本和 Agent 活动，保留群与成员；已发出的请求将继续确认结果。'} okText={paused?'恢复':'暂停'} cancelText="取消" onConfirm={()=>mutation.mutateAsync()}><Button disabled={group.status!=='active'||!!group.activeJobId} loading={mutation.isPending}>{paused?'恢复营销':'暂停营销'}</Button></Popconfirm></>
}
