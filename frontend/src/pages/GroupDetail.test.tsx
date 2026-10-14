import {render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {MemoryRouter,Routes,Route} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import {api} from '../api/client'
import GroupDetail from './GroupDetail'
const message=(id:string,text:string,sentAt:string)=>({msgId:id,clientMsgId:null,senderPlatformUserId:'u1',isOwn:false,text,sentAt,deliveryStatus:null,failCode:null})
const server=setupServer(
 http.get('/api/groups/g1',()=>HttpResponse.json({id:'g1',gatewayGroupId:'gw1',status:'active',creatorAccountId:'a1',agentEnabled:false,autoKickEnabled:false,members:[{accountId:'a1',platformUserId:'u1',role:'creator'}],activeSequenceRunId:null,activeAgentRunId:null})),
 http.get('/api/accounts',()=>HttpResponse.json([])),
 http.get('/api/groups/g1/agent-runs',()=>HttpResponse.json([{id:'r1',status:'blocked',endReason:'audit_blocked',groupId:'g1'}])),
 http.get('/api/groups/g1/messages',({request})=>new URL(request.url).searchParams.has('before')?HttpResponse.json({items:[message('m1','较早消息','2026-10-14T01:00:00Z'),message('m2','最新消息','2026-10-14T02:00:00Z')],nextCursor:null}):HttpResponse.json({items:[message('m2','最新消息','2026-10-14T02:00:00Z')],nextCursor:'opaque-cursor'})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
test('viewer群详情展示成员、阻塞运行及游标加载更早无重复',async()=>{api.setSession({accessToken:'token',role:'viewer'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/groups/g1']}><Routes><Route path="/groups/:id" element={<GroupDetail/>}/></Routes></MemoryRouter></QueryClientProvider>);await screen.findByText('最新消息');expect(screen.getByText('群主')).toBeInTheDocument();expect(await screen.findByText('审计阻塞')).toBeInTheDocument();await userEvent.click(screen.getByRole('button',{name:'加载更早'}));expect(await screen.findByText('较早消息')).toBeInTheDocument();expect(screen.getAllByText('最新消息')).toHaveLength(1);expect(screen.queryByRole('button',{name:'发送消息'})).not.toBeInTheDocument()})

