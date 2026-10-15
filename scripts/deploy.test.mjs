import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,copyFile,rm} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {verifyCandidate} from './deploy.mjs';
import {spawn} from 'node:child_process';
import http from 'node:http';
import {pathToFileURL} from 'node:url';

async function deploymentFixture(options={}) {
  const directory=await mkdtemp(path.join(os.tmpdir(),'kapibala-deploy-cli-'));
  const sha='a'.repeat(40);
  const services={postgres:{image:'postgres:16'}};
  if (options.gatewayDatabase) services['gateway-db']={image:'postgres:16'};
  for (const [name,image] of Object.entries({backend:'backend',migrate:'backend',agent:'backend',gateway:'gateway',frontend:'frontend'})) {
    services[name]={image:'kapibala-'+image+':'+sha,build:{context:'unavailable-source'}};
  }
  const files={'COMMIT_SHA':sha+'\n','images.tar.gz':'fixture archive','compose.yaml':JSON.stringify({services})};
  for (const [name,body] of Object.entries(files)) await writeFile(path.join(directory,name),body);
  await writeFile(path.join(directory,'SHA256SUMS'),Object.entries(files).map(([name,body])=>createHash('sha256').update(body).digest('hex')+'  '+name).join('\n')+'\n');
  await mkdir(path.join(directory,'scripts'));
  await copyFile(new URL('./deploy.mjs',import.meta.url),path.join(directory,'scripts','deploy.mjs'));
  const executable=path.join(directory,'docker-fixture.mjs');
  // An external executable contract fixture: Node's preload handles Docker's
  // argv before resolving its entrypoint, including on Windows without a shell.
  await writeFile(executable,`
import path from 'node:path';
import {appendFileSync} from 'node:fs';
const command=path.basename(process.argv[1] || '');
if (['load','image','compose'].includes(command)) {
  const args=[command,...process.argv.slice(2)];
  appendFileSync(process.env.DOCKER_TRACE,JSON.stringify({args})+'\\n');
  if (args.includes('build') || args.includes('--build') || args.includes('pull')) {console.error('candidate build or pull attempted');process.exit(7);}
  if (command==='image' && process.env.FIXTURE_MISSING_IMAGE==='1') {console.error('image not found');process.exit(1);}
  if (command==='compose' && args.includes('config')) console.log(process.env.FIXTURE_GATEWAY_DATABASE==='1' ? 'postgres\\ngateway-db\\nmigrate\\ngateway' : 'postgres\\nmigrate\\ngateway');
  if (command==='compose' && args.includes('run')) {
    if (args.includes('--no-build')) {console.error('unknown flag: --no-build');process.exit(2);}
    if (args[args.indexOf('--pull')+1]!=='never') {console.error('migration may pull an image');process.exit(3);}
  }
  if (command==='compose' && args.includes('up') && !args.includes('--no-build')) {console.error('up may build');process.exit(5);}
  if (command==='compose' && args.includes('run') && process.env.FIXTURE_MIGRATION_FAIL==='1') {console.error('migration failed');process.exit(6);}
  process.exit(0);
}
`);
  let healthCalls=0;
  const server=http.createServer((request,response)=>{healthCalls++;response.end('{"ok":true}');});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const trace=path.join(directory,'docker-trace.jsonl');
  const project='fixture-candidate';
  const child=spawn(process.execPath,[path.join(directory,'scripts','deploy.mjs'),directory,project,options.mode || 'deploy'],{
    cwd:directory,windowsHide:true,env:{...process.env,POSTGRES_PASSWORD:'fixture-only',CONSOLE_PORT:String(server.address().port),DOCKER_COMMAND:process.execPath,
      NODE_OPTIONS:'--import '+pathToFileURL(executable).href,DOCKER_TRACE:trace,
      FIXTURE_MISSING_IMAGE:options.missingImage ? '1' : '',FIXTURE_MIGRATION_FAIL:options.migrationFail ? '1' : '',FIXTURE_GATEWAY_DATABASE:options.gatewayDatabase ? '1' : ''},
    stdio:['ignore','pipe','pipe'],
  });
  let stdout='',stderr='';child.stdout.on('data',chunk=>stdout+=chunk);child.stderr.on('data',chunk=>stderr+=chunk);
  const exitCode=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('close',resolve);});
  await new Promise(resolve=>server.close(resolve));
  return {directory,sha,exitCode,stdout,stderr,healthCalls,calls:(await readFile(trace,'utf8')).trim().split('\n').map(line=>JSON.parse(line)),state:path.join(directory,'.local','deploy',project+'.json')};
}

