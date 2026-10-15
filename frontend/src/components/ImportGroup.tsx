import {useState} from 'react'
import {useMutation,useMutationState,useQuery,useQueryClient} from '@tanstack/react-query'
import {App,Button,Form,Select,Modal} from 'antd'
import {api} from '../api/client'
import {invalidateAccountState} from '../api/account-cache'
import type {Account,JoinedGroups} from '../types'
import {ErrorNotice} from './Feedback'
import {useAccountMessage} from './AccountFeedback'
import {accountLabel} from './AccountTableColumns'
import {formatTime} from '../hooks'
import styles from '../layout/MainLayout.module.scss'
const roles={creator:'群主',admin:'管理员',member:'成员',unknown:'角色未知'}
export default function ImportGroup({accounts}:{accounts:Account[]}){
 const cache=useQueryClient(),{message}=App.useApp(),showError=useAccountMessage(),[open,setOpen]=useState(false),[accountId,setAccountId]=useState<string>(),[groupId,setGroupId]=useState<string>(),[others,setOthers]=useState<string[]>([])
 const account=accounts.find(a=>a.id===accountId)
 const pending=useMutationState({filters:{mutationKey:['account-action'],status:'pending'},select:m=>(m.state.variables as {account:Account}).account.id})
 const joined=useQuery({queryKey:['account-joined-groups',accountId],queryFn:()=>api.request<JoinedGroups>('/api/accounts/'+encodeURIComponent(accountId!)+'/joined-groups'),enabled:open&&!!accountId})
 const sync=useMutation({mutationKey:['account-action'],mutationFn:({account}:{account:Account})=>api.request<JoinedGroups>('/api/accounts/'+encodeURIComponent(account.id)+'/joined-groups/sync','POST'),onSuccess:(r,v)=>cache.setQueryData(['account-joined-groups',v.account.id],r),onError:e=>showError(e),onSettled:()=>invalidateAccountState(cache)})
 const mutation=useMutation({mutationFn:(data:{gatewayGroupId:string;accountIds:string[]})=>api.request('/api/groups/import','POST',data),onSuccess:()=>{setOpen(false);message.success('群组已导入');cache.invalidateQueries({queryKey:['groups']})},onError:e=>showError(e)})
 const selected=joined.data?.items.find(g=>g.id===groupId),busy=!!accountId&&pending.includes(accountId),online=account?.status==='online'
 const changeAccount=(id:string)=>{setAccountId(id);setGroupId(undefined);setOthers([]);sync.reset()}
 return <><Button aria-label="导入已有群" onClick={()=>{setOpen(true);setAccountId(undefined);setGroupId(undefined);setOthers([])}}>导入已有群</Button><Modal title="导入已有群" open={open} onCancel={()=>!mutation.isPending&&setOpen(false)} footer={null} width={600} style={{top:24,paddingBottom:0}} styles={{container:{maxHeight:'calc(100dvh - 48px)',display:'flex',flexDirection:'column'},body:{minHeight:0,overflow:'auto'}}}>
  <p>选择账号，再从它已加入的群中导入。导入会核验群成员身份，不会自动加群或开启 Agent。</p>
  <Form layout="vertical" onFinish={()=>selected&&accountId&&mutation.mutate({gatewayGroupId:selected.id,accountIds:[accountId,...others]})}>
   <Form.Item label="选择账号" required><Select aria-label="选择账号" placeholder="选择用于读取群信息的账号" value={accountId} onChange={changeAccount} disabled={mutation.isPending} showSearch optionFilterProp="label" options={accounts.map(a=>({value:a.id,label:accountLabel(a)+(a.status==='online'?'':' · 未在线')}))}/></Form.Item>
   {accountId&&<><div className={styles.accountDetailToolbar}><span className={styles.meta}>最近同步：{formatTime(joined.data?.lastSyncedAt??null)}</span><Button disabled={!online||busy||mutation.isPending} loading={sync.isPending} onClick={()=>account&&sync.mutate({account})}>同步群组</Button></div>
    <ErrorNotice error={joined.error}/>{joined.isError&&<Button onClick={()=>joined.refetch()}>重试读取群列表</Button>}
    {!online&&<p className={styles.meta}>请先在账号管理中连接并检测该账号，再同步或导入。当前可查看最近缓存。</p>}
    {sync.isError&&joined.data?.lastSyncedAt&&<p className={styles.meta}>同步失败，仍显示上次成功同步的群。</p>}
    <Form.Item label="已加入的群" required extra="当前支持超级群；普通群请先在 Telegram 升级为超级群。"><Select aria-label="已加入的群" placeholder="选择要导入的群" value={groupId} onChange={setGroupId} disabled={mutation.isPending} loading={joined.isFetching} showSearch optionFilterProp="label" notFoundContent={joined.isFetching?'正在读取群列表':joined.data?.lastSyncedAt?'该账号尚无已同步的群':'尚未同步，请点击“同步群组”。'} options={(joined.data?.items??[]).map(g=>({value:g.id,label:g.title+' · '+roles[g.role]+(g.type==='group'?' · 普通群暂不支持':''),title:g.username?'@'+g.username:g.id,disabled:g.type!=='supergroup'}))}/></Form.Item>
    {!joined.isFetching&&!joined.error&&!joined.data?.items.length&&<p className={styles.meta}>{joined.data?.lastSyncedAt?'最近同步未发现已加入的群。':'尚未同步，请点击“同步群组”。'}</p>}
    <Form.Item label="其他已在群内的账号（可选）" extra="所选账号会一并核验成员身份，未在群内的账号不会自动加入。"><Select aria-label="其他关联账号" mode="multiple" value={others} onChange={setOthers} disabled={!groupId||mutation.isPending} showSearch optionFilterProp="label" options={accounts.filter(a=>a.status==='online'&&a.id!==accountId).map(a=>({value:a.id,label:accountLabel(a)}))}/></Form.Item>
   </>}
   <Button aria-label="确认导入" type="primary" htmlType="submit" disabled={!online||busy||joined.isFetching||selected?.type!=='supergroup'} loading={mutation.isPending}>确认导入</Button>
  </Form></Modal></>
}
