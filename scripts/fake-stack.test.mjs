import {test} from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const script = fileURLToPath(new URL('./fake-stack.mjs', import.meta.url));

test('missing Go exits promptly instead of waiting forever for a never-started child', async () => {
  const ports = [];
  for (let i = 0; i < 3; i++) {
    const listener = net.createServer();
    await new Promise(resolve => listener.listen(0, '127.0.0.1', resolve));
    ports.push(String(listener.address().port));
    await new Promise(resolve => listener.close(resolve));
  }
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toUpperCase() !== 'PATH'));
  const result = spawnSync(process.execPath, [script], {
    env: {...env, PATH: '/nonexistent-go-path', E2E_DATABASE_URL: 'postgres://example/isolated_e2e', BACKEND_PORT: ports[0], GATEWAY_PORT: ports[1], AGENT_PORT: ports[2]},
    encoding:'utf8', timeout:5000,
  });
  assert.equal(result.error, undefined, 'startup failure must not hang');
  assert.equal(result.status, 1);
  assert.match(result.stderr, /ENOENT/);
});

test('missing E2E database fails before builds or migrations', () => {
  const result = spawnSync(process.execPath, [script], {env: {...process.env, E2E_DATABASE_URL: ''}, encoding: 'utf8'});
  assert.equal(result.status, 1);
  assert.match(result.stderr, /E2E_DATABASE_URL is required/);
});

test('occupied service port is refused without stopping the existing listener', async () => {
  const listener = net.createServer();
  await new Promise(resolve => listener.listen(0, '127.0.0.1', resolve));
  try {
    const result = spawnSync(process.execPath, [script], {
      env: {...process.env, E2E_DATABASE_URL: 'postgres://example/isolated_e2e', BACKEND_PORT: String(listener.address().port)},
      encoding: 'utf8', timeout: 10000,
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /port .* is already in use/);
    assert.equal(listener.listening, true);
  } finally { listener.close(); }
});
