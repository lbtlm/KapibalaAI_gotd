import {useState} from 'react'
import {act,fireEvent,render,screen,waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {expect,test,vi} from 'vitest'
import {App} from 'antd'
import type {Account} from '../types'
import {ApiError} from '../api/client'
import {AccountDeleteButton,DeleteAccountDialog} from './AccountDeletion'

test.each(['idle','disconnected','session_expired','suspended'])('%s可打开单账号删除确认',async status=>{
 const open=vi.fn()
 render(<AccountDeleteButton writable status={status} busy={false} onClick={open}/>)
 await userEvent.click(screen.getByRole('button',{name:'删除账号'}))
 expect(open).toHaveBeenCalledTimes(1)
})

test.each(['online','rate_limited','connecting'])('%s先离线再删除，不能触发确认',async status=>{
 const open=vi.fn()
 render(<AccountDeleteButton writable status={status} busy={false} onClick={open}/>)
 const button=screen.getByRole('button',{name:'删除账号'})
 expect(button).toBeDisabled()
 await userEvent.hover(button.parentElement!)
 await waitFor(()=>expect(screen.getByText('请先将账号离线，再删除。')).toBeVisible())
 expect(open).not.toHaveBeenCalled()
})

test('viewer没有删除入口',()=>{
 render(<AccountDeleteButton writable={false} status="idle" busy={false} onClick={vi.fn()}/>)
 expect(screen.queryByRole('button',{name:'删除账号'})).not.toBeInTheDocument()
})

const account=(id:string,status:Account['status']='idle'):Account=>({id,status,phone:'12025550123',firstName:'Test',platformUserId:null,rateLimitedUntil:null})
function dialog(accounts:Account[],remove:(id:string)=>Promise<unknown>,complete=vi.fn(async(_ids:string[])=>{}),cancel=vi.fn()){
 return render(<App><DeleteAccountDialog accounts={accounts} onDelete={remove} onComplete={complete} onCancel={cancel}/></App>)
}

test('取消不删除，确认显示姓名手机号且悬浮可见ID',async()=>{
 const remove=vi.fn(),cancel=vi.fn()
 dialog([account('selection')],remove,undefined,cancel)
 expect(screen.getByText('Test · 12025550123')).toBeInTheDocument()
 await userEvent.hover(screen.getByText('Test · 12025550123'));expect(await screen.findByRole('tooltip')).toHaveTextContent('selection')
 expect(screen.getByText(/不会删除 Telegram 账号，也不会退出 Telegram 的其他设备/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'取消'}))
 expect(cancel).toHaveBeenCalledTimes(1)
 expect(remove).not.toHaveBeenCalled()
})

test('一次确认批量删除；跳过在线账号，失败不阻塞其余账号，成功ID用于刷新缓存',async()=>{
 const remove=vi.fn(async(id:string)=>{if(id==='occupied')throw new ApiError(409,'ACCOUNT_IN_USE','',{reason:'active_job'})}),complete=vi.fn(async(_ids:string[])=>{})
 dialog([account('one'),account('occupied'),account('expired','session_expired'),account('online','online')],remove,complete)
 fireEvent.click(screen.getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(complete).toHaveBeenCalledTimes(1))
 expect(remove.mock.calls.map(([id])=>id).sort()).toEqual(['expired','occupied','one'])
 expect(complete.mock.calls[0][0]).toEqual(expect.arrayContaining(['one','expired']))
 expect(complete.mock.calls[0][0]).toHaveLength(2)
 await waitFor(()=>expect(screen.getByText(/账号仍有运行中的任务.*ACCOUNT_IN_USE/)).toBeVisible())
 expect(screen.getByRole('button',{name:'确认删除'})).toBeEnabled()
})

test('最多三个删除同时执行，双击不会重复提交，执行期间不能关闭',async()=>{
 const releases:(()=>void)[]=[],complete=vi.fn(async(_ids:string[])=>{}),remove=vi.fn((_id:string)=>new Promise<void>(resolve=>releases.push(resolve)))
 dialog(Array.from({length:5},(_,i)=>account('id-'+i)),remove,complete)
 const button=screen.getByRole('button',{name:'确认删除'})
 fireEvent.click(button);fireEvent.click(button)
 expect(remove).toHaveBeenCalledTimes(3)
 expect(screen.getByRole('button',{name:'取消'})).toBeDisabled()
 await act(async()=>{releases[0]()})
 expect(remove).toHaveBeenCalledTimes(4)
 await act(async()=>{releases[1]();releases[2]()})
 expect(remove).toHaveBeenCalledTimes(5)
 await act(async()=>{releases[3]();releases[4]()})
 expect(complete).toHaveBeenCalledTimes(1)
 expect(new Set(remove.mock.calls.map(([id])=>id)).size).toBe(5)
})

test('全部在线时确认不可用',()=>{
 const remove=vi.fn()
 dialog([account('online','online')],remove)
 expect(screen.getByRole('button',{name:'确认删除'})).toBeDisabled()
 expect(remove).not.toHaveBeenCalled()
})

test('部分删除后只保留失败目标供重试，不重复删除成功目标',async()=>{
 let fail=true
 const remove=vi.fn(async(id:string)=>{if(id==='retry'&&fail){fail=false;throw new ApiError(409,'ACCOUNT_IN_USE','')}})
 function Selection(){const [accounts,setAccounts]=useState([account('done'),account('retry')]);return accounts.length?<DeleteAccountDialog accounts={accounts} onDelete={remove} onComplete={async ids=>{setAccounts(current=>current.filter(a=>!ids.includes(a.id)))}} onCancel={vi.fn()}/>:<span>已完成</span>}
 render(<App><Selection/></App>)
 fireEvent.click(screen.getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(screen.queryByText('done')).not.toBeInTheDocument())
 await waitFor(()=>expect(screen.getByRole('button',{name:'确认删除'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(screen.getByText('已完成')).toBeInTheDocument())
 expect(remove.mock.calls.filter(([id])=>id==='done')).toHaveLength(1)
 expect(remove.mock.calls.filter(([id])=>id==='retry')).toHaveLength(2)
})
