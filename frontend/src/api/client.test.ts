import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { ApiClient } from './client'
const server = setupServer()
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
test('并发过期请求共享cookie续期且使用新的access恢复读取', async () => {
  let refreshes = 0
  server.use(http.get('http://localhost/api/protected-resource', ({ request }) => request.headers.get('Authorization') === 'Bearer renewed' ? HttpResponse.json({ value: 'protected' }) : HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })), http.post('http://localhost/api/auth/refresh', async ({ request }) => { expect(await request.text()).toBe(''); refreshes++; await new Promise(r => setTimeout(r, 40)); return HttpResponse.json({ accessToken: 'renewed', role: 'admin' }) }))
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'expired', role: 'admin' })
  const results = await Promise.all([client.request('/api/protected-resource'), client.request('/api/protected-resource')])
  expect(results).toEqual([{ value: 'protected' }, { value: 'protected' }])
  expect(refreshes).toBe(1)
  expect(localStorage.length).toBe(0)
})

function barrier() {
  let release!: () => void
  const promise = new Promise<void>(resolve => { release = resolve })
  return { promise, release }
}

test('业务401原样返回且不续期、不重放、不清除Web登录',async()=>{
 let requests=0,refreshes=0
 server.use(
  http.post('http://localhost/api/protected-resource',()=>{requests++;return HttpResponse.json({error:{code:'SESSION_EXPIRED',message:'credential expired'}},{status:401})}),
  http.post('http://localhost/api/auth/refresh',()=>{refreshes++;return HttpResponse.json({accessToken:'renewed',role:'admin'})})
 )
 const client=new ApiClient('http://localhost')
 client.setSession({accessToken:'web-session',role:'admin'})
 await expect(client.request('/api/protected-resource','POST',{value:'one-operation'})).rejects.toMatchObject({status:401,code:'SESSION_EXPIRED'})
 expect(requests).toBe(1)
 expect(refreshes).toBe(0)
 expect(client.getSession()).toEqual({accessToken:'web-session',role:'admin'})
})
test('旧会话写请求的迟到401不会在登录B后使用B身份重放', async () => {
  const started = barrier(), reply = barrier()
  const writes: (string | null)[] = []
  server.use(
    http.post('http://localhost/api/protected-resource', async ({ request }) => {
      const token = request.headers.get('Authorization')
      writes.push(token)
      if (token === 'Bearer session-A') {
        started.release()
        await reply.promise
        return HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })
      }
      return HttpResponse.json({ value: 'unexpected-B-write' }, { status: 202 })
    }),
    http.post('http://localhost/api/auth/logout', () => HttpResponse.json({})),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'admin' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'session-A', role: 'admin' })
  const oldWrite = client.request('/api/protected-resource', 'POST', { value: 'session-A-write' })
    .then(value => ({ value, error: undefined }), error => ({ value: undefined, error }))
  await started.promise
  await client.logout()
  await client.login('B', 'demo')
  reply.release()
  const result = await oldWrite
  expect(result.error?.code).toBe('SESSION_CHANGED')
  expect(writes).toEqual(['Bearer session-A'])
  expect(client.getSession()).toEqual({ accessToken: 'session-B', role: 'admin' })
})


test.each(['logout', 'login-B'] as const)('待完成续期在%s后不能恢复旧会话', async change => {
  const started = barrier(), reply = barrier()
  server.use(
    http.post('http://localhost/api/auth/refresh', async () => {
      started.release()
      await reply.promise
      return HttpResponse.json({ accessToken: 'renewed-A', role: 'admin' })
    }),
    http.post('http://localhost/api/auth/logout', () => HttpResponse.json({})),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'viewer' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'session-A', role: 'admin' })
  const oldRefresh = client.refresh().then(value => ({ value, error: undefined }), error => ({ value: undefined, error }))
  await started.promise
  const changed = change === 'logout' ? client.logout() : client.login('B', 'demo')
  reply.release()
  await changed
  const result = await oldRefresh
  expect(result.error?.code).toBe('SESSION_CHANGED')
  expect(client.getSession()).toEqual(change === 'logout' ? null : { accessToken: 'session-B', role: 'viewer' })
})


