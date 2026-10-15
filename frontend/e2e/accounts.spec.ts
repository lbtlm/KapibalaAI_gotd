import {test,expect,type APIRequestContext} from '@playwright/test'
import {DatabaseSync} from 'node:sqlite'
import {mkdtemp,writeFile,rm} from 'node:fs/promises'
import path from 'node:path'
import os from 'node:os'
const backend=process.env.E2E_API_URL??'http://127.0.0.1:8080'
async function checked<T>(response:Awaited<ReturnType<APIRequestContext['get']>>){const body=await response.text();expect(response.ok(),body).toBeTruthy();return (body?JSON.parse(body):{}) as T}
async function syntheticPair(directory:string,basename:string,seed:number,phone:string,valid=true){
 const session=path.join(directory,basename+'.session'),metadata=path.join(directory,basename+'.json')
 if(valid){const db=new DatabaseSync(session);db.exec('CREATE TABLE sessions (dc_id INTEGER, server_address TEXT, port INTEGER, auth_key BLOB)');const key=Buffer.alloc(256,seed);Buffer.from(basename).copy(key);db.prepare('INSERT INTO sessions VALUES (2,?,443,?)').run('149.154.167.51',key);db.close()}else await writeFile(session,'deliberately invalid synthetic session')
 await writeFile(metadata,JSON.stringify({api_id:123,api_hash:'0123456789abcdef0123456789abcdef',phone,first_name:'Synthetic',last_name:basename}))
 return [session,metadata]
}
test('账号管理实际栈：配对导入、逐项结果、分组、模拟检测、只读详情、群快照与日志',async({page,request},testInfo)=>{
 test.setTimeout(120000)
 const directory=await mkdtemp(path.join(os.tmpdir(),'kapibala-synthetic-accounts-')),suffix=Date.now().toString(36),groupName='账号验收-'+suffix
 const phone='155'+Date.now().toString().slice(-8),phone2='156'+Date.now().toString().slice(-8)
 try{
  const one=await syntheticPair(directory,'one-'+suffix,201,phone),two=await syntheticPair(directory,'two-'+suffix,202,phone2),broken=await syntheticPair(directory,'broken-'+suffix,203,'15550000003',false)
  await page.setViewportSize({width:1920,height:1080})
  await page.goto('/login');await page.getByLabel('用户名').fill('admin');await page.getByLabel('密码',{exact:true}).fill('admin')
  const loginResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/auth/login'&&r.request().method()==='POST')
  await page.getByRole('button',{name:'登录',exact:true}).click();const {accessToken}=await (await loginResponse).json();const headers={Authorization:'Bearer '+accessToken}
  await expect(page.getByRole('heading',{name:'账号管理'})).toBeVisible()
  await page.getByRole('button',{name:'管理分组'}).click();let modal=page.getByRole('dialog',{name:'管理分组'})
  await modal.getByRole('textbox',{name:'新分组名称'}).fill(groupName);await modal.getByRole('button',{name:'创建分组'}).click();await expect(modal.getByRole('cell',{name:groupName,exact:true})).toBeVisible();await modal.getByRole('button',{name:'关闭',exact:true}).last().click()
  await page.getByRole('button',{name:'上传账号'}).click();modal=page.getByRole('dialog',{name:'上传账号'})
  await modal.getByRole('combobox',{name:'导入到分组'}).click();await page.locator('.ant-select-item-option-content').filter({hasText:groupName}).click()
  await modal.getByLabel('账号文件').setInputFiles([...one,...two,...broken]);await expect(modal.getByText('3 组配对完整')).toBeVisible()
  const imported=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/accounts/import'&&r.request().method()==='POST')
  await modal.getByRole('button',{name:'开始导入'}).click();const response=await imported
  expect(response.ok()).toBeTruthy();const contentType=response.request().headers()['content-type'];expect(contentType).toMatch(/^multipart\/form-data; boundary=/)
  const {results}=await response.json() as {results:{basename:string;accountId:string|null;success:boolean;error:{code:string}|null}[]}
  expect(results.filter(r=>r.success)).toHaveLength(2);expect(results.find(r=>!r.success)?.error?.code).toBe('INVALID_SESSION')
  await expect(modal.getByText('导入成功',{exact:true})).toHaveCount(2);await expect(modal.getByText(/Session 文件无效/)).toBeVisible();await page.screenshot({path:testInfo.outputPath('account-upload-results.png'),fullPage:true,animations:'disabled'})
  const id=results.find(r=>r.basename==='one-'+suffix)!.accountId!,secondId=results.find(r=>r.basename==='two-'+suffix)!.accountId!
  await modal.getByRole('button',{name:'关闭',exact:true}).last().click()
  await page.getByRole('textbox',{name:'搜索账号'}).fill(id)
  let row=page.getByRole('row').filter({has:page.getByText(phone,{exact:true})})
  await expect(row.getByText('空闲',{exact:true})).toBeVisible();await expect(row.getByRole('cell',{name:groupName,exact:true})).toBeVisible()
  const lifecycleWrites:string[]=[];page.on('request',r=>{if(r.method()==='POST'&&new URL(r.url()).pathname.startsWith('/api/accounts/'+id+'/'))lifecycleWrites.push(new URL(r.url()).pathname)})
  await row.getByRole('button',{name:'连接并检测',exact:true}).click();await expect(row.getByText('在线',{exact:true})).toBeVisible();await expect(row.getByText('模拟检测成功',{exact:true})).toBeVisible()
  expect(lifecycleWrites).toEqual(['/api/accounts/'+id+'/check'])
  await row.getByRole('button',{name:'详情',exact:true}).click();modal=page.getByRole('dialog',{name:'账号详情 · '+id})
  await expect(modal.getByText('模拟资料，未核验手机号')).toBeVisible();await expect(modal.getByText('Telegram 已核验')).toHaveCount(0);await page.screenshot({path:testInfo.outputPath('account-details.png'),fullPage:true,animations:'disabled'})
  await modal.getByRole('tab',{name:'操作日志'}).click();await expect(modal.getByRole('cell',{name:'导入账号',exact:true})).toBeVisible();await expect(modal.getByRole('cell',{name:'检测账号',exact:true})).toBeVisible();await page.screenshot({path:testInfo.outputPath('account-logs.png'),fullPage:true,animations:'disabled'})
  expect(lifecycleWrites).toEqual(['/api/accounts/'+id+'/check'])
  const accounts=await checked<{id:string;status:string}[]>(await request.get(backend+'/api/accounts',{headers}));const member=accounts.find(a=>a.id==='account-1')!
  if(member.status!=='online')await checked(await request.post(backend+'/api/accounts/'+member.id+'/connect',{headers}))
  const {jobId}=await checked<{jobId:string}>(await request.post(backend+'/api/groups',{headers,data:{creatorAccountId:id,memberAccountIds:[member.id]}}))
  let createdGroupId:string|undefined
  await expect.poll(async()=>{const job=await checked<{status:string;groupId?:string}>(await request.get(backend+'/api/jobs/'+jobId,{headers}));createdGroupId=job.groupId;return job.status},{timeout:20000}).toBe('finished')
  const managedBefore=await checked<{id:string;agentEnabled:boolean}[]>(await request.get(backend+'/api/groups',{headers}))
  await modal.getByRole('tab',{name:'所在群'}).click();await expect(modal.getByText('尚无成功同步的群组快照。')).toBeVisible()
  await modal.getByRole('button',{name:'同步群组'}).click();await expect(modal.getByRole('cell',{name:'群主',exact:true})).toBeVisible()
  const managedAfter=await checked<{id:string;agentEnabled:boolean}[]>(await request.get(backend+'/api/groups',{headers}));expect(managedAfter).toEqual(managedBefore);expect(managedAfter.find(g=>g.id===createdGroupId)?.agentEnabled).toBe(false)
  await page.screenshot({path:testInfo.outputPath('account-groups.png'),fullPage:true,animations:'disabled'});await modal.getByRole('button',{name:'关闭',exact:true}).last().click()
  await page.getByRole('button',{name:'上传账号'}).click();modal=page.getByRole('dialog',{name:'上传账号'});await modal.getByLabel('账号文件').setInputFiles(one);await modal.getByRole('button',{name:'开始导入'}).click();await expect(modal.getByText(/账号已导入，请勿重复上传/)).toBeVisible();await modal.getByRole('button',{name:'关闭',exact:true}).last().click()
  await page.getByRole('textbox',{name:'搜索账号'}).fill(secondId);row=page.getByRole('row').filter({has:page.getByText(phone2,{exact:true})});await row.getByRole('checkbox').check();await page.getByRole('button',{name:'移动到分组'}).click();modal=page.getByRole('dialog',{name:'批量移动分组'});await modal.getByRole('button',{name:'确认移动'}).click();await expect(modal).toHaveCount(0);await expect(row.getByRole('cell',{name:'未分组',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'管理分组'}).click();modal=page.getByRole('dialog',{name:'管理分组'});const groupRow=modal.getByRole('row').filter({has:page.getByRole('cell',{name:groupName,exact:true})});await groupRow.getByRole('button',{name:'删除',exact:true}).click();await page.getByRole('button',{name:'确认删除'}).click();await expect(modal.getByRole('cell',{name:groupName,exact:true})).toHaveCount(0);await modal.getByRole('button',{name:'关闭',exact:true}).last().click();await page.getByRole('textbox',{name:'搜索账号'}).fill(id);await expect(page.getByText(phone,{exact:true})).toBeVisible();await expect(page.getByRole('row').filter({has:page.getByText(phone,{exact:true})}).getByRole('cell',{name:'未分组',exact:true})).toBeVisible()
  await testInfo.attach('account-inspection-evidence',{body:JSON.stringify({imported:2,failed:1,multipart:contentType.split(';')[0],source:'simulated',phoneVerified:false,groupSyncRole:'creator',detailReadOnly:true,bulkMove:true,deleteGroupPreservesAccount:true}),contentType:'application/json'})
 }finally{await rm(directory,{recursive:true,force:true})}
})
test('账号管理实际栈：viewer隐藏全部写入口并可只读查看详情',async({page,request})=>{
 await page.goto('/login');await page.getByLabel('用户名').fill('viewer');await page.getByLabel('密码',{exact:true}).fill('viewer');await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page.getByRole('heading',{name:'账号管理'})).toBeVisible()
 for(const name of ['上传账号','管理分组','连接并检测','检测','标记离线','释放账号','移动到分组'])await expect(page.getByRole('button',{name,exact:true})).toHaveCount(0)
 await expect(page.getByRole('checkbox')).toHaveCount(0);await page.getByRole('button',{name:'详情',exact:true}).first().click();const modal=page.getByRole('dialog');await modal.getByRole('tab',{name:'所在群'}).click();await expect(modal.getByRole('button',{name:'同步群组'})).toHaveCount(0)
 const {accessToken}=await checked<{accessToken:string}>(await request.post(backend+'/api/auth/login',{data:{username:'viewer',password:'viewer'}}));const result=await request.post(backend+'/api/account-groups',{headers:{Authorization:'Bearer '+accessToken},data:{name:'must-not-write'}});expect(result.status()).toBe(403)
})
