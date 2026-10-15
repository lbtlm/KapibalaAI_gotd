import {QueryClient} from '@tanstack/react-query'
import {expect,test} from 'vitest'
import {invalidateAccountState} from './account-cache'

test('账号操作结束刷新列表、详情、各筛选日志，保留成功群快照',async()=>{
 const cache=new QueryClient()
 const refreshed=[['accounts'],['account','selection'],['account-logs','selection',undefined,undefined],['account-logs','selection','check','failed']]
 const snapshot=['account-joined-groups','selection']
 try{
  for(const key of [...refreshed,snapshot])cache.setQueryData(key,'persisted-value')
  await invalidateAccountState(cache)
  for(const key of refreshed)expect(cache.getQueryState(key)?.isInvalidated,key.join('/')).toBe(true)
  expect(cache.getQueryState(snapshot)?.isInvalidated).toBe(false)
  expect(cache.getQueryData(snapshot)).toBe('persisted-value')
 }finally{cache.clear()}
})
