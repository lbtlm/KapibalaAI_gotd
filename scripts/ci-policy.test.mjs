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

test('CI Gate requires every exact docs/light/full job result and rejects missing or unknown profiles', () => {
  const expected={
    docs:{policy:'success',go_light:'skipped',go:'skipped',frontend:'skipped',e2e:'skipped',artifact:'skipped'},
    light:{policy:'success',go_light:'success',go:'skipped',frontend:'success',e2e:'skipped',artifact:'skipped'},
    full:{policy:'success',go_light:'skipped',go:'success',frontend:'success',e2e:'success',artifact:'success'},
  };
  for (const [mode,results] of Object.entries(expected)) {
    const needs=Object.fromEntries(Object.entries(results).map(([job,result])=>[job,{result}]));
    needs.policy.outputs={mode};
    assert.doesNotThrow(()=>validateGate(needs));
    for (const [job,result] of Object.entries(results)) {
      for (const wrong of ['success','failure','cancelled','skipped',undefined].filter(value=>value!==result)) {
        assert.throws(()=>validateGate({...needs,[job]:{...needs[job],result:wrong}}),mode+' '+job+' '+wrong);
      }
      const missing={...needs};delete missing[job];assert.throws(()=>validateGate(missing));
    }
    for (const invalidMode of ['',undefined,'unknown']) assert.throws(()=>validateGate({...needs,policy:{result:'success',outputs:{mode:invalidMode}}}));
  }
});

test('CI public mode routes dev PRs to light/docs, release PRs and explicit manual runs to full',async () => {
  const directory=await mkdtemp(path.join(tmpdir(),'kapibala-ci-mode-'));
  const script=fileURLToPath(new URL('./ci-policy.mjs',import.meta.url));
  const git=(...args)=>execFileSync('git',args,{cwd:directory,encoding:'utf8'}).trim();
  const commit=()=>{git('add','--all');git('-c','user.name=CI Fixture','-c','user.email=fixture@example.invalid','commit','-qm','docs: fixture');return git('rev-parse','HEAD');};
  const pr=(baseSHA,headSHA,overrides={})=>({pull_request:{
    title:'fix: fixture',base:{ref:'dev',sha:baseSHA,repo:{full_name:'owner/repository'}},head:{ref:'fix/fixture',sha:headSHA,repo:{full_name:'owner/repository'}},...overrides,
  }});
  const classify=async (name,event,{rejected=false}={}) => {
    const eventPath=path.join(directory,'.git','event.json');
    const outputPath=path.join(directory,'.git','output.txt');
    await writeFile(eventPath,JSON.stringify(event));await writeFile(outputPath,'');
    const run=()=>execFileSync(process.execPath,[script,'mode'],{cwd:directory,env:{...process.env,GITHUB_EVENT_NAME:name,GITHUB_EVENT_PATH:eventPath,GITHUB_OUTPUT:outputPath},encoding:'utf8',stdio:['ignore','pipe','pipe']});
    if (rejected) {assert.throws(run);assert.equal(await readFile(outputPath,'utf8'),'');return;}
    run();
    return (await readFile(outputPath,'utf8')).trim();
  };
  try {
    git('init','--quiet');
    await writeFile(path.join(directory,'README.md'),'one');
    await writeFile(path.join(directory,'code.go'),'package fixture');
    const baseline=commit();
    await writeFile(path.join(directory,'README.md'),'two');
    const docs=commit();
    assert.equal(await classify('pull_request',pr(baseline,docs)),'mode=docs');
    assert.equal(await classify('pull_request',pr(docs,docs)),'mode=light','empty diff cannot bypass light tests');
    assert.equal(await classify('pull_request',pr('f'.repeat(40),docs)),'mode=light','unavailable diff cannot bypass light tests');
    assert.equal(await classify('workflow_dispatch',{inputs:{profile:'full'}}),'mode=full');
    await classify('workflow_dispatch',{}, {rejected:true});
    await classify('workflow_dispatch',{inputs:{profile:'light'}},{rejected:true});
    await classify('push',{before:baseline,after:docs},{rejected:true});
    await classify('pull_request_target',pr(baseline,docs),{rejected:true});
    for (const event of [{},null,pr(undefined,docs),pr('0'.repeat(40),docs),pr(baseline,undefined),pr(baseline,docs,{base:{ref:'main',sha:baseline}}),pr(baseline,docs,{head:{ref:'main',sha:docs,repo:{full_name:'owner/repository'}}})]) {
      await classify('pull_request',event,{rejected:true});
    }
    const release=pr(baseline,docs,{base:{ref:'main',sha:baseline,repo:{full_name:'owner/repository'}},head:{ref:'dev',sha:docs,repo:{full_name:'owner/repository'}}});
    assert.equal(await classify('pull_request',release),'mode=full','even docs-only dev-to-main runs full');
    await classify('pull_request',pr(baseline,docs,{base:release.pull_request.base}),{rejected:true});
    await classify('pull_request',pr(baseline,docs,{base:release.pull_request.base,head:{ref:'dev',sha:docs,repo:{full_name:'foreign/repository'}}}),{rejected:true});
    await mkdir(path.join(directory,'docs'));
    await writeFile(path.join(directory,'docs','guide.md'),'guide');
    const guide=commit();
    assert.equal(await classify('pull_request',pr(docs,guide)),'mode=docs');
    await rename(path.join(directory,'code.go'),path.join(directory,'AGENTS.md'));
    const renamed=commit();
    assert.equal(await classify('pull_request',pr(docs,renamed)),'mode=light','renamed source still appears in changed paths');
    await writeFile(path.join(directory,'runtime.yaml'),'services: {}');
    const config=commit();
    assert.equal(await classify('pull_request',pr(renamed,config)),'mode=light');
    await rm(path.join(directory,'runtime.yaml'));
    const deleted=commit();
    assert.equal(await classify('pull_request',pr(config,deleted)),'mode=light');
    let previous=deleted;
    for (const file of ['scripts/instructions.md','frontend/package.json','.github/workflows/ci.yml','unknown.md','docs/runtime.yaml','docs/example.mdx']) {
      await mkdir(path.dirname(path.join(directory,file)),{recursive:true});
      await writeFile(path.join(directory,file),'changed');
      const next=commit();
      assert.equal(await classify('pull_request',pr(previous,next)),'mode=light',file+' must run light tests');
      previous=next;
    }
  } finally {await rm(directory,{recursive:true,force:true});}
});

