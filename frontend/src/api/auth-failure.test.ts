import {expect,test} from 'vitest'
import {isWebAuthFailure} from './client'

test.each([
 [401,'SESSION_EXPIRED'],
 [403,'ACCOUNT_SUSPENDED'],
 [409,'ILLEGAL_TRANSITION'],
 [401,'UNKNOWN_BUSINESS_FAILURE']
])('HTTP %s %s不应触发Web续期、自动重放或退出', (status,code)=>{
 expect(isWebAuthFailure(status,code)).toBe(false)
})

test('只有Web认证UNAUTHORIZED 401才允许续期与重试',()=>{
 expect(isWebAuthFailure(401,'UNAUTHORIZED')).toBe(true)
 expect(isWebAuthFailure(403,'UNAUTHORIZED')).toBe(false)
 expect(isWebAuthFailure(undefined,undefined)).toBe(false)
})
