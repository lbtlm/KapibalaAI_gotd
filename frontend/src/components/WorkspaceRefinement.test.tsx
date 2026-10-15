import {fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {App} from 'antd'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import {vi} from 'vitest'
import ImportGroup from './ImportGroup'
import SequenceProgress from './SequenceProgress'
import GroupAgentHistory from './GroupAgentHistory'
import {api} from '../api/client'
import type {Account,Group} from '../types'

const accounts:Account[]=[{id:'a',firstName:'小林',phone:'12345678901',status:'online',platformUserId:'1',rateLimitedUntil:null},{id:'b',firstName:'小陈',phone:'12345678902',status:'online',platformUserId:'2',rateLimitedUntil:null}]
const group:Group={id:'g',gatewayGroupId:'tg-1',title:'测试群',status:'active',creatorAccountId:'a',adminAccountId:'a',agentEnabled:true,autoKickEnabled:false,members:[],activeSequenceRunId:null,activeAgentRunId:null}
const posted=vi.fn()
const server=setupServer(http.post('/api/groups/import',async({request})=>{posted(await request.json());return HttpResponse.json({error:{code:'ACCOUNT_NOT_IN_GROUP',message:'成员状态已变化，请重新同步'}},{status:409})}))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close());afterEach(()=>server.resetHandlers());beforeEach(()=>{posted.mockClear();api.setSession({accessToken:'test',role:'viewer'})})
function mount(child:React.ReactNode,data:[unknown[],unknown][]){const cache=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity},mutations:{retry:false}}});for(const [key,value]of data)cache.setQueryData(key,value);return render(<App><QueryClientProvider client={cache}><MemoryRouter initialEntries={['/groups/g']}><Routes><Route path="/groups/:id" element={child}/></Routes></MemoryRouter></QueryClientProvider></App>)}
test('step variables open outside the row and preserve full values',async()=>{
 mount(<SequenceProgress id="r" accounts={accounts}/>,[[['sequence-run','r'],{status:'finished',currentStepIndex:1,steps:[{index:1,accountId:'a',text:'消息内容',status:'sent',resolvedVars:{location:'很长的地点名称'},varSources:{location:'default'}}]}]])
 expect(screen.queryByText('很长的地点名称')).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'查看 1 个变量'}))
 await waitFor(()=>expect(within(screen.getByRole('dialog')).getByText('很长的地点名称')).toBeVisible())
 expect(within(screen.getAllByRole('table')[0]).queryByText('很长的地点名称')).not.toBeInTheDocument()
})
test('import reads selected account snapshots and clears stale group selection on account change',async()=>{
 mount(<ImportGroup accounts={accounts}/>,[[['account-joined-groups','a'],{items:[{id:'tg-1',title:'读书会',type:'supergroup',role:'admin'}],lastSyncedAt:'2026-10-15T10:00:00Z'}],[['account-joined-groups','b'],{items:[],lastSyncedAt:null}]])
 fireEvent.click(screen.getByText('导入已有群').closest('button')!)
 fireEvent.mouseDown(screen.getByLabelText('选择账号'));fireEvent.click(await screen.findByText('小林 · 12345678901'))
 fireEvent.mouseDown(screen.getByLabelText('已加入的群'));fireEvent.click(await screen.findByText('读书会 · 管理员'))
 fireEvent.click(screen.getByText('确认导入').closest('button')!)
 await waitFor(()=>expect(posted).toHaveBeenCalledWith({gatewayGroupId:'tg-1',accountIds:['a']}))
 fireEvent.mouseDown(screen.getByLabelText('选择账号'));fireEvent.click(await screen.findByText('小陈 · 12345678902'))
 await waitFor(()=>expect(screen.getByText('确认导入').closest('button')).toBeDisabled())
 expect(screen.getByText('同步群组').closest('button')).toBeEnabled()
 expect(screen.getByText('尚未同步，请点击“同步群组”。')).toBeVisible()
})
test('Agent history selects runs and shows every reply',async()=>{
 const steps=[{name:'send_message',kind:'tool_use',toolUseId:'one',input:{text:'第一条模型回复'},resultSummary:'{"deliveryStatus":"accepted"}',auditVerdict:'pass'},{name:'send_message',kind:'tool_use',toolUseId:'two',input:{text:'第二条模型回复'},resultSummary:'{"deliveryStatus":"accepted"}',auditVerdict:'pass'}]
 const runs=[{id:'run-1',groupId:'g',status:'finished',summary:'处理完成',steps},{id:'run-2',groupId:'g',status:'failed',steps:[],summary:'历史错误'}]
 server.use(http.get('/api/agent-runs/:id',({params})=>HttpResponse.json(runs.find(r=>r.id===params.id))))
 mount(<GroupAgentHistory group={group} accounts={accounts}/>,[[['group','g'],group],[['accounts'],accounts],[['agent-runs','g'],runs],[['group-jobs','g'],[]],[['messages','g'],{pages:[{items:[],nextCursor:null}],pageParams:[null]}]])
 expect(await screen.findByText('第一条模型回复')).toBeVisible();expect(screen.getByText('第二条模型回复')).toBeVisible()
 fireEvent.click(screen.getByText('较早处理').closest('button')!)
 expect(await screen.findByText('历史错误')).toBeVisible();expect(screen.queryByText('第一条模型回复')).not.toBeInTheDocument()
})
