import {fireEvent,render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {MemoryRouter} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import Sequences from './Sequences'
import {api} from '../api/client'
const server=setupServer(http.get('/api/groups',()=>HttpResponse.json([{id:'g1',status:'active'}])),http.get('/api/sequences',()=>HttpResponse.json([{id:'s1',name:'通知序列',steps:[{index:1,text:'{event}',accountRole:'admin',delaySeconds:1}]}])),http.post('/api/groups/g1/sequence-runs/preview',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{},stepVars:{}});return HttpResponse.json({error:{code:'UNRESOLVED_PLACEHOLDER',message:'变量缺失',stepIndex:1,key:'event'}},{status:422})}))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
test('预检未通过显示具体步骤和变量且不能启动',async()=>{api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/sequences?group=g1']}><Sequences/></MemoryRouter></QueryClientProvider>);await userEvent.click(await screen.findByRole('button',{name:'预检'}));expect(await screen.findByText(/第 1 步 · 变量 event/)).toBeInTheDocument();expect(screen.queryByRole('button',{name:'确认启动'})).not.toBeInTheDocument()})


test('预检展示逐步变量来源，确认后才启动并显示真实运行进度',async()=>{
 server.use(http.post('/api/groups/g1/sequence-runs/preview',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{event:'周会'},stepVars:{'2':{event:'复盘'}}});return HttpResponse.json({steps:[{index:1,text:'周会',resolvedVars:{event:'周会'},varSources:{event:'default'}},{index:2,text:'复盘',resolvedVars:{event:'复盘'},varSources:{event:'step:2'}}]})}),http.post('/api/groups/g1/sequence-runs',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{event:'周会'},stepVars:{'2':{event:'复盘'}}});return HttpResponse.json({runId:'sr1'},{status:201})}),http.get('/api/sequence-runs/sr1',()=>HttpResponse.json({status:'finished',currentStepIndex:2,steps:[{index:1,status:'sent',scheduledAt:null,sentAt:'2026-10-14T02:00:00Z',clientMsgId:'c1',resolvedVars:{event:'周会'},varSources:{event:'default'}},{index:2,status:'skipped',scheduledAt:null,sentAt:'2026-10-14T02:00:01Z',clientMsgId:null,resolvedVars:{event:'复盘'},varSources:{event:'step:2'}}]})))
 api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/sequences?group=g1']}><Sequences/></MemoryRouter></QueryClientProvider>)
 const defaults=await screen.findByLabelText('默认变量 vars');await userEvent.clear(defaults);fireEvent.change(defaults,{target:{value:'{"event":"周会"}'}})
 const overrides=screen.getByLabelText('逐步覆盖 stepVars');await userEvent.clear(overrides);fireEvent.change(overrides,{target:{value:'{"2":{"event":"复盘"}}'}})
 await userEvent.click(screen.getByRole('button',{name:'预检'}));expect(await screen.findByText('step:2')).toBeInTheDocument();expect(screen.getByText('default')).toBeInTheDocument()
 await userEvent.click(screen.getByRole('button',{name:'确认启动'}));expect(await screen.findByText('当前第 2 步 · 已完成 2 / 2')).toBeInTheDocument()
})


