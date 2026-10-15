import {pathToFileURL} from 'node:url';
import http from 'node:http';
import https from 'node:https';
import {createHash,randomBytes} from 'node:crypto';

// Node 22's WebSocket client cannot set Origin. Probe the real HTTP upgrade and
// first auth frame directly, keeping the candidate smoke dependency-free.
async function websocket(frontend,origin,accessToken) {
  const url=new URL('/ws',frontend);
  return new Promise((resolve,reject) => {
    const key=randomBytes(16).toString('base64');
    let socket;
    let settled=false;
    const finish=(error,result) => {
      if (settled) return;
      settled=true;
      clearTimeout(timer);
      socket?.destroy();
      request.destroy();
      if (error) reject(error); else resolve(result);
    };
    const request=(url.protocol==='https:' ? https : http).request(url,{headers:{
      Origin:origin,Connection:'Upgrade',Upgrade:'websocket','Sec-WebSocket-Version':'13','Sec-WebSocket-Key':key,
    }});
    const timer=setTimeout(()=>finish(new Error('WebSocket probe timed out')),5000);
    request.on('error',()=>finish(new Error('WebSocket connection failed')));
    request.on('response',response=>{response.resume();finish(null,{status:response.statusCode});});
    request.on('upgrade',(response,stream,head) => {
      socket=stream;
      const accept=createHash('sha1').update(key+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
      if (response.headers['sec-websocket-accept']!==accept) return finish(new Error('WebSocket handshake invalid'));
      if (!accessToken) return finish(null,{status:101});
      const body=Buffer.from(JSON.stringify({type:'auth',accessToken}));
      const extended=body.length>=126;
      const frame=Buffer.alloc((extended ? 4 : 2)+4+body.length);
      frame[0]=0x81;frame[1]=0x80|(extended ? 126 : body.length);
      if (extended) frame.writeUInt16BE(body.length,2);
      const offset=extended ? 4 : 2;
      const mask=randomBytes(4);mask.copy(frame,offset);
      for (let i=0;i<body.length;i++) frame[offset+4+i]=body[i]^mask[i%4];
      let buffered=Buffer.alloc(0);
      const receive=chunk => {
        buffered=Buffer.concat([buffered,chunk]);
        if (buffered.length<2) return;
        // The bounded auth reply is a single short unmasked server text frame.
        const size=buffered[1]&127;
        if (buffered[0]!==0x81 || buffered[1]&0x80 || size>=126) return finish(new Error('WebSocket auth frame invalid'));
        if (buffered.length<2+size) return;
        try {finish(null,{status:101,auth:JSON.parse(buffered.subarray(2,2+size).toString())});}
        catch {finish(new Error('WebSocket auth JSON invalid'));}
      };
      socket.on('error',()=>finish(new Error('WebSocket socket failed')));
      socket.on('end',()=>finish(new Error('WebSocket closed before auth')));
      socket.on('data',receive);
      socket.write(frame);
      if (head.length) receive(head);
    });
    request.end();
  });
}

async function ready(url) {
  const deadline = Date.now()+60000;
  while (Date.now()<deadline) {
    try {
      const response = await fetch(url,{signal:AbortSignal.timeout(2000)});
      if (response.ok) return response;
    } catch {}
    await new Promise(resolve=>setTimeout(resolve,500));
  }
  throw new Error(`Container service unhealthy: ${url}`);
}

export async function smoke(frontend,gateway,agent) {
  const [backendHealth,gatewayHealth,agentHealth] = await Promise.all([
    ready(frontend+'/api/health'),ready(gateway+'/health'),ready(agent+'/health'),
  ]);
  const backend = await backendHealth.json();
  const messageGateway = await gatewayHealth.json();
  const provider = await agentHealth.json();
  if (backend.ok !== true || messageGateway.status !== 'ok' || messageGateway.mode !== 'telegram' || provider.ok !== true || provider.mode !== 'scripted') {
    throw new Error('Container health contract failed');
  }
  const root = await fetch(frontend+'/',{signal:AbortSignal.timeout(5000)});
  const html = await root.text();
  const asset = /\bsrc="([^" ]+\.js)"/.exec(html)?.[1];
  if (!root.ok || !/id="root"/.test(html) || !asset) throw new Error('Frontend root page contract failed');
  if (!(await fetch(new URL(asset,frontend),{signal:AbortSignal.timeout(5000)})).ok) throw new Error('Frontend built asset is unavailable');
  const response = await fetch(frontend+'/api/auth/login',{
    method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'admin',password:'admin'}),signal:AbortSignal.timeout(5000),
  });
  const login = await response.json();
  const cookie = response.headers.get('set-cookie') || '';
  if (!response.ok || login.role !== 'admin' || typeof login.accessToken !== 'string' || !login.accessToken || !/refresh_token=/.test(cookie) || !/httponly/i.test(cookie)) {
    throw new Error('Container login contract failed');
  }
  const accountResponse=await fetch(frontend+'/api/accounts',{headers:{Authorization:'Bearer '+login.accessToken},signal:AbortSignal.timeout(5000)});
  const accounts=await accountResponse.json();
  if (!accountResponse.ok || !Array.isArray(accounts) || accounts.length!==0) throw new Error('Fresh candidate must start without accounts');
  const removed=await fetch(gateway+'/admin/reset',{method:'POST',signal:AbortSignal.timeout(5000)});
  if (removed.status!==404) throw new Error('Removed gateway simulation control is still reachable');
  const origin=new URL(frontend).origin;
  const valid=await websocket(frontend,origin,login.accessToken);
  if (valid.status!==101 || valid.auth?.type!=='auth' || valid.auth.success!==true) throw new Error('WebSocket same-origin authentication failed');
  const invalid=await websocket(frontend,origin,'invalid-smoke-token');
  if (invalid.status!==101 || invalid.auth?.type!=='auth' || invalid.auth.success!==false) throw new Error('WebSocket invalid token was not rejected');
  const cross=await websocket(frontend,'http://example.invalid');
  if (cross.status!==403) throw new Error('WebSocket cross-origin upgrade was not rejected');
  const logout=await fetch(frontend+'/api/auth/logout',{method:'POST',headers:{Authorization:'Bearer '+login.accessToken},signal:AbortSignal.timeout(5000)});
  if (!logout.ok) throw new Error('Candidate smoke login session was not revoked');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [frontend,gateway,agent] = process.argv.slice(2);
    if (!frontend || !gateway || !agent) throw new Error('Usage: node scripts/compose-smoke.mjs <fresh-frontend-url> <telegram-gateway-url> <scripted-provider-url>');
    await smoke(frontend,gateway,agent);
    console.log('Candidate images: real empty-account services, frontend assets, login and WebSocket origin/auth contracts passed; no Telegram accounts or RPCs exercised');
  } catch(error) {console.error(error.message);process.exitCode=1;}
}