test('candidate CLI reuses the release PR merge artifact and refuses mismatched source, parents or tree',async () => {
  const directory=await mkdtemp(path.join(tmpdir(),'kapibala-candidate-policy-'));
  const script=fileURLToPath(new URL('./ci-policy.mjs',import.meta.url));
  const base='1'.repeat(40),head='2'.repeat(40),merge='3'.repeat(40),tree='4'.repeat(40);
  const run={name:'CI',path:'.github/workflows/ci.yml',status:'completed',conclusion:'success',event:'pull_request',head_branch:'dev',head_sha:head,run_attempt:2,
    repository:{id:7,full_name:'owner/repository'},pull_requests:[{base:{ref:'main',sha:base,repo:{id:7}},head:{ref:'dev',sha:head,repo:{id:7}}}]};
  const candidate={sha:merge,parents:[{sha:base},{sha:head}],commit:{tree:{sha:tree}}};
  const source={sha:head,commit:{tree:{sha:tree}}};
  const select=async (metadata=run,tested=candidate,accepted=merge,sourceCommit=source,rejected=false)=>{
    for (const [file,value] of [['run.json',metadata],['candidate-commit.json',tested],['source-commit.json',sourceCommit]]) await writeFile(path.join(directory,file),JSON.stringify(value));
    const output=path.join(directory,'output.env');await writeFile(output,'');
    const command=()=>execFileSync(process.execPath,[script,'candidate'],{cwd:directory,env:{...process.env,GITHUB_ENV:output,GITHUB_REPOSITORY:'owner/repository',ACCEPTED_SHA:accepted},encoding:'utf8',stdio:['ignore','pipe','pipe']});
    if (rejected) {assert.throws(command);assert.equal(await readFile(output,'utf8'),'');return;}
    command();return readFile(output,'utf8');
  };
  try {
    assert.equal(await select(),`CANDIDATE_NAME=candidate-${merge}-2\nCANDIDATE_TREE=${tree}\n`,'run.head_sha is source HEAD; artifact uses tested merge SHA');
    for (const event of ['push','workflow_dispatch']) {
      const direct={...run,event,head_branch:'main'};
      assert.match(await select(direct,{...candidate,sha:head},head),new RegExp('candidate-'+head+'-2'));
      await select({...direct,head_branch:'fix/a'},candidate,merge,source,true);
    }
    for (const bad of [{...run,status:'in_progress'},{...run,conclusion:'failure'},{...run,event:'pull_request_target'},{...run,repository:{id:8,full_name:'foreign/repository'}},{...run,pull_requests:[]},{...run,run_attempt:undefined},{...run,head_sha:undefined},{...run,head_branch:'fix/a'}]) await select(bad,candidate,merge,source,true);
    for (const pr of [
      {...run.pull_requests[0],base:{ref:'dev',sha:base,repo:{id:7}}},
      {...run.pull_requests[0],head:{ref:'fix/a',sha:head,repo:{id:7}}},
      {...run.pull_requests[0],head:{ref:'dev',sha:head,repo:{id:8}}},
      {...run.pull_requests[0],head:{ref:'dev',sha:'5'.repeat(40),repo:{id:7}}},
    ]) await select({...run,pull_requests:[pr]},candidate,merge,source,true);
    for (const bad of [{...candidate,sha:head},{...candidate,parents:[{sha:head}]},{...candidate,parents:[{sha:head},{sha:base}]},{...candidate,commit:{tree:{sha:'5'.repeat(40)}}}]) await select(run,bad,merge,source,true);
    await select(run,candidate,merge,{...source,sha:base},true);
  } finally {await rm(directory,{recursive:true,force:true});}
});
