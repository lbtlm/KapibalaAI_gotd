import {test,expect} from '@playwright/test'

// C3 uses existing persisted groups/runs. It never fabricates a gateway response,
// creates a run, sends a message, or changes campaign state. Opt in explicitly.
test('C3 登录 → 打开已有群 → 查看 Agent run 的全部步骤',async({page})=>{
 test.skip(process.env.E2E_EXISTING_GROUP!=='1','Requires a local system with an existing group and Agent run; see docs/api-flow-coverage.md')
 const blocked:string[]=[],errors:string[]=[]
 page.on('pageerror',e=>errors.push(e.name))
 await page.route('**/*',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname
  if(!['GET','HEAD','OPTIONS'].includes(request.method())&&!['/api/auth/login','/api/auth/refresh','/api/auth/logout'].includes(path)){blocked.push(request.method()+' '+path);await route.abort();return}
  await route.continue()
 })
 await page.goto('/login')
 await page.getByLabel('用户名').fill(process.env.E2E_USERNAME??'viewer')
 await page.getByLabel('密码',{exact:true}).fill(process.env.E2E_PASSWORD??'viewer')
 const login=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/auth/login'&&r.request().method()==='POST')
 await page.getByRole('button',{name:'登录',exact:true}).click()
 const response=await login;expect(response.ok()).toBe(true)
 const {accessToken}=await response.json() as {accessToken:string}
 const headers={Authorization:'Bearer '+accessToken}
 await expect(page.getByRole('heading',{name:'账号管理',exact:true})).toBeVisible()
 await page.getByRole('menuitem',{name:/群组工作区/}).click()
 const groupsResponse=await page.request.get('/api/groups',{headers});expect(groupsResponse.ok()).toBe(true)
 const groups=await groupsResponse.json() as {id:string}[]
 let groupID='',runID='',stepCount=0
 for(const group of groups){
  if(process.env.E2E_GROUP_ID&&process.env.E2E_GROUP_ID!==group.id)continue
  const response=await page.request.get('/api/groups/'+group.id+'/agent-runs',{headers});expect(response.ok()).toBe(true)
  const runs=await response.json() as {id:string}[]
  for(const run of runs){
   const detail=await page.request.get('/api/agent-runs/'+run.id,{headers});expect(detail.ok()).toBe(true)
   const stored=await detail.json() as {steps:{kind:string}[];status:string}
   if(stored.status!=='running'&&stored.steps.length){groupID=group.id;runID=run.id;stepCount=stored.steps.length;break}
  }
  if(groupID)break
 }
 expect(Boolean(groupID),'C3 requires an existing completed/failed run with persisted steps; no synthetic run is inserted').toBe(true)
 await page.locator('a').filter({hasText:'进入管理'}).and(page.locator(`[href="/groups/${groupID}"]`)).click()
 await expect(page.getByRole('tab',{name:'实时聊天',exact:true})).toBeVisible()
 await page.getByRole('tab',{name:'Agent 回复记录',exact:true}).click()
 await page.getByRole('combobox',{name:'处理批次'}).click()
 await page.locator('.ant-select-item-option').filter({hasText:runID.slice(0,8)}).click()
 await expect(page.getByText(stepCount+' 个执行步骤',{exact:true})).toBeVisible()
 const table=page.getByRole('tabpanel',{name:'Agent 回复记录'}).getByRole('table')
 await expect(table.locator('tbody tr.ant-table-row')).toHaveCount(stepCount)
 for(const title of ['操作','回复 / 结果','审计','错误码','详情'])await expect(table.getByRole('columnheader',{name:title,exact:true})).toBeVisible()
 const detailButton=table.getByRole('button',{name:/^(完整结果|结果摘要|原始响应体)$/}).first()
 await detailButton.hover();await expect(page.getByRole('tooltip')).toBeVisible()
 await detailButton.click();await expect(page.getByRole('dialog')).toBeVisible()
 expect((await page.getByRole('dialog').locator('pre').innerText()).length>0).toBe(true)
 await page.locator('.ant-modal-close:visible').click()
 expect(blocked).toEqual([]);expect(errors).toEqual([])
})

// Real-message content must never be uploaded in CI artifacts or test traces.
test.use({trace:'off',screenshot:'off',video:'off'})
