import {useEffect,useRef,useState} from 'react'
import {useMutation,useMutationState,useQuery,useQueryClient} from '@tanstack/react-query'
import {Button,Form,Input,Modal,Select,Space,Table} from 'antd'
import {api} from '../api/client'
import {invalidateAccountState,removeAccountState} from '../api/account-cache'
import type {Account,AccountGroup,AccountStatus} from '../types'
import {buildAccountTableColumns,type AccountAction} from '../components/AccountTableColumns'
import {LoadState} from '../components/Feedback'
import {AccountErrorNotice as ErrorNotice,useAccountMessage} from '../components/AccountFeedback'
import AccountDetails from '../components/AccountDetails'
import ImportAccounts from '../components/ImportAccounts'
import AccountGroupManager from '../components/AccountGroupManager'
import {accountDeletionDisabledReason,DeleteAccountDialog} from '../components/AccountDeletion'
import {useSession} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
export default function Accounts(){
 const session=useSession(),cache=useQueryClient(),writable=session?.role==='admin',showError=useAccountMessage()
 const [search,setSearch]=useState(''),[group,setGroup]=useState<string>(),[status,setStatus]=useState<AccountStatus>(),[page,setPage]=useState(1),[pageSize,setPageSize]=useState(20),[height,setHeight]=useState(420)
 const table=useRef<HTMLDivElement>(null)
 const [detail,setDetail]=useState<string>(),[deleting,setDeleting]=useState<string[]>([])
 const [importOpen,setImportOpen]=useState(false)
 const [groupsOpen,setGroupsOpen]=useState(false),[moveOpen,setMoveOpen]=useState(false),[selected,setSelected]=useState<React.Key[]>([]),[moveForm]=Form.useForm()
 const query=useQuery({queryKey:['accounts'],queryFn:()=>api.request<Account[]>('/api/accounts')})
 const groups=useQuery({queryKey:['account-groups'],queryFn:()=>api.request<AccountGroup[]>('/api/account-groups')})
 const move=useMutation({mutationFn:(groupId:string|null)=>api.request('/api/account-groups/move','POST',{accountIds:selected,groupId}),onError:error=>showError(error),onSuccess:async()=>{setSelected([]);setMoveOpen(false);await Promise.all([invalidateAccountState(cache),cache.invalidateQueries({queryKey:['account-groups']})])}})
 const inFlight=useRef(new Set<string>())
 const pending=useMutationState({filters:{mutationKey:['account-action'],status:'pending'},select:m=>(m.state.variables as {account:Account}).account.id})
 const action=useMutation({mutationKey:['account-action'],mutationFn:({account,to}:{account:Account;to:AccountAction})=>to==='check'?api.request('/api/accounts/'+account.id+'/check','POST',{connectIfNeeded:account.status!=='online'}):api.request('/api/accounts/'+account.id+'/transition','POST',{to,expectedFrom:account.status}),onSettled:async(_,error,{account})=>{inFlight.current.delete(account.id);if(error)showError(error,account.phone||account.id);await invalidateAccountState(cache)}})
 const runAction=(account:Account,to:AccountAction)=>{if(inFlight.current.has(account.id))return;inFlight.current.add(account.id);action.mutate({account,to})}
 const finishDelete=async(ids:string[])=>{setDetail(current=>current&&ids.includes(current)?undefined:current);setDeleting(current=>current.filter(id=>!ids.includes(id)));setSelected(current=>current.filter(key=>!ids.includes(String(key))));await removeAccountState(cache,ids)}
 const isBusy=(id:string)=>pending.includes(id)||(move.isPending&&selected.includes(id))||deleting.includes(id)
 const selectedBusy=selected.some(id=>isBusy(String(id)))
 useEffect(()=>{const node=table.current;if(!node)return;const measure=()=>{if(node.clientHeight)setHeight(Math.max(180,node.clientHeight-112))};const observer=new ResizeObserver(measure);observer.observe(node);measure();return()=>observer.disconnect()},[])
 const accounts=query.data??[],needle=search.trim().replace(/^@/,'').toLowerCase()
 const filtered=accounts.filter(a=>(!needle||[a.id,a.phone,a.username,a.platformUserId].some(v=>v?.toLowerCase().includes(needle)))&&(!group||(group==='ungrouped'?!a.groupId:a.groupId===group))&&(!status||a.status===status))
 const deleteAccounts=accounts.filter(account=>deleting.includes(account.id))
 const requestDelete=(account:Account)=>{if(!writable||isBusy(account.id)||accountDeletionDisabledReason(account.status))return;setDeleting([account.id])}
 useEffect(()=>{if(!query.data)return;const ids=new Set(query.data.map(account=>account.id));setSelected(current=>current.some(key=>!ids.has(String(key)))?current.filter(key=>ids.has(String(key))):current);setDetail(current=>current&&!ids.has(current)?undefined:current);setDeleting(current=>current.some(id=>!ids.has(id))?current.filter(id=>ids.has(id)):current)},[query.data])
 useEffect(()=>setPage(current=>Math.min(current,Math.max(1,Math.ceil(filtered.length/pageSize)))),[filtered.length,pageSize])
 useEffect(()=>{if(group&&group!=='ungrouped'&&groups.data&&!groups.data.some(g=>g.id===group)){setGroup(undefined);setPage(1)}},[group,groups.data])
 const filterChange=()=>setPage(1)
 return <section aria-label="账号管理工作区" className={styles.accountsPage}>
  <div className={styles.pageHeading}><div><h1>账号管理</h1><p>导入账号、核验连接与资料，按分组管理。</p></div>{writable&&<Space><Button onClick={()=>setGroupsOpen(true)}>管理分组</Button><Button type="primary" onClick={()=>setImportOpen(true)}>上传账号</Button></Space>}</div>
  <ErrorNotice error={query.error??groups.error}/>{(query.isError||groups.isError)&&<div><Button onClick={()=>{if(query.isError)void query.refetch();if(groups.isError)void groups.refetch()}}>重试加载</Button></div>}
  <div className={styles.toolbar}><div className={styles.summary} aria-label="账号统计"><span><strong>{accounts.length}</strong>个账号</span><span><strong>{accounts.filter(a=>a.status==='online').length}</strong>在线</span><span><strong>{accounts.filter(a=>a.status==='rate_limited').length}</strong>限流中</span><span><strong>{accounts.filter(a=>['suspended','session_expired'].includes(a.status)).length}</strong>不可用</span></div><span className={styles.meta}>{writable?'终态账号不可重连':'只读模式'}</span></div>
  <div className={styles.accountFilters}><Space wrap><Input aria-label="搜索账号" placeholder="搜索手机号 / 用户名 / ID" allowClear value={search} onChange={e=>{setSearch(e.target.value);filterChange()}} className={styles.accountSearch}/><Select aria-label="筛选分组" placeholder="全部分组" allowClear value={group} onChange={v=>{setGroup(v);filterChange()}} options={[{value:'ungrouped',label:'未分组'},...(groups.data??[]).map(g=>({value:g.id,label:g.name}))]} className={styles.accountSelect}/><Select aria-label="筛选状态" placeholder="全部状态" allowClear value={status} onChange={v=>{setStatus(v);filterChange()}} options={[['idle','空闲'],['online','在线'],['disconnected','已离线'],['rate_limited','限流中'],['suspended','已停用'],['session_expired','会话失效']].map(([value,label])=>({value,label}))} className={styles.accountSelect}/></Space><span className={styles.meta}>筛选结果 {filtered.length} 个账号</span></div>
  {writable&&selected.length>0&&<div className={styles.accountBulk}><span>已选 {selected.length} 个账号</span><Space><Button onClick={()=>setSelected([])}>取消选择</Button><Button disabled={selectedBusy} onClick={()=>{move.reset();moveForm.setFieldsValue({groupId:'ungrouped'});setMoveOpen(true)}}>移动到分组</Button><Button danger disabled={selectedBusy} onClick={()=>setDeleting(selected.map(String))}>批量删除</Button></Space></div>}<div ref={table} className={styles.tableShell+' '+styles.accountsTable}><Table size="small" rowKey="id" rowSelection={writable?{selectedRowKeys:selected,onChange:setSelected,preserveSelectedRowKeys:true}:undefined} loading={query.isLoading} locale={{emptyText:<LoadState empty={!query.isLoading} description={query.isError?'账号加载失败，请重试。':needle||group||status?'没有符合筛选条件的账号。':'上传真实账号的同名 .session 和 .json 文件以导入账号。'}/>}} dataSource={filtered} columns={buildAccountTableColumns({writable,isBusy,groups:groups.data??[],onAction:runAction,onDetail:account=>setDetail(account.id),onDelete:requestDelete})} scroll={{x:1300,y:height}} pagination={{current:page,pageSize,total:filtered.length,showSizeChanger:true,pageSizeOptions:[20,50,100],showTotal:total=>'共 '+total+' 个账号',onChange:(p,s)=>{setPage(p);setPageSize(s)}}}/></div>
  {detail&&<AccountDetails key={detail} id={detail} groups={groups.data??[]} writable={writable} busy={isBusy(detail)} onAction={runAction} onDelete={requestDelete} onClose={()=>setDetail(undefined)}/>}
  {writable&&deleteAccounts.length>0&&<DeleteAccountDialog accounts={deleteAccounts} onDelete={id=>api.request<void>('/api/accounts/'+encodeURIComponent(id),'DELETE')} onComplete={finishDelete} onCancel={()=>setDeleting([])}/>}
  {writable&&importOpen&&<ImportAccounts groups={groups.data??[]} onClose={()=>setImportOpen(false)}/>}
  {writable&&groupsOpen&&<AccountGroupManager groups={groups.data??[]} error={groups.error} onClose={()=>setGroupsOpen(false)}/>}
  {writable&&<Modal title="批量移动分组" open={moveOpen} footer={null} onCancel={()=>{if(!move.isPending)setMoveOpen(false)}}><p>将所选 {selected.length} 个账号移至一个分组。</p><Form form={moveForm} layout="vertical" initialValues={{groupId:'ungrouped'}} onFinish={v=>move.mutate(v.groupId==='ungrouped'?null:v.groupId)}><Form.Item name="groupId" label="目标分组" rules={[{required:true,message:'请选择目标分组'}]}><Select aria-label="目标分组" options={[{value:'ungrouped',label:'未分组'},...(groups.data??[]).map(g=>({value:g.id,label:g.name}))]}/></Form.Item><Button type="primary" htmlType="submit" loading={move.isPending}>确认移动</Button></Form></Modal>}
 </section>
}
