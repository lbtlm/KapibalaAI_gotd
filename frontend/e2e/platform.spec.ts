import {test,expect,type APIRequestContext,type WebSocketRoute} from '@playwright/test'
const backend=process.env.E2E_API_URL??'http://127.0.0.1:8080'
const gateway=process.env.E2E_GATEWAY_URL??'http://127.0.0.1:8081'
async function checked<T = unknown>(response:Awaited<ReturnType<APIRequestContext['get']>>){const text=await response.text();expect(response.ok(),text).toBeTruthy();return (text?JSON.parse(text):{}) as T}
async function createTestGroup(request:APIRequestContext){
 const health=await request.get(backend+'/api/health')
 expect(health.ok(),'需要先启动 PostgreSQL、backend、fake gateway、scripted Agent；见 frontend/README.md').toBeTruthy()
 const {accessToken}=await checked<{accessToken:string}>(await request.post(backend+'/api/auth/login',{data:{username:'admin',password:'admin'}}))
 const headers={Authorization:'Bearer '+accessToken}
 const accounts=await checked(await request.get(backend+'/api/accounts',{headers})) as {id:string;status:string}[]
 expect(accounts.length).toBeGreaterThanOrEqual(2)
 const chosen=accounts.filter(a=>['idle','disconnected','online'].includes(a.status)).slice(0,2)
 expect(chosen).toHaveLength(2)
 for(const account of chosen)if(account.status!=='online')await checked(await request.post(backend+'/api/accounts/'+account.id+'/connect',{headers}))
 const {jobId}=await checked<{jobId:string}>(await request.post(backend+'/api/groups',{headers,data:{creatorAccountId:chosen[0].id,memberAccountIds:[chosen[1].id]}}))
 let createdGroupId: string | undefined
 await expect.poll(async()=>{const job=await checked<{status:string;groupId?:string}>(await request.get(backend+'/api/jobs/'+jobId,{headers}));createdGroupId=job.groupId;return job.status},{timeout:20000}).toBe('finished')
 expect(createdGroupId,'本次成功建群任务必须返回关联群ID').toBeTruthy()
 const group=await checked<{id:string;gatewayGroupId:string}>(await request.get(backend+'/api/groups/'+createdGroupId,{headers}))
 expect(group.id).toBe(createdGroupId)
 return {group,headers}
}

test('真实栈 C3：登录 → 群 → 外部消息触发 Agent → 可查看步骤',async({page,request})=>{
 test.setTimeout(120000)
 const {group,headers}=await createTestGroup(request)
 await checked(await request.patch(backend+'/api/groups/'+group!.id,{headers,data:{agentEnabled:true}}))
 await page.goto('/login')
 await page.getByLabel('用户名').fill('admin');await page.getByLabel('密码',{exact:true}).fill('admin');await page.getByRole('button',{name:'登录',exact:true}).click()
 await expect(page.getByRole('heading',{name:'账号管理'})).toBeVisible()
 await page.getByText('群组工作区',{exact:true}).click()
 await page.getByRole('link',{name:group!.id,exact:true}).click()
 await expect(page.getByRole('heading',{name:'群消息工作区'})).toBeVisible()
 const marker='e2e-'+Date.now()
 await checked(await request.post(gateway+'/admin/events',{data:{type:'message',data:{groupId:group!.gatewayGroupId,msgId:marker,senderPlatformUserId:'90001',text:marker+' 请回复这个问题',sentAt:new Date().toISOString()}}}))
 await expect(page.getByText(marker+' 请回复这个问题',{exact:true})).toBeVisible()
 const run=page.locator('a[href^="/agent-runs/"]').first()
 await expect(run).toBeVisible({timeout:70000});await run.click()
 await expect(page.getByRole('heading',{name:'Agent 运行追踪'})).toBeVisible()
 await expect(page.getByRole('heading',{name:/步骤 1/})).toBeVisible({timeout:30000})
 await page.screenshot({path:'test-results/agent-run.png',fullPage:true})
})
test('真实栈 viewer没有写按钮，直接请求写API获得403',async({page,request})=>{
 const health=await request.get(backend+'/api/health');expect(health.ok(),'真实后端栈尚未就绪').toBeTruthy()
 await page.goto('/login');await page.getByLabel('用户名').fill('viewer');await page.getByLabel('密码',{exact:true}).fill('viewer');await page.getByRole('button',{name:'登录',exact:true}).click()
 await expect(page.getByRole('heading',{name:'账号管理'})).toBeVisible()
 await expect(page.getByRole('button',{name:'连接并检测',exact:true})).toHaveCount(0)
 await page.getByText('群组工作区',{exact:true}).click();await expect(page.getByRole('button',{name:'创建群组'})).toHaveCount(0)
 const {accessToken}=await checked<{accessToken:string}>(await request.post(backend+'/api/auth/login',{data:{username:'viewer',password:'viewer'}}))
 const result=await request.post(backend+'/api/accounts/account-1/connect',{headers:{Authorization:'Bearer '+accessToken}})
 expect(result.status()).toBe(403);expect((await result.json()).error.code).toBe('FORBIDDEN')
})


