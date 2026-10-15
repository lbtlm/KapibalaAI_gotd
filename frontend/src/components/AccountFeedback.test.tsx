import {render,screen} from '@testing-library/react'
import {expect,test} from 'vitest'
import {ApiError} from '../api/client'
import {AccountErrorNotice} from './AccountFeedback'

test.each([
 ['SESSION_EXPIRED','会话已失效，无法重新连接'],
 ['ACCOUNT_SUSPENDED','账号已停用，无法重新连接'],
 ['ILLEGAL_TRANSITION','当前账号状态不允许此操作']
])('账号错误%s保留具体码并提供中文说明',(code,message)=>{
 render(<AccountErrorNotice error={new ApiError(401,code,'')}/>)
 expect(screen.getByText(message)).toBeInTheDocument()
 expect(screen.getByText(code,{exact:false})).toBeInTheDocument()
})
