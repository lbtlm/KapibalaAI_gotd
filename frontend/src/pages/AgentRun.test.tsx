import userEvent from '@testing-library/user-event'
import { render,screen,waitFor,within } from '@testing-library/react'
import {MemoryRouter,Routes,Route} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import AgentRun from './AgentRun'
import {api} from '../api/client'
const server=setupServer(http.get('/api/agent-runs/r1',()=>HttpResponse.json({id:'r1',groupId:'g1',status:'blocked',endReason:'audit_blocked',summary:null,steps:[{kind:'protocol_error',name:null,input:null,resultSummary:'格式错误',isError:true,errorCode:'BAD_JSON',auditVerdict:null,rawResponse:'<bad-agent-response>'}]})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
afterEach(()=>server.resetHandlers())
test('full tool JSON is available on hover and click beyond the 200 character summary',async()=>{
 const result=JSON.stringify({messages:[{text:'全文'.repeat(150)+'末尾验证'}],truncated:false})
 server.use(http.get('/api/agent-runs/r1',()=>HttpResponse.json({id:'r1',groupId:'g1',status:'finished',steps:[{kind:'tool_use',name:'get_recent_messages',input:{limit:20},resultSummary:result.slice(0,200),result,isError:false}]})))
 api.setSession({accessToken:'test',role:'viewer'})
 render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/agent-runs/r1']}><Routes><Route path="/agent-runs/:id" element={<AgentRun/>}/></Routes></MemoryRouter></QueryClientProvider>)
 expect(await screen.findByText('读取 1 条消息')).toBeVisible()
 await userEvent.hover(screen.getByRole('button',{name:'完整结果'}))
 expect(await screen.findByRole('tooltip')).toHaveTextContent('末尾验证')
 await userEvent.click(screen.getByRole('button',{name:'完整结果'}))
 expect(await screen.findByRole('dialog')).toHaveTextContent('末尾验证')
})
test('审计阻塞以表格展示且协议错误可查看原始响应',async()=>{api.setSession({accessToken:'test',role:'viewer'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/agent-runs/r1']}><Routes><Route path="/agent-runs/:id" element={<AgentRun/>}/></Routes></MemoryRouter></QueryClientProvider>);expect(await screen.findByText('审计阻塞，工具未执行')).toBeInTheDocument();expect(screen.getByRole('table')).toBeInTheDocument();expect(screen.queryByText('<bad-agent-response>')).not.toBeInTheDocument();await userEvent.click(screen.getByText('原始响应体'));await waitFor(()=>expect(within(screen.getByRole('dialog')).getByText('<bad-agent-response>')).toBeVisible());expect(screen.getByText('BAD_JSON')).toBeInTheDocument()})
