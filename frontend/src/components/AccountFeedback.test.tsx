import {render,screen} from '@testing-library/react'
import {expect,test} from 'vitest'
import {ApiError} from '../api/client'
import {AccountErrorNotice} from './AccountFeedback'

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
