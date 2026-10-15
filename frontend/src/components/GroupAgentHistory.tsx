import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Button,Select,Space} from 'antd'
import {api} from '../api/client'
import type {Account,AgentRun,Group} from '../types'
import {accountLabel} from './AccountTableColumns'
import {formatTime} from '../hooks'
import {AgentRunDetails} from '../pages/AgentRun'
import {ErrorNotice} from './Feedback'
import styles from '../layout/MainLayout.module.scss'

export default function GroupAgentHistory({group,accounts}:{group:Group;accounts:Account[]}){
 const [selected,setSelected]=useState('')
 const query=useQuery({queryKey:['agent-runs',group.id],queryFn:()=>api.request<AgentRun[]>('/api/groups/'+group.id+'/agent-runs'),refetchInterval:5000})
 const runs=query.data??[],current=selected||runs[0]?.id,index=runs.findIndex(r=>r.id===current),admin=accounts.find(a=>a.id===group.adminAccountId)
 const state=group.status==='left'||group.status==='ending'?'已结束':group.marketingPaused?(group.activeAgentRunId?'正在暂停':'已暂停'):!group.agentEnabled?'已关闭':group.activeAgentRunId?'正在处理消息':'持续待命'
 return <section className={styles.historyPanel}><div className={styles.panelTitle}><div><h2>本群专属 Agent · {state}</h2><p className={styles.meta}>执行账号：{admin?accountLabel(admin):'未绑定管理员'}。每次处理完成后继续待命，上下文随群保存。</p><span className={styles.meta}>最近 50 次处理；选择记录查看本次的全部回复、工具结果与审计。</span></div></div>
  <ErrorNotice error={query.error}/><div className={styles.agentBatchPicker}><label htmlFor="agent-run-picker">处理批次</label><Select id="agent-run-picker" aria-label="处理批次" value={current} loading={query.isLoading} onChange={setSelected} placeholder="尚无处理记录" showSearch optionFilterProp="label" options={runs.map(r=>({value:r.id,label:`${formatTime(r.createdAt??null)} · ${r.id.slice(0,8)} · ${r.steps?.filter(s=>s.name==='send_message').length??0} 次回复调用`}))}/><Space><Button disabled={index<=0} onClick={()=>setSelected(runs[index-1].id)}>较新处理</Button><Button disabled={index<0||index>=runs.length-1} onClick={()=>setSelected(runs[index+1].id)}>较早处理</Button></Space></div>
  {!runs.length&&!query.isLoading&&<p className={styles.meta}>启用 Agent 后，外部成员消息的处理记录会显示在这里。</p>}
  {current&&<div className={styles.panel}><AgentRunDetails key={current} id={current} embedded/></div>}
 </section>
}
