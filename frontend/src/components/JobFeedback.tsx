import {useQuery} from '@tanstack/react-query'
import {Alert} from 'antd'
import {Link} from 'react-router-dom'
import {api} from '../api/client'
import type {Job} from '../types'
import {ErrorNotice} from './Feedback'
export const jobPhaseLabel=(phase?:string)=>({create:'创建群组',configure:'设置名称与链接',invite:'获取邀请链接',join:'等待成员入群',promote:'设置管理员',drain:'等待已发起操作确认',leave:'依次退群'}[phase??'']??phase??'等待执行')
export default function JobFeedback({id,onFinished}:{id:string;onFinished?:()=>void}) {
 const query=useQuery({queryKey:['job',id],queryFn:async()=>{const job=await api.request<Job>('/api/jobs/'+id);if(job.status!=='running')onFinished?.();return job},refetchInterval:q=>q.state.data?.status==='running'?1000:false})
 const job=query.data
 return <><ErrorNotice error={query.error}/><Alert showIcon type={job?.status==='failed'?'error':job?.status==='finished'?'success':'info'} title={job?.status==='finished'?'任务已完成':job?.status==='failed'?'任务失败':jobPhaseLabel(job?.phase)} description={<>{job?.groupId&&<Link to={'/groups/'+job.groupId}>进入群工作区</Link>}<div>{id}</div>{job?.total? <div>已处理 {job.position??0} / {job.total} 个账号</div>:null}{job?.errors?.map((e,i)=><div key={i}>{e.step==='private_fallback'?'已回退为私密链接':jobPhaseLabel(e.step)} · {e.code}</div>)}</>}/></>
}
