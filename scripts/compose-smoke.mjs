import {pathToFileURL} from 'node:url';

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
  if (backend.ok !== true || messageGateway.status !== 'ok' || messageGateway.mode !== 'fake' || provider.ok !== true || provider.mode !== 'scripted') {
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
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [frontend,gateway,agent] = process.argv.slice(2);
    if (!frontend || !gateway || !agent) throw new Error('Usage: node scripts/compose-smoke.mjs <frontend-url> <fake-gateway-url> <scripted-provider-url>');
    await smoke(frontend,gateway,agent);
    console.log('Candidate images: gateway/provider/backend health, frontend page/asset and admin login passed');
  } catch(error) {console.error(error.message);process.exitCode=1;}
}
