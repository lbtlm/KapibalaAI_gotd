import {act,fireEvent,render,screen,waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {MemoryRouter,Routes,Route} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import Sequences from './Sequences'
import {api} from '../api/client'
import MainLayout from '../layout/MainLayout'
const server=setupServer(http.get('/api/groups',()=>HttpResponse.json([{id:'g1',status:'active'}])),http.get('/api/sequences',()=>HttpResponse.json([{id:'s1',name:'通知序列',steps:[{index:1,text:'{event}',accountRole:'admin',delaySeconds:1}]}])),http.post('/api/groups/g1/sequence-runs/preview',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{},stepVars:{}});return HttpResponse.json({error:{code:'UNRESOLVED_PLACEHOLDER',message:'变量缺失',stepIndex:1,key:'event'}},{status:422})}))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
afterEach(()=>server.resetHandlers())

test.each(['vars','stepVars','sequence','group'])('延迟预检期间修改 %s 后旧响应不能恢复确认',async(field)=>{
 let release!:()=>void,arrived!:()=>void
 const pending=new Promise<void>(resolve=>{release=resolve}),started=new Promise<void>(resolve=>{arrived=resolve})
 let replies=0
 server.use(http.get('/api/groups',()=>HttpResponse.json([{id:'g1',status:'active'},{id:'g2',status:'active'}])),http.get('/api/sequences',()=>HttpResponse.json([{id:'s1',name:'通知序列',steps:[]},{id:'s2',name:'第二序列',steps:[]}])),http.post('/api/groups/g1/sequence-runs/preview',async()=>{arrived();await pending;return HttpResponse.json({steps:[{index:1,text:'旧输入',resolvedVars:{},varSources:{}}]})}))
 const listener=()=>{replies++};server.events.on('response:mocked',listener)
 api.setSession({accessToken:'token',role:'admin'})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter initialEntries={['/sequences?group=g1']}><Sequences/></MemoryRouter></QueryClientProvider>)
 await userEvent.click(await screen.findByRole('button',{name:'预检'}));await started
 if(field==='vars'||field==='stepVars')fireEvent.change(screen.getByLabelText(field==='vars'?'默认变量 vars':'逐步覆盖 stepVars'),{target:{value:field==='vars'?'{} ':'{"1":{}}'}})
 else{await userEvent.click(screen.getAllByRole('combobox')[field==='group'?0:1]);await userEvent.click(await screen.findByText(field==='group'?'g2':'第二序列',{selector:'.ant-select-item-option-content'}))}
 const before=replies
 await act(async()=>{release();await pending})
 await waitFor(()=>expect(replies).toBeGreaterThan(before))
 await waitFor(()=>expect(screen.getByRole('button',{name:'预检'})).toHaveAttribute('aria-busy','false'))
 expect(screen.queryByRole('button',{name:'确认启动'})).not.toBeInTheDocument()
 server.events.removeListener('response:mocked',listener)
})
test('预检未通过显示具体步骤和变量且不能启动',async()=>{api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/sequences?group=g1']}><Sequences/></MemoryRouter></QueryClientProvider>);await userEvent.click(await screen.findByRole('button',{name:'预检'}));expect(await screen.findByText(/第 1 步 · 变量 event/)).toBeInTheDocument();expect(screen.queryByRole('button',{name:'确认启动'})).not.toBeInTheDocument()})


test('预检展示逐步变量来源，确认后才启动并显示真实运行进度',async()=>{
 server.use(http.post('/api/groups/g1/sequence-runs/preview',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{event:'周会'},stepVars:{'2':{event:'复盘'}}});return HttpResponse.json({steps:[{index:1,text:'周会',resolvedVars:{event:'周会'},varSources:{event:'default'}},{index:2,text:'复盘',resolvedVars:{event:'复盘'},varSources:{event:'step:2'}}]})}),http.post('/api/groups/g1/sequence-runs',async({request})=>{expect(await request.json()).toEqual({sequenceId:'s1',vars:{event:'周会'},stepVars:{'2':{event:'复盘'}}});return HttpResponse.json({runId:'sr1'},{status:201})}),http.get('/api/sequence-runs/sr1',()=>HttpResponse.json({status:'finished',currentStepIndex:2,steps:[{index:1,status:'sent',scheduledAt:null,sentAt:'2026-10-14T02:00:00Z',clientMsgId:'c1',resolvedVars:{event:'周会'},varSources:{event:'default'}},{index:2,status:'skipped',scheduledAt:null,sentAt:'2026-10-14T02:00:01Z',clientMsgId:null,resolvedVars:{event:'复盘'},varSources:{event:'step:2'}}]})))
 api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/sequences?group=g1']}><Sequences/></MemoryRouter></QueryClientProvider>)
 const defaults=await screen.findByLabelText('默认变量 vars');await userEvent.clear(defaults);fireEvent.change(defaults,{target:{value:'{"event":"周会"}'}})
 const overrides=screen.getByLabelText('逐步覆盖 stepVars');await userEvent.clear(overrides);fireEvent.change(overrides,{target:{value:'{"2":{"event":"复盘"}}'}})
 await userEvent.click(screen.getByRole('button',{name:'预检'}));expect(await screen.findByText('step:2')).toBeInTheDocument();expect(screen.getByText('default')).toBeInTheDocument()
 await userEvent.click(screen.getByRole('button',{name:'确认启动'}));expect(await screen.findByText('当前第 2 步 · 已完成 2 / 2')).toBeInTheDocument()
})

