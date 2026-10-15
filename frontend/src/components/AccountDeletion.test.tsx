import {act,render,screen,waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {expect,test,vi} from 'vitest'
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

test('取消确认不会提交，文案仅涉及本系统与本系统凭据',async()=>{
 const confirm=vi.fn(),cancel=vi.fn()
 render(<DeleteAccountDialog id="selection" busy={false} error={null} onConfirm={confirm} onCancel={cancel}/>)
 expect(screen.getByText(/不会删除 Telegram 账号，也不会退出 Telegram 的其他设备/)).toBeInTheDocument()
 await userEvent.click(screen.getByRole('button',{name:'取消'}))
 expect(cancel).toHaveBeenCalledTimes(1)
 expect(confirm).not.toHaveBeenCalled()
})

test('确认窗显示可用的手机号、昵称和固定内部ID以核对目标',()=>{
 render(<DeleteAccountDialog id="selection" phone="显示用手机号" name="显示用昵称" busy={false} error={null} onConfirm={vi.fn()} onCancel={vi.fn()}/>)
 expect(screen.getByText('显示用手机号')).toBeInTheDocument()
 expect(screen.getByText('显示用昵称')).toBeInTheDocument()
 expect(screen.getByText('selection')).toBeInTheDocument()
})

test('确认双击只提交一次，未结束不能取消',async()=>{
 let release!:()=>void
 const wait=new Promise<void>(resolve=>{release=resolve}),confirm=vi.fn(()=>wait),cancel=vi.fn()
 render(<DeleteAccountDialog id="selection" busy={false} error={null} onConfirm={confirm} onCancel={cancel}/>)
 try{
  await userEvent.dblClick(screen.getByRole('button',{name:'确认删除'}))
  expect(confirm).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button',{name:'取消'})).toBeDisabled()
 }finally{await act(async()=>release())}
})

test('删除失败留在确认窗，保留具体错误并允许手动重试',async()=>{
 const failure=new ApiError(503,'ACCOUNT_DELETE_INCOMPLETE',''),confirm=vi.fn(async()=>{throw failure}),cancel=vi.fn()
 const view=render(<DeleteAccountDialog id="selection" busy={false} error={null} onConfirm={confirm} onCancel={cancel}/>)
 await userEvent.click(screen.getByRole('button',{name:'确认删除'}))
 view.rerender(<DeleteAccountDialog id="selection" busy={false} error={failure} onConfirm={confirm} onCancel={cancel}/>)
 await waitFor(()=>expect(screen.getByRole('dialog')).toBeVisible())
 expect(screen.getByText('删除尚未完成，请重试删除')).toBeInTheDocument()
 expect(screen.getByText('ACCOUNT_DELETE_INCOMPLETE')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'确认删除'})).toBeEnabled()
 expect(cancel).not.toHaveBeenCalled()
})
