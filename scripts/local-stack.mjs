import {spawn} from 'node:child_process';
import {mkdir} from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const ports = [process.env.BACKEND_PORT || '8080', process.env.GATEWAY_PORT || '8081', process.env.AGENT_PORT || '8082'];
const bin = path.join(root, '.local', 'local-stack', ports.join('-'), 'bin');
const children = new Set();
let stopping = false;

async function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) child.kill('SIGTERM');
  const timer = setTimeout(() => {
    for (const child of children) child.kill('SIGKILL');
  }, 5000);
  timer.unref();
  await Promise.all([...children].map(child => new Promise(resolve => child.once('exit', resolve))));
  clearTimeout(timer);
  process.exitCode = code;
}
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => void stop());

function run(command, args, cwd, env, background = false) {
  const child = spawn(command, args, {cwd, env, stdio: 'inherit', windowsHide: true});
  children.add(child);
  child.once('exit', () => children.delete(child));
  const completed = new Promise((resolve, reject) => {
    child.once('error', error => { children.delete(child); reject(error); });
    child.once('exit', (code, signal) => {
      if (stopping || code === 0) resolve();
      else reject(new Error(`${path.basename(command)} exited (${code ?? signal})`));
    });
  });
  if (background) {
    completed.then(() => { if (!stopping) throw new Error(`${path.basename(command)} stopped unexpectedly`); })
      .catch(error => { console.error(error.message); void stop(1); });
  }
  return completed;
}
function available(port) {
  return new Promise((resolve, reject) => {
    const listener = net.createServer();
    listener.once('error', () => reject(new Error(`port ${port} is already in use; stop its owner explicitly`)));
    listener.listen(Number(port), '127.0.0.1', () => listener.close(resolve));
  });
}
async function ready(url) {
  const deadline = Date.now() + 60000;
  while (!stopping && Date.now() < deadline) {
    try { if ((await fetch(url, {signal: AbortSignal.timeout(1000)})).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  throw new Error(`service did not become healthy: ${url}`);
}

try {
  const dsn = process.env.E2E_DATABASE_URL;
  const gatewayDSN = process.env.E2E_GATEWAY_DATABASE_URL;
  if (!dsn) throw new Error('E2E_DATABASE_URL is required; use a dedicated backend database');
  if (!gatewayDSN) throw new Error('E2E_GATEWAY_DATABASE_URL is required; use a separate gateway database');
  const backendDB = new URL(dsn), gatewayDB = new URL(gatewayDSN);
  if (backendDB.hostname === gatewayDB.hostname && (backendDB.port || '5432') === (gatewayDB.port || '5432') && backendDB.pathname === gatewayDB.pathname) {
    throw new Error('Backend and gateway must use separate databases');
  }
  for (const port of ports) await available(port);
  await mkdir(bin, {recursive: true});
  const env = {...process.env, DATABASE_URL: dsn, BIND_HOST: '127.0.0.1'};
  const extension = process.platform === 'win32' ? '.exe' : '';
  const commands = [['migrate', 'backend'], ['server', 'backend'], ['agent', 'backend'], ['gateway', 'gateway']];
  for (const [name, module] of commands) {
    await run('go', ['build', '-trimpath', '-o', path.join(bin, name + extension), './cmd/' + name], path.join(root, module), env);
    if (stopping) break;
  }
  if (!stopping) {
    await run(path.join(bin, 'migrate' + extension), [], root, env);
    void run(path.join(bin, 'gateway' + extension), [], root, {...env, DATABASE_URL: gatewayDSN, PORT: ports[1], GATEWAY_MODE: 'telegram'}, true);
    void run(path.join(bin, 'agent' + extension), [], root, {...env, PORT: ports[2], AGENT_MODE: 'scripted', AGENT_ADMIN_ENABLED: 'false', OPENAI_API_KEY: ''}, true);
    await Promise.all([ready(`http://127.0.0.1:${ports[1]}/health`), ready(`http://127.0.0.1:${ports[2]}/health`)]);
    if (!stopping) {
      void run(path.join(bin, 'server' + extension), [], root, {...env, PORT: ports[0], GATEWAY_URL: `http://127.0.0.1:${ports[1]}`, AGENT_URL: `http://127.0.0.1:${ports[2]}`}, true);
      await ready(`http://127.0.0.1:${ports[0]}/api/health`);
      console.log('Local services ready; accounts are uploaded and verified through Telegram. Ctrl+C stops only owned processes.');
    }
  }
} catch (error) {
  console.error(error.message);
  await stop(1);
}