test('R1完成后保留结果，WS通知群有R2时自动显示当前运行并在结束后保留R2',async()=>{
 class Socket {
  static current:Socket
  onopen:(()=>void)|null=null;onmessage:((event:{data:string})=>void)|null=null;onclose:(()=>void)|null=null
  constructor(){Socket.current=this}send(){}close(){}
  emit(frame:unknown){this.onmessage?.({data:JSON.stringify(frame)})}
 }
 vi.stubGlobal('WebSocket',Socket)
 let active:string|null=null,status='running'
 server.use(http.get('/api/groups',()=>HttpResponse.json([{id:'g1',status:'active',activeSequenceRunId:active}])),http.post('/api/groups/g1/sequence-runs/preview',()=>HttpResponse.json({steps:[]})),http.post('/api/groups/g1/sequence-runs',()=>{active='r1';return HttpResponse.json({runId:'r1'})}),http.get('/api/sequence-runs/:id',()=>HttpResponse.json({status,currentStepIndex:1,steps:[]})))
 api.setSession({accessToken:'token',role:'admin'})
 const cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 try{
  render(<QueryClientProvider client={cache}><MemoryRouter initialEntries={['/sequences?group=g1']}><Routes><Route element={<MainLayout/>}><Route path="sequences" element={<Sequences/>}/></Route></Routes></MemoryRouter></QueryClientProvider>)
  await userEvent.click(await screen.findByRole('button',{name:'预检'}));await userEvent.click(await screen.findByRole('button',{name:'确认启动'}));expect(await screen.findByText('r1',{exact:true})).toBeInTheDocument()
  await act(async()=>{Socket.current.emit({type:'auth',success:true});active=null;status='finished';Socket.current.emit({seq:1,type:'sequence_run',payload:{groupId:'g1',runId:'r1'}})})
  expect(await screen.findByText('已完成',{exact:true})).toBeInTheDocument();expect(screen.getByText('r1',{exact:true})).toBeInTheDocument()
  await act(async()=>{active='r2';status='running';Socket.current.emit({seq:2,type:'sequence_run',payload:{groupId:'g1',runId:'r2'}})})
  expect(await screen.findByText('r2',{exact:true})).toBeInTheDocument();expect(screen.queryByText('r1',{exact:true})).not.toBeInTheDocument()
  await act(async()=>{active=null;status='finished';Socket.current.emit({seq:3,type:'sequence_run',payload:{groupId:'g1',runId:'r2'}})})
  expect(await screen.findByText('已完成',{exact:true})).toBeInTheDocument();expect(screen.getByText('r2',{exact:true})).toBeInTheDocument()
 }finally{cache.clear();vi.unstubAllGlobals()}
})


