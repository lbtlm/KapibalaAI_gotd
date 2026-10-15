import {useState} from 'react'
import {Select,Space,Table} from 'antd'
import type {Account,Job} from '../types'
import {formatTime} from '../hooks'
import {accountLabel} from './AccountTableColumns'
import {jobPhaseLabel} from './JobFeedback'
import StatusTag from './StatusTag'
import styles from '../layout/MainLayout.module.scss'

const kinds:Record<string,string>={create:'建群',join:'成员加群',promote:'设置管理员',leave:'结束营销',pause:'暂停营销',resume:'恢复营销'}
export default function GroupLogs({jobs,accounts}:{jobs:Job[];accounts:Account[]}){
 const [kind,setKind]=useState(''),[result,setResult]=useState('')
 const rows=jobs.flatMap((job,j)=>(job.logs?.length?job.logs:[{at:job.createdAt??'',phase:job.phase??'',result:job.status,code:job.errors.map(e=>e.code).join('；')}]).map((entry,i)=>({...entry,key:(job.id??j)+'-'+i,jobId:job.id,kind:job.kind??''}))).sort((a,b)=>b.at.localeCompare(a.at))
 return <div className={styles.tableShell}><Space wrap style={{marginBottom:12}}><Select aria-label="日志任务" value={kind} onChange={setKind} style={{width:150}} options={[{value:'',label:'全部任务'},...Object.entries(kinds).map(([value,label])=>({value,label}))]}/><Select aria-label="日志结果" value={result} onChange={setResult} style={{width:140}} options={[{value:'',label:'全部结果'},{value:'success',label:'成功'},{value:'failed',label:'失败'}]}/><span className={styles.meta}>最近 {jobs.length} 个任务的逐步记录</span></Space>
  <Table size="small" rowKey="key" scroll={{x:850}} dataSource={rows.filter(r=>(!kind||r.kind===kind)&&(!result||r.result===result||(result==='success'&&r.result==='finished')))} pagination={{pageSize:20,showSizeChanger:false}} columns={[
   {title:'时间',dataIndex:'at',width:170,render:formatTime},{title:'任务',dataIndex:'kind',width:110,render:v=>kinds[v]??v},{title:'操作阶段',dataIndex:'phase',width:160,render:jobPhaseLabel},
   {title:'执行账号',render:(_,r)=>{if(!r.accountId)return '—';const a=accounts.find(a=>a.id===r.accountId);return a?accountLabel(a):r.accountId}},
   {title:'结果',dataIndex:'result',width:90,render:v=><StatusTag status={v}/>},{title:'错误码',dataIndex:'code',render:v=>v||'—'},{title:'任务 ID',dataIndex:'jobId',width:110,render:v=><span title={v} className={styles.mono}>{v?.slice(0,8)??'—'}</span>}
  ]}/>
 </div>
}
