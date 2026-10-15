import {test,expect,type Page} from '@playwright/test'

async function login(page:Page,username:'admin'|'viewer'){
 await page.goto('/login')
 await page.getByLabel('用户名').fill(username)
 await page.getByLabel('密码',{exact:true}).fill(username)
 const response=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/auth/login'&&r.request().method()==='POST')
 await page.getByRole('button',{name:'登录',exact:true}).click()
 const result=await response
 expect(result.ok()).toBeTruthy()
 const session=await result.json() as {accessToken:string}
 const accounts=await page.request.get('/api/accounts',{headers:{Authorization:'Bearer '+session.accessToken}})
 expect(accounts.ok()).toBeTruthy()
 expect(await accounts.json(),'只允许在没有账号的隔离真实系统运行').toEqual([])
 await expect(page.getByRole('heading',{name:'账号管理',exact:true})).toBeVisible()
 return session
}

test('真实空系统登录后引导上传真实账号，桌面账号工作区填满可用宽度',async({page},testInfo)=>{
 const errors:string[]=[]
 page.on('pageerror',error=>errors.push(error.message))
 await login(page,'admin')
 await expect(page.getByText('上传真实账号的同名 .session 和 .json 文件以导入账号。',{exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'上传账号',exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'管理分组',exact:true})).toBeVisible()
 await expect(page.getByRole('textbox',{name:'搜索账号'})).toBeVisible()
 for(const width of [1366,1920,2560]){
  await page.setViewportSize({width,height:900})
  const workspace=page.getByRole('region',{name:'账号管理工作区'})
  const bounds=await workspace.boundingBox()
  expect(bounds).not.toBeNull()
  expect(width-bounds!.x-bounds!.width).toBeLessThanOrEqual(32)
  expect(bounds!.width).toBeGreaterThan(width-260)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:testInfo.outputPath('empty-accounts-'+width+'.png'),animations:'disabled'})
 }
 expect(errors).toEqual([])
 await page.getByRole('button',{name:/退出登录/}).click()
 await expect(page.getByRole('button',{name:'登录',exact:true})).toBeVisible()
})

test('真实空系统viewer只有读取入口，实际导入API拒绝写入',async({page})=>{
 const session=await login(page,'viewer')
 for(const name of ['上传账号','管理分组','连接并检测','检测','移动到分组'])await expect(page.getByRole('button',{name,exact:true})).toHaveCount(0)
 const denied=await page.request.post('/api/accounts/import',{headers:{Authorization:'Bearer '+session.accessToken}})
 expect(denied.status()).toBe(403)
 expect((await denied.json()).error.code).toBe('FORBIDDEN')
 await page.getByRole('button',{name:/退出登录/}).click()
 await expect(page.getByRole('button',{name:'登录',exact:true})).toBeVisible()
})
