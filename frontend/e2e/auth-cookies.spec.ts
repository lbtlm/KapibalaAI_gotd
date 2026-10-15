import {test,expect,type BrowserContext,type Page} from '@playwright/test'
import {createServer,type ServerResponse} from 'node:http'
import {readFileSync} from 'node:fs'
import {dirname,join} from 'node:path'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
import ts from 'typescript'
interface CookieSession {accessToken:string;role:'admin'|'viewer'}
interface BrowserClient {
 login(username:string,password:string):Promise<void>
 logout():Promise<void>
 refresh():Promise<CookieSession>
 getSession():CookieSession|null
}
declare global {
 interface Window {
  cookieClient:BrowserClient
  oldRefresh:Promise<{code?:string}>
  cookieChange:Promise<{code?:string}>
  queuedLogout:Promise<{code?:string}>
 }
}
function barrier(){
 let release!:()=>void
 const promise=new Promise<void>(resolve=>{release=resolve})
 return {promise,release}
}
const require=createRequire(import.meta.url)
const axios=readFileSync(join(dirname(require.resolve('axios/package.json')),'dist/axios.min.js'),'utf8')
const clientSource=readFileSync(fileURLToPath(new URL('../src/api/client.ts',import.meta.url)),'utf8')
const clientJS=ts.transpileModule(clientSource,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText
async function cookieFixture(expiredLogout=false){
 const aStarted=barrier(),aReply=barrier(),bHeaders=barrier(),bBody=barrier()
 let holdA=false
 const refreshCookies:string[]=[]
 const authRequests:string[]=[]
 const revoked=new Set<string>()
 function reply(res:ServerResponse,status:number,body:unknown,cookie?:string){
  res.writeHead(status,{'Content-Type':'application/json',...(cookie?{'Set-Cookie':cookie}:{})})
  res.end(JSON.stringify(body))
 }
 const cookie=(value:string)=>'refresh_token='+value+'; HttpOnly; SameSite=Strict; Path=/api/auth'
 const session=(user:string,version:number)=>({accessToken:'access-'+user+version,role:user==='B'?'viewer':'admin'})
 const server=createServer(async(req,res)=>{
  const path=new URL(req.url??'/', 'http://fixture').pathname
  if(path==='/'){res.writeHead(200,{'Content-Type':'text/html'});res.end('<!doctype html><title>HttpOnly auth boundary fixture</title><script src="/axios.js"></script><script src="/client.js"></script>');return}
  if(path==='/axios.js'){res.writeHead(200,{'Content-Type':'text/javascript'});res.end(axios);return}
  if(path==='/client.js'){res.writeHead(200,{'Content-Type':'text/javascript'});res.end('const exports={};function require(){return window.axios;}\n'+clientJS+'\nwindow.cookieClient=new exports.ApiClient();');return}
  if(path==='/checkpoint'){reply(res,200,{ok:true});return}
  let body=''
  for await(const part of req)body+=part
  const token=/refresh_token=([^;]+)/.exec(req.headers.cookie??'')?.[1]
  authRequests.push(path)
  if(path==='/api/auth/login'){
   const user=JSON.parse(body).username
   if(user==='B'){
    res.writeHead(200,{'Content-Type':'application/json','Set-Cookie':cookie('B1')})
    res.flushHeaders()
    bHeaders.release()
    await bBody.promise
    res.end(JSON.stringify(session('B',1)))
   }else reply(res,200,session('A',1),cookie('A1'))
   return
  }
  if(path==='/api/auth/refresh'){
   refreshCookies.push(req.headers.cookie??'')
   if(!token||revoked.has(token[0])){reply(res,401,{error:{code:'UNAUTHORIZED'}});return}
   const user=token[0],version=Number(token.slice(1))+1
   if(holdA&&token==='A1'){holdA=false;aStarted.release();await aReply.promise}
   reply(res,200,session(user,version),cookie(user+version))
   return
  }
  if(path==='/api/auth/logout'){
   if(expiredLogout&&req.headers.authorization==='Bearer access-A1'){reply(res,401,{error:{code:'UNAUTHORIZED'}});return}
   if(token)revoked.add(token[0])
   reply(res,200,{},'refresh_token=; HttpOnly; SameSite=Strict; Path=/api/auth; Max-Age=0')
   return
  }
  reply(res,404,{error:{code:'NOT_FOUND'}})
 })
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve))
 const address=server.address()
 if(!address||typeof address==='string')throw new Error('No HTTP fixture port')
 return {url:'http://127.0.0.1:'+address.port,aStarted,aReply,bHeaders,bBody,refreshCookies,authRequests,
  holdNextARefresh(){holdA=true},
  async close(){aReply.release();bBody.release();server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()))}
 }
}
async function refreshCookie(context:BrowserContext){return (await context.cookies()).find(c=>c.name==='refresh_token')}
async function loginA(page:Page,url:string){
 await page.goto(url)
 await page.evaluate(()=>window.cookieClient.login('A','demo'))
 expect(await page.evaluate(()=>document.cookie)).toBe('')
}
async function startOldRefresh(page:Page){
 await page.evaluate(()=>{window.oldRefresh=window.cookieClient.refresh().then(()=>({}),error=>({code:error.code}))})
}
test('真实HttpOnly：A续期挂起时登录B，最终cookie与内存一致且再次续期仍B',async({page,context})=>{
 const fixture=await cookieFixture()
 let bDispatched=false
 page.on('request',request=>{if(request.url().endsWith('/api/auth/login')&&request.postDataJSON()?.username==='B')bDispatched=true})
 try{
  await loginA(page,fixture.url)
  expect(await refreshCookie(context)).toMatchObject({value:'A1',httpOnly:true})
  fixture.holdNextARefresh()
  await startOldRefresh(page)
  await fixture.aStarted.promise
  await page.evaluate(()=>{window.cookieChange=window.cookieClient.login('B','demo').then(()=>({}),error=>({code:error.code}))})
  await page.evaluate(()=>fetch('/checkpoint').then(r=>r.json()))
  // If the transport dispatched B concurrently, hold its body after applying B1 headers.
  // This keeps the late A2 header schedule deterministic while allowing serialized clients.
  if(bDispatched){
   await fixture.bHeaders.promise
   await expect.poll(async()=> (await refreshCookie(context))?.value).toBe('B1')
  }
  fixture.aReply.release()
  expect(await page.evaluate(()=>window.oldRefresh)).toEqual({code:'SESSION_CHANGED'})
  fixture.bBody.release()
  expect(await page.evaluate(()=>window.cookieChange)).toEqual({})
  expect(await refreshCookie(context)).toMatchObject({value:'B1',httpOnly:true})
  expect(await page.evaluate(()=>window.cookieClient.getSession())).toEqual({accessToken:'access-B1',role:'viewer'})
  expect(await page.evaluate(()=>window.cookieClient.refresh())).toEqual({accessToken:'access-B2',role:'viewer'})
  expect(await refreshCookie(context)).toMatchObject({value:'B2',httpOnly:true})
  expect(fixture.refreshCookies).toEqual(['refresh_token=A1','refresh_token=B1'])
 }finally{await fixture.close()}
})
for(const expired of [false,true]){
 test('真实HttpOnly：挂起A续期后退出'+(expired?'（access过期）':'')+'，旧cookie不能复活且后续B续期仍B',async({page,context})=>{
  const fixture=await cookieFixture(expired)
  try{
   await loginA(page,fixture.url)
   fixture.holdNextARefresh()
   await startOldRefresh(page)
   await fixture.aStarted.promise
   await page.evaluate(()=>{window.cookieChange=window.cookieClient.logout().then(()=>({}),error=>({code:error.code}))})
   await page.evaluate(()=>fetch('/checkpoint').then(r=>r.json()))
   fixture.aReply.release()
   expect(await page.evaluate(()=>window.oldRefresh)).toEqual({code:'SESSION_CHANGED'})
   expect(await page.evaluate(()=>window.cookieChange)).toEqual({})
   expect(await refreshCookie(context)).toBeUndefined()
   expect(await page.evaluate(()=>window.cookieClient.getSession())).toBeNull()
   expect(await page.evaluate(()=>window.cookieClient.refresh().catch(error=>({code:error.code})))).toEqual({code:'UNAUTHORIZED'})
   fixture.bBody.release()
   await page.evaluate(()=>window.cookieClient.login('B','demo'))
   expect(await page.evaluate(()=>window.cookieClient.refresh())).toEqual({accessToken:'access-B2',role:'viewer'})
   expect(await refreshCookie(context)).toMatchObject({value:'B2',httpOnly:true})
   if(expired)expect(fixture.authRequests.filter(p=>p==='/api/auth/logout')).toHaveLength(2)
  }finally{await fixture.close()}
 })
}
test('真实HttpOnly：排队的旧退出在B登录意图之后取消，不向服务器发送',async({page,context})=>{
 const fixture=await cookieFixture()
 try{
  await loginA(page,fixture.url)
  fixture.holdNextARefresh()
  await startOldRefresh(page)
  await fixture.aStarted.promise
  await page.evaluate(()=>{
   window.queuedLogout=window.cookieClient.logout().then(()=>({}),error=>({code:error.code}))
   window.cookieChange=window.cookieClient.login('B','demo').then(()=>({}),error=>({code:error.code}))
  })
  fixture.aReply.release()
  expect(await page.evaluate(()=>window.oldRefresh)).toEqual({code:'SESSION_CHANGED'})
  expect(await page.evaluate(()=>window.queuedLogout)).toEqual({code:'SESSION_CHANGED'})
  fixture.bBody.release()
  expect(await page.evaluate(()=>window.cookieChange)).toEqual({})
  expect(fixture.authRequests).not.toContain('/api/auth/logout')
  expect(await page.evaluate(()=>window.cookieClient.refresh())).toEqual({accessToken:'access-B2',role:'viewer'})
  expect(await refreshCookie(context)).toMatchObject({value:'B2',httpOnly:true})
 }finally{await fixture.close()}
})
