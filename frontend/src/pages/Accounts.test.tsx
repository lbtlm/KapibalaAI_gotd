import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { api } from '../api/client'
import Accounts from './Accounts'
const server = setupServer(http.get('/api/account-groups',()=>HttpResponse.json([])),http.get('/api/accounts', () => HttpResponse.json(['idle','online','rate_limited','disconnected','suspended','session_expired'].map((status,i) => ({id:'a'+i,status,platformUserId:null,rateLimitedUntil:null})))))
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {server.resetHandlers(); act(()=>api.setSession(null))})
afterAll(() => server.close())
function mount(role: 'admin'|'viewer') { api.setSession({accessToken:'token',role}); render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Accounts/></QueryClientProvider>) }
test('合法转移按钮只显示在对应状态且viewer看不到写操作', async () => {
 mount('admin')
 expect(await screen.findAllByLabelText('连接并检测',{selector:'button'})).toHaveLength(2)
 expect(screen.getAllByLabelText('检测',{selector:'button'})).toHaveLength(1)
 expect(screen.getAllByLabelText('标记离线',{selector:'button'})).toHaveLength(2)
 expect(screen.getAllByLabelText('释放账号',{selector:'button'})).toHaveLength(2)
})
test('viewer只读账号列表',async()=> {mount('viewer'); await screen.findByText('a0'); for(const name of ['连接并检测','检测','标记离线','释放账号','上传账号','管理分组','移动到分组'])expect(screen.queryByRole('button',{name})).not.toBeInTheDocument();expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()})
test('Telegram已读取但用户名为空时，列表和详情明确显示未设置',async()=>{
 const account={id:'account-1',status:'online',platformUserId:'10001',rateLimitedUntil:null,username:null,verificationSource:'telegram'}
 server.use(http.get('/api/accounts',()=>HttpResponse.json([account])),http.get('/api/accounts/account-1',()=>HttpResponse.json(account)))
 mount('viewer')
 expect(await screen.findByText('未设置用户名')).toBeInTheDocument()
 fireEvent.click(screen.getByLabelText('详情',{selector:'button'}))
 const modal=await screen.findByRole('dialog')
 expect(await within(modal).findByText('未设置',{exact:true})).toBeInTheDocument()
})
test('操作遭403显示权限反馈',async()=>{ server.use(http.post('/api/accounts/a0/check',()=>HttpResponse.json({error:{code:'FORBIDDEN',message:'denied'}},{status:403}))); mount('admin'); await screen.findByText('a0'); await userEvent.click(screen.getAllByRole('button',{name:'连接并检测'})[0]); expect(await screen.findByText('没有操作权限')).toBeInTheDocument() })

test('账号管理完整显示手机号并按手机号、用户名和ID搜索，统计来自全部账号',async()=>{
 server.use(http.get('/api/accounts',()=>HttpResponse.json([
  {id:'alpha',status:'online',phone:'+8613812345678',nickname:'运营一',username:'operator_one',platformUserId:'12345',rateLimitedUntil:null},
  {id:'beta',status:'idle',phone:'+8613912345678',nickname:'运营二',username:'operator_two',platformUserId:'67890',rateLimitedUntil:null}
 ])))
 mount('viewer')
 expect(await screen.findByRole('heading',{name:'账号管理'})).toBeInTheDocument()
 expect(await screen.findByText('+8613812345678')).toBeInTheDocument()
 const search=screen.getByRole('textbox',{name:'搜索账号'})
 fireEvent.change(search,{target:{value:'operator_two'}})
 expect(screen.queryByText('+8613812345678')).not.toBeInTheDocument()
 expect(screen.getByText('+8613912345678')).toBeInTheDocument()
 fireEvent.change(search,{target:{value:'alpha'}})
 expect(screen.getByText('+8613812345678')).toBeInTheDocument()
 expect(screen.queryByText('+8613912345678')).not.toBeInTheDocument()
 fireEvent.change(search,{target:{value:'1391234'}})
 expect(screen.getByText('+8613912345678')).toBeInTheDocument()
 expect(within(screen.getByLabelText('账号统计')).getByText('2')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'上传账号'})).not.toBeInTheDocument()
})

