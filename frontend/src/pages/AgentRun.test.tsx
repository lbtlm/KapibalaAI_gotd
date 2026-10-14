import { render,screen } from '@testing-library/react'
import {MemoryRouter,Routes,Route} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import AgentRun from './AgentRun'
import {api} from '../api/client'
const server=setupServer(http.get('/api/agent-runs/r1',()=>HttpResponse.json({id:'r1',groupId:'g1',status:'blocked',endReason:'audit_blocked',summary:null,steps:[{kind:'protocol_error',name:null,input:null,resultSummary:'格式错误',isError:true,errorCode:'BAD_JSON',auditVerdict:null,rawResponse:'<bad-agent-response>'}]})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
test('审计阻塞醒目显示且协议错误可查看原始响应',async()=>{api.setSession({accessToken:'test',role:'viewer'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/agent-runs/r1']}><Routes><Route path="/agent-runs/:id" element={<AgentRun/>}/></Routes></MemoryRouter></QueryClientProvider>);expect(await screen.findByText('审计阻塞，工具未执行')).toBeInTheDocument();expect(screen.getByText('<bad-agent-response>')).toBeInTheDocument();expect(screen.getByText('BAD_JSON')).toBeInTheDocument()})

