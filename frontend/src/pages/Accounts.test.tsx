import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { api } from '../api/client'
import Accounts from './Accounts'
const server = setupServer(http.get('/api/accounts', () => HttpResponse.json(['idle','online','rate_limited','disconnected','suspended','session_expired'].map((status,i) => ({id:'a'+i,status,platformUserId:null,rateLimitedUntil:null})))))
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {server.resetHandlers(); act(()=>api.setSession(null))})
afterAll(() => server.close())
function mount(role: 'admin'|'viewer') { api.setSession({accessToken:'token',role}); render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Accounts/></QueryClientProvider>) }
test('合法转移按钮只显示在对应状态且viewer看不到写操作', async () => {
 mount('admin')
 expect(await screen.findAllByRole('button',{name:'重连'})).toHaveLength(2)
 expect(screen.getAllByRole('button',{name:'标记离线'})).toHaveLength(2)
 expect(screen.getAllByRole('button',{name:'释放账号'})).toHaveLength(2)
})
test('viewer只读账号列表',async()=> {mount('viewer'); await screen.findByText('a0'); expect(screen.queryByRole('button',{name:'重连'})).not.toBeInTheDocument()})
test('操作遭403显示权限反馈',async()=>{ server.use(http.post('/api/accounts/a0/connect',()=>HttpResponse.json({error:{code:'FORBIDDEN',message:'denied'}},{status:403}))); mount('admin'); await screen.findByText('a0'); await userEvent.click(screen.getAllByRole('button',{name:'重连'})[0]); expect(await screen.findByText('没有操作权限')).toBeInTheDocument() })


