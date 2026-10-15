import {render,screen,within} from '@testing-library/react'
import {Table} from 'antd'
import {test,expect,vi} from 'vitest'
import {buildAccountTableColumns} from './AccountTableColumns'
import type {Account} from '../types'

test('检测中的账号只禁用自己，其他账号仍可操作',()=>{
 const accounts:Account[]=['pending','available'].map(id=>({id,status:'idle',platformUserId:null,rateLimitedUntil:null}))
 render(<Table rowKey="id" pagination={false} dataSource={accounts} columns={buildAccountTableColumns({writable:true,isBusy:id=>id==='pending',groups:[],onAction:vi.fn(),onDetail:vi.fn(),onDelete:vi.fn()})}/> )
 const rows=screen.getAllByRole('row').slice(1)
 expect(within(rows[0]).getByRole('button',{name:'连接并检测'})).toBeDisabled()
 expect(within(rows[1]).getByRole('button',{name:'连接并检测'})).toBeEnabled()
})
