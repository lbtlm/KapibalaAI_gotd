import {test} from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {createHash} from 'node:crypto';
import {smoke} from './compose-smoke.mjs';

async function fixture(login,{denySameOrigin=false,allowCrossOrigin=false,acceptInvalidToken=false,rejectValidToken=false}={}) {
  const server = http.createServer((request,response) => {
    if (request.url === '/api/health') return response.end('{"ok":true}');
    if (request.url === '/gateway/health') return response.end('{"status":"ok","mode":"fake"}');
    if (request.url === '/agent/health') return response.end('{"ok":true,"mode":"scripted"}');
    if (request.url === '/') return response.end('<html><div id="root"></div><script src="/assets/app.js"></script></html>');
    if (request.url === '/assets/app.js') return response.end('console.log("demo");');
    if (request.url === '/api/auth/login') {
      response.setHeader('Set-Cookie','refresh_token=demo; HttpOnly; Path=/api/auth');
      return response.end(JSON.stringify(login));
    }
    response.statusCode=404;response.end();
  });
  server.on('upgrade',(request,socket) => {
    const sameOrigin = request.headers.origin === 'http://'+request.headers.host;
    if ((sameOrigin && denySameOrigin) || (!sameOrigin && !allowCrossOrigin)) {
      socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
      return;
    }
    const accept = createHash('sha1').update(request.headers['sec-websocket-key']+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
    socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: '+accept+'\r\n\r\n');
    let buffered = Buffer.alloc(0);
    socket.on('data',chunk => {
      buffered=Buffer.concat([buffered,chunk]);
      if (buffered.length<6) return;
      const extended=(buffered[1]&127)===126;
      const size=extended ? buffered.readUInt16BE(2) : buffered[1]&127;
      const offset=extended ? 4 : 2;
      if (buffered.length<offset+4+size) return;
      const body=Buffer.from(buffered.subarray(offset+4,offset+4+size));
      for (let i=0;i<body.length;i++) body[i]^=buffered[offset+i%4];
      const auth=JSON.parse(body.toString());
      const success=!rejectValidToken && (auth.type==='auth' && auth.accessToken==='demo' || acceptInvalidToken);
      const reply=Buffer.from(JSON.stringify({type:'auth',success}));
      socket.end(Buffer.concat([Buffer.from([0x81,reply.length]),reply]));
    });
    socket.on('error',()=>{});
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  return {server,url:'http://127.0.0.1:'+server.address().port};
}

test('image smoke requires backend, gateway, provider, frontend and authenticated login', async () => {
  const {server,url}=await fixture({accessToken:'demo',role:'admin'});
  try {await smoke(url,url+'/gateway',url+'/agent');} finally {server.close();}
});

test('healthy proxy alone cannot hide a broken login contract', async () => {
  const {server,url}=await fixture({role:'admin'});
  try {await assert.rejects(smoke(url,url+'/gateway',url+'/agent'),/login contract/);} finally {server.close();}
});

for (const [name,options] of [
  ['same-origin upgrade rejected',{denySameOrigin:true}],
  ['cross-origin upgrade accepted',{allowCrossOrigin:true}],
  ['invalid token accepted',{acceptInvalidToken:true}],
  ['valid token rejected',{rejectValidToken:true}],
]) {
  test('image smoke rejects '+name,async () => {
    const {server,url}=await fixture({accessToken:'demo',role:'admin'},options);
    try {await assert.rejects(smoke(url,url+'/gateway',url+'/agent'),/WebSocket/);} finally {server.close();}
  });
}
