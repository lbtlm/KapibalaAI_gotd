import {QueryClient} from '@tanstack/react-query'
import {expect,test} from 'vitest'
import {invalidateAccountState,removeAccountState} from './account-cache'

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

test('删除只清目标缓存，剪除列表目标并刷新分组数，保留其他目标和历史业务缓存',async()=>{
 const cache=new QueryClient()
 try{
  // Ordinary cache keys and IDs, without account profile or platform responses.
  cache.setQueryData(['accounts'],[{id:'target'},{id:'second'},{id:'other'}])
  const removed=[['account','second'],['account-logs','second'],['account','target'],['account-logs','target','check'],['account-joined-groups','target']]
  const retained=[['account','other'],['account-logs','other'],['account-joined-groups','other'],['groups'],['messages','history']]
  for(const key of [...removed,...retained,['account-groups']])cache.setQueryData(key,'stored-value')
  await removeAccountState(cache,['target','second'])
  expect(cache.getQueryData(['accounts'])).toEqual([{id:'other'}])
  for(const key of removed)expect(cache.getQueryData(key)).toBeUndefined()
  for(const key of retained)expect(cache.getQueryData(key)).toBe('stored-value')
  expect(cache.getQueryState(['accounts'])?.isInvalidated).toBe(true)
  expect(cache.getQueryState(['account-groups'])?.isInvalidated).toBe(true)
 }finally{cache.clear()}
})
