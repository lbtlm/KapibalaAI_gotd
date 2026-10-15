import {readFileSync,appendFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';

const conventional = /^(feat|fix|hotfix|docs|test|refactor|chore|ci|build|perf|style|revert)(\([a-zA-Z0-9_.\/-]+\))?!?: .+$/;
const validSHA=sha=>typeof sha==='string' && /^[a-f0-9]{40}$/.test(sha) && !/^0+$/.test(sha);
export function validatePR({base, head, title, sameRepository}) {
  if (!conventional.test(title)) throw new Error('PR title must use a Conventional Commit type');
  if (base === 'main') {
    if (head !== 'dev' || !sameRepository) throw new Error('main accepts only a promotion PR from this repository dev branch');
  } else if (base !== 'dev' || !/^(feat|fix|hotfix|docs|test|refactor|chore|ci)\/.+/.test(head)) {
    throw new Error('Development branches must use an approved prefix and target dev');
  }
}
export function validateGate(needs) {
  const mode=needs.policy?.outputs?.mode;
  if (needs.policy?.result!=='success' || !['docs','light','full'].includes(mode)) throw new Error('Successful policy and an explicit docs/light/full profile are required');
  const required={policy:true,go_light:mode==='light',go:mode==='full',frontend:mode!=='docs',e2e:mode==='full',artifact:mode==='full'};
  for (const [job,run] of Object.entries(required)) {
    const expected=run ? 'success' : 'skipped';
    if (needs[job]?.result !== expected) throw new Error(`${job}: ${needs[job]?.result || 'missing'} blocks CI Gate (expected ${expected})`);
  }
}

function pullRequest(event) {
  const pr=event?.pull_request;
  if (!pr || !validSHA(pr.base?.sha) || !validSHA(pr.head?.sha) || !pr.base?.repo?.full_name || !pr.head?.repo?.full_name) throw new Error('Complete PR source, target, repository and commit metadata is required');
  validatePR({base:pr.base.ref,head:pr.head.ref,title:pr.title,sameRepository:pr.base.repo.full_name===pr.head.repo.full_name});
  return pr;
}

function changeMode(eventName,event) {
  if (eventName==='workflow_dispatch') {
    if (event?.inputs?.profile!=='full') throw new Error('Manual CI requires explicit profile=full');
    return 'full';
  }
  if (eventName!=='pull_request') throw new Error('CI supports pull_request or explicit manual full only');
  const pr=pullRequest(event);
  if (pr.base.ref==='main') return 'full';
  try {
    // Disable rename detection so moving code into a documentation path also
    // reports the removed source path. NUL separation preserves unusual names.
    const paths=execFileSync('git',['diff','--no-renames','--name-only','-z',pr.base.sha+'...'+pr.head.sha,'--'],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).split('\0').filter(Boolean);
    const documentation=/^(?:(?:README|CONTRIBUTING|AGENTS|CHANGELOG)\.md|docs\/[^\0]+\.(?:md|rst|txt))$/;
    return paths.length>0 && paths.every(file=>documentation.test(file)) ? 'docs' : 'light';
  } catch {return 'light';}
}

function acceptedCandidate(run,candidate,source,sha,repository) {
  if (run.name!=='CI' || run.path!=='.github/workflows/ci.yml' || run.status!=='completed' || run.conclusion!=='success' || run.repository?.full_name!==repository || !Number.isInteger(run.run_attempt) || run.run_attempt<1) throw new Error('Candidate requires a successful CI run from this repository');
  if (!validSHA(sha) || !validSHA(run.head_sha) || candidate.sha!==sha || source.sha!==run.head_sha || !validSHA(candidate.commit?.tree?.sha) || candidate.commit.tree.sha!==source.commit?.tree?.sha) throw new Error('Candidate commit, source and accepted tree identities must match');
  if (run.event==='pull_request') {
    const pr=run.pull_requests?.length===1 ? run.pull_requests[0] : null;
    if (!pr || pr.base?.ref!=='main' || pr.head?.ref!=='dev' || run.head_branch!=='dev' || !validSHA(pr.base.sha) || !run.repository?.id || pr.base?.repo?.id!==run.repository.id || pr.head?.repo?.id!==run.repository.id || pr.head.sha!==run.head_sha) throw new Error('Only this repository dev-to-main release PR may supply a PR candidate');
    if (candidate.parents?.length!==2 || candidate.parents[0].sha!==pr.base.sha || candidate.parents[1].sha!==pr.head.sha) throw new Error('Tested merge parents must match the accepted release source');
  } else if (!['push','workflow_dispatch'].includes(run.event) || !['dev','main'].includes(run.head_branch) || sha!==run.head_sha) {
    throw new Error('Only release PR, historical push or manual dev/main full candidates are accepted');
  }
  return {name:`candidate-${sha}-${run.run_attempt}`,tree:candidate.commit.tree.sha};
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv[2]==='candidate') {
      const json=file=>JSON.parse(readFileSync(file,'utf8'));
      const candidate=acceptedCandidate(json('run.json'),json('candidate-commit.json'),json('source-commit.json'),process.env.ACCEPTED_SHA,process.env.GITHUB_REPOSITORY);
      appendFileSync(process.env.GITHUB_ENV,`CANDIDATE_NAME=${candidate.name}\nCANDIDATE_TREE=${candidate.tree}\n`);
    } else if (process.argv[2] === 'gate') {
      validateGate(JSON.parse(process.env.CI_NEEDS || '{}'));
    } else if (process.argv[2] === 'mode') {
      const event=JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH,'utf8'));
      const mode=changeMode(process.env.GITHUB_EVENT_NAME,event);
      if (!process.env.GITHUB_OUTPUT) throw new Error('GITHUB_OUTPUT is required for CI mode');
      appendFileSync(process.env.GITHUB_OUTPUT,`mode=${mode}\n`);
      console.log(`CI change mode: ${mode}`);
    } else if (process.env.GITHUB_EVENT_NAME === 'pull_request') {
      const pr=pullRequest(JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH,'utf8')));
      const subjects = execFileSync('git', ['log', '--format=%s', '--no-merges', `${pr.base.sha}..${pr.head.sha}`], {encoding:'utf8'}).trim().split('\n').filter(Boolean);
      for (const subject of subjects) if (!conventional.test(subject)) throw new Error('Non-conventional commit subject detected');
    } else {
      changeMode(process.env.GITHUB_EVENT_NAME,JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH,'utf8')));
    }
    console.log('CI policy passed');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
