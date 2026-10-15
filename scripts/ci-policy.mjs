import {readFileSync,appendFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';

const conventional = /^(feat|fix|hotfix|docs|test|refactor|chore|ci|build|perf|style|revert)(\([a-zA-Z0-9_.\/-]+\))?!?: .+$/;
export function validatePR({base, head, title, sameRepository}) {
  if (!conventional.test(title)) throw new Error('PR title must use a Conventional Commit type');
  if (base === 'main') {
    if (head !== 'dev' || !sameRepository) throw new Error('main accepts only a promotion PR from this repository dev branch');
  } else if (base !== 'dev' || !/^(feat|fix|hotfix|docs|test|refactor|chore|ci)\/.+/.test(head)) {
    throw new Error('Development branches must use an approved prefix and target dev');
  }
}
export function validateGate(needs) {
  const docs=needs.policy?.result==='success' && needs.policy?.outputs?.mode==='docs';
  for (const job of ['policy', 'go', 'frontend', 'e2e', 'artifact']) {
    const expected=docs && job!=='policy' ? 'skipped' : 'success';
    if (needs[job]?.result !== expected) throw new Error(`${job}: ${needs[job]?.result || 'missing'} blocks CI Gate (expected ${expected})`);
  }
}

function changeMode(eventName,event) {
  let base,head,range;
  if (eventName==='pull_request') {
    base=event?.pull_request?.base?.sha;head=event?.pull_request?.head?.sha;range='...';
  } else if (eventName==='push') {
    base=event?.before;head=event?.after;range='..';
  }
  if (![base,head].every(sha=>typeof sha==='string' && /^[a-f0-9]{40}$/.test(sha) && !/^0+$/.test(sha))) return 'full';
  try {
    // Disable rename detection so moving code into a documentation path also
    // reports the removed source path. NUL separation preserves unusual names.
    const paths=execFileSync('git',['diff','--no-renames','--name-only','-z',base+range+head,'--'],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).split('\0').filter(Boolean);
    const documentation=/^(?:(?:README|CONTRIBUTING|AGENTS|CHANGELOG)\.md|docs\/[^\0]+\.(?:md|rst|txt))$/;
    return paths.length>0 && paths.every(file=>documentation.test(file)) ? 'docs' : 'full';
  } catch {return 'full';}
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv[2] === 'gate') {
      validateGate(JSON.parse(process.env.CI_NEEDS || '{}'));
    } else if (process.argv[2] === 'mode') {
      let event={};
      try {event=JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH,'utf8'));} catch {}
      const mode=changeMode(process.env.GITHUB_EVENT_NAME,event);
      if (!process.env.GITHUB_OUTPUT) throw new Error('GITHUB_OUTPUT is required for CI mode');
      appendFileSync(process.env.GITHUB_OUTPUT,`mode=${mode}\n`);
      console.log(`CI change mode: ${mode}`);
    } else if (process.env.GITHUB_EVENT_NAME === 'pull_request') {
      const pr = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')).pull_request;
      validatePR({base: pr.base.ref, head: pr.head.ref, title: pr.title, sameRepository: pr.head.repo?.full_name === pr.base.repo.full_name});
      const subjects = execFileSync('git', ['log', '--format=%s', '--no-merges', `${pr.base.sha}..${pr.head.sha}`], {encoding:'utf8'}).trim().split('\n').filter(Boolean);
      for (const subject of subjects) if (!conventional.test(subject)) throw new Error('Non-conventional commit subject detected');
    }
    console.log('CI policy passed');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
