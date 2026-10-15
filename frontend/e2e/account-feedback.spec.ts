import {test,expect} from '@playwright/test'
import fs from 'node:fs/promises'
const account={id:'account-1',status:'online',platformUserId:'10001',phone:null,firstName:null,lastName:null,username:null,rateLimitedUntil:null}
function contrast(foreground:string,background:string){
 const rgb=(s:string)=>s.match(/[\d.]+/g)!.map(Number).slice(0,3).map(v=>{const c=v/255;return c<=.04045?c/12.92:((c+.055)/1.055)**2.4})
 const lum=(s:string)=>{const c=rgb(s);return .2126*c[0]+.7152*c[1]+.0722*c[2]}
 const a=lum(foreground),b=lum(background);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05)
}
test('日志成功和失败结果保持可读状态色（空身份展示夹具）',async({page},testInfo)=>{
 await page.route(url=>url.pathname.startsWith('/api/'),route=>{const pathname=new URL(route.request().url()).pathname;const data=pathname==='/api/auth/refresh'?{accessToken:'feedback-fixture',role:'viewer'}:pathname==='/api/accounts'?[account]:pathname==='/api/account-groups'?[]:pathname==='/api/accounts/account-1'?account:pathname.endsWith('/logs')?{items:[{id:'success',actor:'system',action:'check',result:'success',errorCode:null,createdAt:'2026-10-15T04:00:00Z'},{id:'failed',actor:'system',action:'check',result:'failed',errorCode:'NETWORK_TIMEOUT',createdAt:'2026-10-15T04:00:01Z'}],nextCursor:null}:{};return route.fulfill({json:data})})
 await page.routeWebSocket('**/ws',ws=>ws.onMessage(()=>ws.send(JSON.stringify({type:'auth',success:true}))))
 await page.goto('/accounts');await page.getByRole('button',{name:'详情',exact:true}).click();const modal=page.getByRole('dialog');await modal.getByRole('tab',{name:'操作日志',exact:true}).click()
 const success=modal.getByRole('cell',{name:'成功',exact:true}).locator('.ant-tag'),failed=modal.getByRole('cell',{name:'失败',exact:true}).locator('.ant-tag')
 await expect(success).toBeVisible();await expect(failed).toBeVisible()
 const colors=await Promise.all([success,failed].map(tag=>tag.evaluate(el=>{const css=getComputedStyle(el);return {text:el.textContent,color:css.color,background:css.backgroundColor,opacity:css.opacity}})))
 const results=colors.map(c=>({...c,contrast:contrast(c.color,c.background)}));const colorsPath=testInfo.outputPath('log-colors.json');await fs.writeFile(colorsPath,JSON.stringify(results,null,2));await testInfo.attach('log-colors',{path:colorsPath,contentType:'application/json'})
 await page.screenshot({path:testInfo.outputPath('log-results.png'),fullPage:true,animations:'disabled'})
 for(const result of results)expect(result.contrast,result.text+' text contrast').toBeGreaterThanOrEqual(4.5)
 expect(results[0].color).not.toBe(results[1].color)
})
for(const scenario of [{name:'未同步',file:'groups-not-synced.png',lastSyncedAt:null,fail:false,message:'尚未同步所在群。'},{name:'成功为空',file:'groups-synced-empty.png',lastSyncedAt:'2026-10-15T04:00:00Z',fail:false,message:'最近一次同步未发现已加入的群组。'},{name:'失败保留上次空结果',file:'groups-failed-retained.png',lastSyncedAt:'2026-10-15T04:00:00Z',fail:true,message:'最近一次同步未发现已加入的群组。'}]){
 test('所在群：'+scenario.name+'（不补身份或群资料）',async({page},testInfo)=>{
  await page.route(url=>url.pathname.startsWith('/api/'),route=>{const pathname=new URL(route.request().url()).pathname;if(pathname.endsWith('/joined-groups/sync'))return route.fulfill({status:504,json:{error:{code:'NETWORK_TIMEOUT',message:'同步超时，请稍后重试'}}});const data=pathname==='/api/auth/refresh'?{accessToken:'feedback-fixture',role:'admin'}:pathname==='/api/accounts'?[account]:pathname==='/api/account-groups'?[]:pathname==='/api/accounts/account-1'?account:pathname.endsWith('/joined-groups')?{items:[],lastSyncedAt:scenario.lastSyncedAt}:{};return route.fulfill({json:data})})
  await page.routeWebSocket('**/ws',ws=>ws.onMessage(()=>ws.send(JSON.stringify({type:'auth',success:true}))))
  await page.goto('/accounts');await page.getByRole('button',{name:'详情',exact:true}).click();const modal=page.getByRole('dialog');await modal.getByRole('tab',{name:'所在群',exact:true}).click()
  await expect(modal.getByText(scenario.message,{exact:true})).toBeVisible()
  const syncTime=modal.locator('span').filter({hasText:/^最近同步：/}),before=await syncTime.textContent()
  if(scenario.fail){await modal.getByRole('button',{name:'同步群组',exact:true}).click();await expect(modal.getByText('本次同步失败，仍显示上次同步结果。',{exact:true})).toBeVisible();await expect(syncTime).toHaveText(before!);await expect(modal.getByText(scenario.message,{exact:true})).toBeVisible()}
  await expect(modal.getByRole('cell')).toHaveCount(1)
  await page.screenshot({path:testInfo.outputPath(scenario.file),fullPage:true,animations:'disabled'})
 })
}
