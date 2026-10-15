import {act,fireEvent,render,screen,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import AccountDetails from './AccountDetails'
import {api} from '../api/client'
const server=setupServer(http.get('/api/accounts/account-1',()=>HttpResponse.json({id:'account-1',status:'online',platformUserId:'10001',rateLimitedUntil:null})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close());afterEach(()=>{server.resetHandlers();act(()=>api.setSession(null))})
function mount(){api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><AccountDetails id="account-1" groups={[]} writable busy={false} error={null} onAction={()=>{}} onClose={()=>{}}/></QueryClientProvider>)}
test.each([false,true])('缓存读取失败=%s时不会声称已经成功同步且没有群组',async(fail)=>{
 server.use(http.get('/api/accounts/account-1/joined-groups',()=>fail?HttpResponse.json({error:{code:'NETWORK_TIMEOUT',message:'读取失败'}},{status:504}):HttpResponse.json({items:[],lastSyncedAt:null})))
 mount();const modal=await screen.findByRole('dialog');fireEvent.click(await within(modal).findByRole('tab',{name:'所在群'}))
 expect(await within(modal).findByText(fail?'所在群加载失败，请重试。':'尚未同步所在群。')).toBeInTheDocument()
 expect(within(modal).queryByText('最近一次同步未发现已加入的群组。')).not.toBeInTheDocument()
},10000)
test('成功同步为空时准确说明未发现群组，失败仍显示上次成功空结果',async()=>{
 server.use(http.get('/api/accounts/account-1/joined-groups',()=>HttpResponse.json({items:[],lastSyncedAt:'2026-10-15T04:00:00Z'})),http.post('/api/accounts/account-1/joined-groups/sync',()=>HttpResponse.json({error:{code:'NETWORK_TIMEOUT',message:'同步超时，请稍后重试'}},{status:504})))
 mount();const modal=await screen.findByRole('dialog');fireEvent.click(await within(modal).findByRole('tab',{name:'所在群'}))
 expect(await within(modal).findByText('最近一次同步未发现已加入的群组。')).toBeInTheDocument()
 expect(within(modal).queryByText('尚无成功同步的群组快照。')).not.toBeInTheDocument()
 fireEvent.click(within(modal).getByRole('button',{name:'同步群组'}))
 expect(await within(modal).findByText('同步超时，请稍后重试')).toBeInTheDocument()
 expect(within(modal).getByText('本次同步失败，仍显示上次同步结果。')).toBeInTheDocument()
 expect(within(modal).getByText('最近一次同步未发现已加入的群组。')).toBeInTheDocument()
},10000)
