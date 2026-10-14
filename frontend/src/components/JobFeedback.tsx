import { useQuery } from '@tanstack/react-query'
import { Alert } from 'antd'
import { api } from '../api/client'
import type {Job} from '../types'
import {ErrorNotice} from './Feedback'
export default function JobFeedback({id,onFinished}:{id:string;onFinished?:()=>void}) {
 const query=useQuery({queryKey:['job',id],queryFn:async()=>{const job=await api.request<Job>('/api/jobs/'+id);if(job.status!=='running')onFinished?.();return job},refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 return <><ErrorNotice error={query.error}/><Alert showIcon type={query.data?.status==='failed'?'error':query.data?.status==='finished'?'success':'info'} title={query.data?.status==='finished'?'任务已完成':query.data?.status==='failed'?'任务失败':'任务处理中'} description={<>{id}{query.data?.errors?.map((e,i)=><div key={i}>{e.step} · {e.code}</div>)}</>}/></>
}

