import {test} from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {smoke} from './compose-smoke.mjs';

async function fixture(login) {
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
