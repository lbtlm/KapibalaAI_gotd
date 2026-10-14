import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { ApiClient } from './client'
const server = setupServer()
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
test('并发过期请求共享cookie续期且使用新的access恢复读取', async () => {
  let refreshes = 0
  server.use(http.get('http://localhost/api/accounts', ({ request }) => request.headers.get('Authorization') === 'Bearer renewed' ? HttpResponse.json([{ id: 'a1' }]) : HttpResponse.json({ error: { code: 'UNAUTHORIZED' } }, { status: 401 })), http.post('http://localhost/api/auth/refresh', async ({ request }) => { expect(await request.text()).toBe(''); refreshes++; await new Promise(r => setTimeout(r, 40)); return HttpResponse.json({ accessToken: 'renewed', role: 'admin' }) }))
  const client = new ApiClient('http://localhost')
  client.setSession({ accessToken: 'expired', role: 'admin' })
  const results = await Promise.all([client.request('/api/accounts'), client.request('/api/accounts')])
  expect(results).toEqual([[{ id: 'a1' }], [{ id: 'a1' }]])
  expect(refreshes).toBe(1)
  expect(localStorage.length).toBe(0)
})
