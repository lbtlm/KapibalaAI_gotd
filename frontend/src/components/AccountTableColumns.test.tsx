import {render,screen,within} from '@testing-library/react'
import {Table} from 'antd'
import {test,expect,vi} from 'vitest'
import {buildAccountTableColumns} from './AccountTableColumns'
import type {Account} from '../types'

test('检测失败只保留最近检测摘要，不在操作列长期显示错误框',()=>{
 const account:Account={id:'failed',status:'session_expired',platformUserId:null,rateLimitedUntil:null,lastCheckResult:'failed',lastCheckError:'AUTH_KEY_DUPLICATED'}
 render(<Table rowKey="id" pagination={false} dataSource={[account]} columns={buildAccountTableColumns({writable:true,isBusy:()=>false,groups:[],onAction:vi.fn(),onDetail:vi.fn(),onDelete:vi.fn()})}/> )
 expect(screen.getByText('检测失败 · AUTH_KEY_DUPLICATED')).toBeVisible()
 expect(screen.queryByText(/Telegram 检测到授权重复使用/)).not.toBeInTheDocument()
})

test('检测中的账号只禁用自己，其他账号仍可操作',()=>{
 const accounts:Account[]=['pending','available'].map(id=>({id,status:'idle',platformUserId:null,rateLimitedUntil:null}))
 render(<Table rowKey="id" pagination={false} dataSource={accounts} columns={buildAccountTableColumns({writable:true,isBusy:id=>id==='pending',groups:[],onAction:vi.fn(),onDetail:vi.fn(),onDelete:vi.fn()})}/> )
 const rows=screen.getAllByRole('row').slice(1)
 expect(within(rows[0]).getByRole('button',{name:'连接并检测'})).toBeDisabled()
 expect(within(rows[1]).getByRole('button',{name:'连接并检测'})).toBeEnabled()
})