test('详情打开只读，离线群同步提示先连接，viewer没有写入口',async()=>{
 const writes:string[]=[]
 server.use(
  http.get('/api/accounts/a0',()=>HttpResponse.json({id:'a0',status:'idle',phone:'+8613812345678',firstName:'测试',lastName:'账号',username:'sample',platformUserId:'12345',rateLimitedUntil:null})),
  http.get('/api/accounts/a0/joined-groups',()=>HttpResponse.json({items:[{id:'tg1',title:'已缓存测试群',username:null,type:'supergroup',role:'unknown'}],lastSyncedAt:null})),
  http.get('/api/accounts/a0/logs',()=>HttpResponse.json({items:[{id:'l1',actor:'admin',action:'import',result:'success',errorCode:null,createdAt:'2026-10-15T00:00:00Z'}],nextCursor:null})),
  http.post('/api/accounts/:id/:action',({request})=>{writes.push(request.url);return HttpResponse.json({})})
 )
 mount('viewer');await screen.findByText('a0')
 fireEvent.click(screen.getAllByRole('button',{name:'详情'})[0])
 const modal=await screen.findByRole('dialog')
 expect(await within(modal).findByRole('heading',{name:'测试 账号'})).toBeInTheDocument()
 fireEvent.click(within(modal).getByRole('tab',{name:'所在群'}))
 expect(await within(modal).findByText('已缓存测试群')).toBeInTheDocument()
 expect(within(modal).getByText('角色未知')).toBeInTheDocument()
 expect(within(modal).getByText(/请先连接并检测账号/)).toBeInTheDocument()
 expect(within(modal).queryByRole('button',{name:'同步群组'})).not.toBeInTheDocument()
 fireEvent.click(within(modal).getByRole('tab',{name:'操作日志'}))
 expect(await within(modal).findByText('导入账号')).toBeInTheDocument()
 expect(writes).toEqual([])
},10000)

test('多账号上传预览完整配对，缺失、歧义和不支持文件不得提交',async()=>{
 mount('admin');await screen.findByText('a0');fireEvent.click(screen.getByRole('button',{name:'上传账号'}))
 const modal=await screen.findByRole('dialog')
 const files=within(modal).getByLabelText('账号文件')
 fireEvent.change(files,{target:{files:[new File(['session'],'one.session')]}})
 expect(await within(modal).findByText('缺少同名 .json 文件')).toBeInTheDocument()
 expect(within(modal).getByRole('button',{name:'开始导入'})).toBeDisabled()
 fireEvent.change(files,{target:{files:[new File(['session'],'one.session'),new File(['{}'],'one.json'),new File(['session'],'two.session'),new File(['{}'],'two.json')]}})
 expect(await within(modal).findByText('2 组配对完整')).toBeInTheDocument()
 expect(within(modal).getByRole('button',{name:'开始导入'})).toBeEnabled()
 fireEvent.change(files,{target:{files:[new File(['one'],'one.session'),new File(['two'],'one.session'),new File(['{}'],'one.json'),new File(['unsupported'],'extra.txt')]}})
 expect(await within(modal).findByText('同名文件重复，无法唯一配对')).toBeInTheDocument()
 expect(within(modal).getByText('仅支持 .session 和 .json 文件')).toBeInTheDocument()
 expect(within(modal).getByRole('button',{name:'开始导入'})).toBeDisabled()
},15000)

test('管理员创建重命名删除分组，删除提示账号保留',async()=>{
 let groups=[{id:'g1',name:'运营组',accountCount:2,createdAt:'2026-10-15T00:00:00Z'}]
 server.use(http.get('/api/accounts',()=>HttpResponse.json([])),http.get('/api/account-groups',()=>HttpResponse.json(groups)),http.post('/api/account-groups',async({request})=>{const {name}=await request.json() as {name:string};groups=[...groups,{id:'g2',name,accountCount:0,createdAt:'2026-10-15T00:00:00Z'}];return HttpResponse.json(groups[1],{status:201})}),http.patch('/api/account-groups/g2',async({request})=>{const {name}=await request.json() as {name:string};groups[1].name=name;return HttpResponse.json(groups[1])}),http.delete('/api/account-groups/g1',()=>{groups=groups.filter(g=>g.id!=='g1');return new HttpResponse(null,{status:204})}))
 mount('admin');await screen.findByRole('heading',{name:'账号管理'});fireEvent.click(screen.getByRole('button',{name:'管理分组'}))
 const modal=await screen.findByRole('dialog')
 fireEvent.change(within(modal).getByRole('textbox',{name:'新分组名称'}),{target:{value:'新组'}})
 fireEvent.click(within(modal).getByRole('button',{name:'创建分组'}))
 expect(await within(modal).findByText('新组')).toBeInTheDocument()
 const row=within(modal).getByText('新组').closest('tr')!
 fireEvent.click(within(row).getByRole('button',{name:'重命名'}))
 fireEvent.change(within(row).getByRole('textbox',{name:'分组名称'}),{target:{value:'重命名组'}})
 fireEvent.click(within(row).getByRole('button',{name:'保存'}))
 expect(await within(modal).findByText('重命名组')).toBeInTheDocument()
 fireEvent.click(within(within(modal).getByText('运营组').closest('tr')!).getByRole('button',{name:'删除'}))
 expect(await screen.findByText('删除后账号将移至未分组，不会删除账号。')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(within(modal).queryByText('运营组')).not.toBeInTheDocument())
},15000)

