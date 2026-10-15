import {expectTypeOf,test} from 'vitest'
import type {Account} from './types'

test('账号核验来源仅允许真实 Telegram 或尚未核验',()=>{
 expectTypeOf<Account['verificationSource']>().toEqualTypeOf<'telegram'|null|undefined>()
})
