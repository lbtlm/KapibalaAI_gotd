import {render,screen,fireEvent,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import {vi} from 'vitest'
import {GroupComposer} from './GroupActions'
import SequenceProgress from './SequenceProgress'
import SequenceHistory from './SequenceHistory'
import {api} from '../api/client'
import type {Account,Group} from '../types'

const account:Account={id:'acct_long_internal_identity',firstName:'小林',phone:'12345678901',status:'online',platformUserId:'123',rateLimitedUntil:null}
const group:Group={id:'g',gatewayGroupId:'tg',status:'active',creatorAccountId:null,adminAccountId:account.id,agentEnabled:true,autoKickEnabled:false,members:[{accountId:account.id,platformUserId:'123',role:'admin'}],activeSequenceRunId:null,activeAgentRunId:null}
const posted=vi.fn()
const server=setupServer(http.post('/api/groups/g/send',async({request})=>{posted(await request.json());return HttpResponse.json({clientMsgId:'m'})}),http.get('/api/sequence-runs/r',()=>HttpResponse.json({status:'running',currentStepIndex:2,steps:[{index:2,accountRole:'admin',accountId:account.id,text:'您好',status:'pending',deliveryStatus:'queued',scheduledAt:'2026-10-15T10:00:00Z',sentAt:null,resolvedVars:{},varSources:{}}]})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close());afterEach(()=>server.resetHandlers());beforeEach(()=>{posted.mockClear();api.setSession({accessToken:'test',role:'admin'})})
function wrap(child:React.ReactNode){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{child}</QueryClientProvider>)}
test('quick reply chooses a recognizable account and sends only on explicit submit',async()=>{
 const clear=vi.fn()
 wrap(<GroupComposer group={group} accounts={[account]} replyTo={{key:'external-1',sender:'外部成员 456',text:'活动几点开始？'}} onClearReply={clear}/> )
 expect(screen.getByText('小林 · 12345678901')).toBeVisible()
 expect(screen.getByText('活动几点开始？')).toBeVisible();expect(posted).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('消息内容'),{target:{value:'明天下午三点。'}})
 fireEvent.click(screen.getByRole('button',{name:'发送消息'}))
 await waitFor(()=>expect(posted).toHaveBeenCalledWith({accountId:account.id,text:'回复 外部成员 456：\n> 活动几点开始？\n\n明天下午三点。'}))
 expect(clear).toHaveBeenCalledOnce();expect(screen.getByText('小林 · 12345678901')).toBeVisible()
})
test('batch steps distinguish dispatch queue from delay and identify the account',async()=>{
 wrap(<SequenceProgress id="r" accounts={[account]}/>)
 const table=await screen.findByRole('table')
 expect(within(table).getByText('小林 · 12345678901')).toBeVisible()
 expect(within(table).getByText('排队中')).toBeVisible()
 expect(within(table).queryByText(account.id)).not.toBeInTheDocument()
})
test('batch history paginates and switches the selected immutable run',async()=>{
 const batch=(id:string)=>({id,sequenceId:'s',sequenceName:'读书会',status:'finished',createdAt:'2026-10-15T10:00:00Z',completedSteps:2,totalSteps:2})
 server.use(http.get('/api/groups/g/sequence-runs',({request})=>HttpResponse.json(new URL(request.url).searchParams.has('before')?{items:[batch('old')],nextCursor:null}:{items:[batch('r')],nextCursor:'r'})),http.get('/api/sequence-runs/old',()=>HttpResponse.json({status:'finished',currentStepIndex:1,steps:[{index:1,text:'历史批次内容',status:'sent',accountId:account.id,accountRole:'admin'}]})))
 wrap(<SequenceHistory groupId="g" accounts={[account]} initialRunId="r"/>)
 expect(await screen.findByText('您好')).toBeVisible()
 fireEvent.click(screen.getByText('较早批次').closest('button')!)
 fireEvent.click((await screen.findByText('查看明细')).closest('button')!)
 expect(await screen.findByText('历史批次内容')).toBeVisible();expect(screen.queryByText('您好')).not.toBeInTheDocument()
 expect(screen.getByText('较早批次').closest('button')).toBeDisabled()
})
