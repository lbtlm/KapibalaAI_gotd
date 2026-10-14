import {test,expect,type APIRequestContext} from '@playwright/test'
const backend=process.env.E2E_API_URL??'http://127.0.0.1:8080'
const gateway=process.env.E2E_GATEWAY_URL??'http://127.0.0.1:8081'
async function checked<T = unknown>(response:Awaited<ReturnType<APIRequestContext['get']>>){const text=await response.text();expect(response.ok(),text).toBeTruthy();return (text?JSON.parse(text):{}) as T}
test('真实栈 C3：登录 → 群 → 外部消息触发 Agent → 可查看步骤',async({page,request})=>{
 test.setTimeout(120000)
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
 await checked(await request.patch(backend+'/api/groups/'+group!.id,{headers,data:{agentEnabled:true}}))
 await page.goto('/login')
 await page.getByLabel('用户名').fill('admin');await page.getByLabel('密码',{exact:true}).fill('admin');await page.getByRole('button',{name:'登录',exact:true}).click()
 await expect(page.getByRole('heading',{name:'服务账号'})).toBeVisible()
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
 await expect(page.getByRole('heading',{name:'服务账号'})).toBeVisible()
 await expect(page.getByRole('button',{name:'重连',exact:true})).toHaveCount(0)
 await page.getByText('群组工作区',{exact:true}).click();await expect(page.getByRole('button',{name:'创建群组'})).toHaveCount(0)
 const {accessToken}=await checked<{accessToken:string}>(await request.post(backend+'/api/auth/login',{data:{username:'viewer',password:'viewer'}}))
 const result=await request.post(backend+'/api/accounts/account-1/connect',{headers:{Authorization:'Bearer '+accessToken}})
 expect(result.status()).toBe(403);expect((await result.json()).error.code).toBe('FORBIDDEN')
})