test('明确连接检测传true，在线检测传false，模拟资料不显示真实核验；群同步失败保留缓存',async()=>{
 let account={id:'only',status:'idle',phone:'+8613812345678',phoneVerified:false,verificationSource:null as string|null,platformUserId:null as string|null,rateLimitedUntil:null,lastCheckResult:null as string|null}
 const checks:boolean[]=[]
 server.use(http.get('/api/accounts',()=>HttpResponse.json([account])),http.get('/api/accounts/only',()=>HttpResponse.json(account)),http.post('/api/accounts/only/check',async({request})=>{const v=await request.json() as {connectIfNeeded:boolean};checks.push(v.connectIfNeeded);account={...account,status:'online',verificationSource:'simulated',lastCheckResult:'success',platformUserId:'12345'};return HttpResponse.json(account)}),http.get('/api/accounts/only/joined-groups',()=>HttpResponse.json({items:[{id:'g1',title:'上次完整快照',username:null,type:'group',role:'creator'}],lastSyncedAt:'2026-10-15T00:00:00Z'})),http.post('/api/accounts/only/joined-groups/sync',()=>HttpResponse.json({error:{code:'NETWORK_TIMEOUT',message:'同步超时，请稍后重试'}},{status:504})))
 mount('admin');await screen.findByText(/^only/);fireEvent.click(screen.getByLabelText('详情',{selector:'button'}))
 const modal=await screen.findByRole('dialog');await within(modal).findByText('导入资料，待检测核验')
 expect(checks).toEqual([])
 fireEvent.click(within(modal).getByRole('button',{name:'连接并检测'}))
 await within(modal).findByText('模拟资料，未核验手机号')
 expect(within(modal).getByText('模拟检测成功')).toBeInTheDocument()
 const check=within(modal).getByRole('button',{name:'检测'})
 await waitFor(()=>expect(check).toBeEnabled());fireEvent.click(check)
 await waitFor(()=>expect(checks).toEqual([true,false]))
 fireEvent.click(within(modal).getByRole('tab',{name:'所在群'}))
 await within(modal).findByText('上次完整快照')
 fireEvent.click(within(modal).getByRole('button',{name:'同步群组'}))
 expect(await within(modal).findByText('同步超时，请稍后重试')).toBeInTheDocument()
 expect(within(modal).getByText('上次完整快照')).toBeInTheDocument()
 expect(within(modal).getByText('群主')).toBeInTheDocument()
},15000)

test('批量移动向公开接口提交选择和目标，权限失败保留选择与弹窗',async()=>{
 const account={id:'only',status:'idle',platformUserId:null,rateLimitedUntil:null}
 let attempts=0
 server.use(http.get('/api/accounts',()=>HttpResponse.json([account])),http.post('/api/account-groups/move',async({request})=>{expect(await request.json()).toEqual({accountIds:['only'],groupId:null});attempts++;return attempts===1?HttpResponse.json({error:{code:'FORBIDDEN',message:'denied'}},{status:403}):HttpResponse.json({updated:1})}))
 mount('admin');const row=(await screen.findByText('only')).closest('tr')!
 fireEvent.click(within(row).getByRole('checkbox'))
 fireEvent.click(screen.getByRole('button',{name:'移动到分组'}))
 const modal=await screen.findByRole('dialog')
 fireEvent.click(within(modal).getByRole('button',{name:'确认移动'}))
 expect(await within(modal).findByText('没有操作权限')).toBeInTheDocument()
 expect(screen.getByText('已选 1 个账号')).toBeInTheDocument()
 fireEvent.click(within(modal).getByRole('button',{name:'确认移动'}))
 await waitFor(()=>expect(screen.queryByText('已选 1 个账号')).not.toBeInTheDocument())
 expect(attempts).toBe(2)
},15000)