test('deployment runs migration with supported CLI options using only loaded original images',async () => {
  const result=await deploymentFixture();
  try {
    assert.equal(result.exitCode,0,result.stderr);
    assert.equal(result.healthCalls,1);
    assert.equal(JSON.parse(await readFile(result.state,'utf8')).sha,result.sha);
    assert.equal(await verifyCandidate(result.directory),result.sha,'original package stays intact');
    assert.ok(result.calls.some(call=>call.args.includes('run')),'migration actually ran');
    const inspected=result.calls.findIndex(call=>call.args[0]==='image' && call.args[1]==='inspect');
    const stopped=result.calls.findIndex(call=>call.args.includes('stop'));
    assert.ok(inspected>=0 && inspected<stopped,'original candidate images must exist before stopping the prior application');
    for (const component of ['backend','gateway','frontend']) assert.ok(result.calls[inspected].args.includes('kapibala-'+component+':'+result.sha));
    assert.doesNotMatch(result.stdout+result.stderr,/fixture-only/,'private config is never printed');
  } finally {await rm(result.directory,{recursive:true,force:true});}
});

test('a candidate with a separate gateway database initializes it before gateway startup',async () => {
  const result=await deploymentFixture({gatewayDatabase:true});
  try {
    assert.equal(result.exitCode,0,result.stderr);
    const initialized=result.calls.findIndex(call=>call.args.includes('run') && call.args.at(-1)==='gateway-db');
    const started=result.calls.findIndex(call=>call.args.includes('up') && call.args.includes('gateway'));
    assert.ok(initialized>=0 && initialized<started,'gateway database must exist before starting gateway');
  } finally {await rm(result.directory,{recursive:true,force:true});}
});

test('missing original image refuses deployment before stopping the application',async () => {
  const result=await deploymentFixture({missingImage:true});
  try {
    assert.equal(result.exitCode,1);
    assert.equal(result.healthCalls,0);
    assert.equal(result.calls.some(call=>call.args[0]==='compose'),false,'no application or database mutation after missing image');
    await assert.rejects(readFile(result.state),{code:'ENOENT'});
  } finally {await rm(result.directory,{recursive:true,force:true});}
});

test('migration failure preserves the failed state without starting application or recording acceptance',async () => {
  const result=await deploymentFixture({migrationFail:true});
  try {
    assert.equal(result.exitCode,1);
    assert.equal(result.healthCalls,0);
    assert.equal(result.calls.some(call=>call.args.includes('up') && call.args.includes('backend')),false);
    await assert.rejects(readFile(result.state),{code:'ENOENT'});
  } finally {await rm(result.directory,{recursive:true,force:true});}
});

test('rollback reuses original images without running migrations or rebuilding',async () => {
  const result=await deploymentFixture({mode:'rollback'});
  try {
    assert.equal(result.exitCode,0,result.stderr);
    assert.equal(result.healthCalls,1);
    assert.equal(result.calls.some(call=>call.args.includes('run')),false);
    assert.equal(JSON.parse(await readFile(result.state,'utf8')).mode,'rollback');
  } finally {await rm(result.directory,{recursive:true,force:true});}
});

test('candidate verification binds the exact commit and refuses tampered images before Docker', async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(),'kapibala-candidate-'));
  try {
    const files = {'COMMIT_SHA':'a'.repeat(40)+'\n','images.tar.gz':'fake archive','compose.yaml':'services: {}\n'};
    for(const [name,body] of Object.entries(files)) await writeFile(path.join(dir,name),body);
    await writeFile(path.join(dir,'SHA256SUMS'),Object.entries(files).map(([name,body])=>createHash('sha256').update(body).digest('hex')+'  '+name).join('\n')+'\n');
    assert.equal(await verifyCandidate(dir),'a'.repeat(40));
    await writeFile(path.join(dir,'images.tar.gz'),'changed archive');
    await assert.rejects(verifyCandidate(dir),/checksum mismatch/);
  } finally {await rm(dir,{recursive:true,force:true});}
});
