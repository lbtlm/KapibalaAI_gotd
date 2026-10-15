import {test} from 'node:test';
import assert from 'node:assert/strict';
import {validatePR, validateGate} from './ci-policy.mjs';
import {mkdtemp,mkdir,writeFile,readFile,rename,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

test('development and hotfix branches target dev; stable promotions come only from dev', () => {
  for (const branch of ['feat/a','fix/a','hotfix/a','docs/a','test/a','refactor/a','chore/a','ci/a']) {
    assert.doesNotThrow(() => validatePR({base:'dev',head:branch,title:'feat(core): deliver behavior',sameRepository:true}));
    assert.throws(() => validatePR({base:'main',head:branch,title:'feat(core): deliver behavior',sameRepository:true}));
  }
  assert.doesNotThrow(() => validatePR({base:'main',head:'dev',title:'chore(release): promote accepted candidate',sameRepository:true}));
  assert.throws(() => validatePR({base:'main',head:'dev',title:'chore: promote candidate',sameRepository:false}));
  assert.throws(() => validatePR({base:'dev',head:'fix/a',title:'misc changes',sameRepository:true}));
});

test('CI Gate refuses failure, cancellation, skip and absent required results', () => {
  const required = ['policy','go','frontend','e2e','artifact'];
  const success = Object.fromEntries(required.map(job => [job,{result:'success'}]));
  assert.doesNotThrow(() => validateGate(success));
  for (const result of ['failure','cancelled','skipped']) {
    assert.throws(() => validateGate({...success,go:{result}}));
  }
  const missing = {...success}; delete missing.artifact;
  assert.throws(() => validateGate(missing));
});

test('only a successful verified docs policy permits exactly the four expensive jobs to skip', () => {
  const docs={policy:{result:'success',outputs:{mode:'docs'}},...Object.fromEntries(['go','frontend','e2e','artifact'].map(job=>[job,{result:'skipped'}]))};
  assert.doesNotThrow(()=>validateGate(docs));
  for (const result of ['failure','cancelled','skipped',undefined]) {
    assert.throws(()=>validateGate({...docs,policy:{result,outputs:{mode:'docs'}}}));
  }
  for (const job of ['go','frontend','e2e','artifact']) {
    for (const result of ['success','failure','cancelled',undefined]) assert.throws(()=>validateGate({...docs,[job]:{result}}));
  }
  for (const mode of ['full','',undefined,'unknown']) assert.throws(()=>validateGate({...docs,policy:{result:'success',outputs:{mode}}}));
});

test('CI mode uses real changed paths, fails closed and includes deleted/renamed code',async () => {
  const directory=await mkdtemp(path.join(tmpdir(),'kapibala-ci-mode-'));
  const script=fileURLToPath(new URL('./ci-policy.mjs',import.meta.url));
  const git=(...args)=>execFileSync('git',args,{cwd:directory,encoding:'utf8'}).trim();
  const commit=()=>{git('add','--all');git('-c','user.name=CI Fixture','-c','user.email=fixture@example.invalid','commit','-qm','docs: fixture');return git('rev-parse','HEAD');};
  const classify=async (name,event) => {
    const eventPath=path.join(directory,'.git','event.json');
    const outputPath=path.join(directory,'.git','output.txt');
    await writeFile(eventPath,JSON.stringify(event));await writeFile(outputPath,'');
    execFileSync(process.execPath,[script,'mode'],{cwd:directory,env:{...process.env,GITHUB_EVENT_NAME:name,GITHUB_EVENT_PATH:eventPath,GITHUB_OUTPUT:outputPath},encoding:'utf8'});
    return (await readFile(outputPath,'utf8')).trim();
  };
  try {
    git('init','--quiet');
    await writeFile(path.join(directory,'README.md'),'one');
    await writeFile(path.join(directory,'code.go'),'package fixture');
    const baseline=commit();
    await writeFile(path.join(directory,'README.md'),'two');
    const docs=commit();
    assert.equal(await classify('push',{before:baseline,after:docs}),'mode=docs');
    assert.equal(await classify('pull_request',{pull_request:{base:{sha:baseline},head:{sha:docs}}}),'mode=docs');
    assert.equal(await classify('push',{before:docs,after:docs}),'mode=full');
    assert.equal(await classify('push',{before:'0'.repeat(40),after:docs}),'mode=full');
    assert.equal(await classify('push',{before:'f'.repeat(40),after:docs}),'mode=full');
    assert.equal(await classify('workflow_dispatch',{}),'mode=full');
    assert.equal(await classify('pull_request',{}),'mode=full');
    assert.equal(await classify('push',null),'mode=full');
    await mkdir(path.join(directory,'docs'));
    await writeFile(path.join(directory,'docs','guide.md'),'guide');
    const guide=commit();
    assert.equal(await classify('push',{before:docs,after:guide}),'mode=docs');
    await rename(path.join(directory,'code.go'),path.join(directory,'AGENTS.md'));
    const renamed=commit();
    assert.equal(await classify('push',{before:docs,after:renamed}),'mode=full');
    assert.equal(await classify('pull_request',{pull_request:{base:{sha:docs},head:{sha:renamed}}}),'mode=full');
    await writeFile(path.join(directory,'runtime.yaml'),'services: {}');
    const config=commit();
    assert.equal(await classify('push',{before:renamed,after:config}),'mode=full');
    await rm(path.join(directory,'runtime.yaml'));
    const deleted=commit();
    assert.equal(await classify('push',{before:config,after:deleted}),'mode=full');
    let previous=deleted;
    for (const file of ['scripts/instructions.md','frontend/package.json','.github/workflows/ci.yml','unknown.md','docs/runtime.yaml','docs/example.mdx']) {
      await mkdir(path.dirname(path.join(directory,file)),{recursive:true});
      await writeFile(path.join(directory,file),'changed');
      const next=commit();
      assert.equal(await classify('push',{before:previous,after:next}),'mode=full',file+' must run full CI');
      previous=next;
    }
  } finally {await rm(directory,{recursive:true,force:true});}
});