test('退出时access已过期仍续期后撤销同一会话，结束后保持退出', async () => {
  const logoutTokens: (string | null)[] = []
  server.use(
    http.post('http://localhost/api/auth/logout', ({ request }) => {
      const token = request.headers.get('Authorization')
      logoutTokens.push(token)
      return token === 'Bearer renewed-A' ? HttpResponse.json({}) : HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })
    }),
    http.post('http://localhost/api/auth/refresh', () => HttpResponse.json({ accessToken: 'renewed-A', role: 'admin' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'expired-A', role: 'admin' })
  await client.logout()
  expect(logoutTokens).toEqual(['Bearer expired-A', 'Bearer renewed-A'])
  expect(client.getSession()).toBeNull()
})


test('旧会话重试的迟到401不能清除新B会话', async () => {
  const retryStarted = barrier(), retryReply = barrier()
  server.use(
    http.post('http://localhost/api/protected-resource', async ({ request }) => {
      if (request.headers.get('Authorization') === 'Bearer renewed-A') {
        retryStarted.release()
        await retryReply.promise
      }
      return HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })
    }),
    http.post('http://localhost/api/auth/refresh', () => HttpResponse.json({ accessToken: 'renewed-A', role: 'admin' })),
    http.post('http://localhost/api/auth/logout', () => HttpResponse.json({})),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'viewer' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'expired-A', role: 'admin' })
  const oldWrite = client.request('/api/protected-resource', 'POST', { value: 'session-A-write' }).catch(error => error)
  await retryStarted.promise
  await client.logout()
  await client.login('B', 'demo')
  retryReply.release()
  expect(await oldWrite).toMatchObject({ code: 'SESSION_CHANGED' })
  expect(client.getSession()).toEqual({ accessToken: 'session-B', role: 'viewer' })
})
test('旧续期的迟到失败不能清除新B会话', async () => {
  const started = barrier(), reply = barrier()
  server.use(
    http.post('http://localhost/api/auth/refresh', async () => {
      started.release()
      await reply.promise
      return HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })
    }),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'viewer' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'session-A', role: 'admin' })
  const oldRefresh = client.refresh().catch(error => error)
  await started.promise
  const login = client.login('B', 'demo')
  reply.release()
  await login
  expect(await oldRefresh).toMatchObject({ code: 'SESSION_CHANGED' })
  expect(client.getSession()).toEqual({ accessToken: 'session-B', role: 'viewer' })
})
test('旧续期完成后B会话仍共享单飞续期', async () => {
  const oldStarted = barrier(), oldReply = barrier(), newStarted = barrier(), newReply = barrier()
  let refreshes = 0
  server.use(
    http.post('http://localhost/api/auth/refresh', async () => {
      refreshes++
      if (refreshes === 1) {
        oldStarted.release()
        await oldReply.promise
        return HttpResponse.json({ accessToken: 'renewed-A', role: 'admin' })
      }
      newStarted.release()
      await newReply.promise
      return HttpResponse.json({ accessToken: 'renewed-B', role: 'viewer' })
    }),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'viewer' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'session-A', role: 'admin' })
  const oldRefresh = client.refresh().catch(error => error)
  await oldStarted.promise
  const login = client.login('B', 'demo')
  oldReply.release()
  expect(await oldRefresh).toMatchObject({ code: 'SESSION_CHANGED' })
  await login
  const firstB = client.refresh()
  await newStarted.promise
  const secondB = client.refresh()
  newReply.release()
  expect(await Promise.all([firstB, secondB])).toEqual([
    { accessToken: 'renewed-B', role: 'viewer' },
    { accessToken: 'renewed-B', role: 'viewer' }
  ])
  expect(refreshes).toBe(2)
  expect(client.getSession()).toEqual({ accessToken: 'renewed-B', role: 'viewer' })
})
test('A退出请求完成时不会清除其间登录的B会话', async () => {
  const started = barrier(), reply = barrier()
  server.use(
    http.post('http://localhost/api/auth/logout', async () => { started.release(); await reply.promise; return HttpResponse.json({}) }),
    http.post('http://localhost/api/auth/login', () => HttpResponse.json({ accessToken: 'session-B', role: 'viewer' }))
  )
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'session-A', role: 'admin' })
  const logout = client.logout()
  await started.promise
  const login = client.login('B', 'demo')
  reply.release()
  await logout
  await login
  expect(client.getSession()).toEqual({ accessToken: 'session-B', role: 'viewer' })
})
