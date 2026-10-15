import {App} from 'antd'
import {act,fireEvent,render,screen} from '@testing-library/react'
import {expect,test,vi} from 'vitest'
import {ApiError} from '../api/client'
import {AccountErrorNotice,useAccountMessage} from './AccountFeedback'

test.each([
 ['AUTH_KEY_DUPLICATED','Telegram 检测到授权重复使用，Session 已失效。请重新登录生成新的 Session 后导入'],
 ['SESSION_EXPIRED','会话已失效，无法重新连接'],
 ['ACCOUNT_SUSPENDED','账号已停用，无法重新连接'],
 ['ILLEGAL_TRANSITION','当前账号状态不允许此操作']
])('账号错误%s保留具体码并提供中文说明',(code,message)=>{
 render(<AccountErrorNotice error={new ApiError(401,code,'')}/>)
 expect(screen.getByText(message)).toBeInTheDocument()
 expect(screen.getByText(code,{exact:false})).toBeInTheDocument()
})

test.each([
 ['ACCOUNT_IN_USE','managed_group','账号仍关联未退出的托管群组，暂时无法删除'],
 ['ACCOUNT_DELETE_INCOMPLETE',undefined,'删除尚未完成，请重试删除'],
 ['ACCOUNT_RETIRED',undefined,'账号已移除，请重试删除以完成本系统清理'],
 ['NETWORK_TIMEOUT',undefined,'请求超时，请稍后重试']
])('删除错误%s保留具体码并说明后续操作',(code,reason,message)=>{
 render(<AccountErrorNotice error={new ApiError(409,code,code,{reason})}/>)
 expect(screen.getByText(message)).toBeInTheDocument()
 expect(screen.getByText(code,{exact:false})).toBeInTheDocument()
})

test('操作错误以message显示具体码，3秒后关闭',async()=>{
 vi.useFakeTimers()
 function Trigger(){const showError=useAccountMessage();return <button onClick={()=>showError(new ApiError(504,'NETWORK_TIMEOUT',''),'测试账号')}>操作</button>}
 try{
  render(<App><Trigger/></App>)
  fireEvent.click(screen.getByRole('button',{name:'操作'}))
  expect(screen.getByText('测试账号：请求超时，请稍后重试 · NETWORK_TIMEOUT')).toBeVisible()
  await act(async()=>{await vi.advanceTimersByTimeAsync(4000)})
  expect(screen.queryByText(/测试账号：请求超时/)).not.toBeInTheDocument()
 }finally{vi.useRealTimers()}
})