test('真实栈 B4：WS断线窗口消息重连后三秒内补齐且不重复',async({page,request},testInfo)=>{
 const {group,headers}=await createTestGroup(request)
 let blocked=false,current:{browser:WebSocketRoute;server:WebSocketRoute}|undefined,reconnectedAt:number|undefined
 const received:number[]=[],cursors:number[]=[]
 await page.routeWebSocket('**/ws',browser=>{
  if(blocked){void browser.close({code:1011,reason:'B4 controlled network outage'});return}
  const server=browser.connectToServer();current={browser,server}
  browser.onMessage(message=>{const frame=JSON.parse(String(message));if(frame.type==='auth')cursors.push(frame.sinceSeq);server.send(message)})
  server.onMessage(message=>{
   const frame=JSON.parse(String(message))
   if(frame.type==='auth'&&frame.success)reconnectedAt=performance.now()
   if(frame.type==='message'&&frame.payload?.groupId===group.id)received.push(frame.seq)
   browser.send(message)
  })
 })
 await page.goto('/login');await page.getByLabel('用户名').fill('admin');await page.getByLabel('密码',{exact:true}).fill('admin');await page.getByRole('button',{name:'登录',exact:true}).click()
 await expect(page.getByText('实时已连接',{exact:true})).toBeVisible()
 await page.goto('/groups/'+group.id)
 await expect(page.getByRole('heading',{name:'群消息工作区'})).toBeVisible()
 await expect(page.getByText('实时已连接',{exact:true})).toBeVisible()
 expect(current).toBeDefined()
 blocked=true
 await current!.server.close({code:1011,reason:'B4 controlled network outage'})
 await current!.browser.close({code:1011,reason:'B4 controlled network outage'})
 await expect(page.getByText('正在恢复连接',{exact:true})).toBeVisible()
 const markers=[1,2,3].map(n=>'b4-'+Date.now()+'-'+n)
 for(const marker of markers)await checked(await request.post(gateway+'/admin/events',{data:{type:'message',data:{groupId:group.gatewayGroupId,msgId:marker,senderPlatformUserId:'90001',text:marker,sentAt:new Date().toISOString()}}}))
 await expect.poll(async()=>{const messages=await checked<{items:{text:string}[]}>(await request.get(backend+'/api/groups/'+group.id+'/messages?limit=50',{headers}));return markers.filter(marker=>messages.items.some(m=>m.text===marker)).length}).toBe(3)
 for(const marker of markers)await expect(page.getByText(marker,{exact:true})).toHaveCount(0)
 received.length=0;reconnectedAt=undefined
 const restoredAt=performance.now();blocked=false
 await Promise.all(markers.map(marker=>page.getByText(marker,{exact:true}).waitFor({state:'visible',timeout:3000})))
 const visibleAt=performance.now(),fromRestoreMs=visibleAt-restoredAt
 expect(reconnectedAt,'实际后端WS认证必须成功').toBeDefined()
 expect(fromRestoreMs,'从恢复网络到三条消息全部可见，保持三秒阈值').toBeLessThan(3000)
 const fromAuthMs=visibleAt-reconnectedAt!
 for(const marker of markers)await expect(page.getByText(marker,{exact:true})).toHaveCount(1)
 expect(received).toHaveLength(3);expect(new Set(received).size).toBe(3)
 // A second real reconnect reuses sinceSeq and must not append any of those messages again.
 const before=cursors.length
 await current!.server.close({code:1011,reason:'B4 replay uniqueness check'})
 await current!.browser.close({code:1011,reason:'B4 replay uniqueness check'})
 await expect.poll(()=>cursors.length).toBeGreaterThan(before)
 await expect(page.getByText('实时已连接',{exact:true})).toBeVisible()
 for(const marker of markers)await expect(page.getByText(marker,{exact:true})).toHaveCount(1)
 expect(received).toHaveLength(3)
 const evidence={fromRestoreMs,fromAuthMs,messageCount:markers.length,uniqueMessageRows:3,receivedSequences:received,authCursors:cursors}
 console.log('B4 reconnect evidence '+JSON.stringify(evidence))
 await testInfo.attach('b4-reconnect.json',{body:JSON.stringify(evidence,null,2),contentType:'application/json'})
})
