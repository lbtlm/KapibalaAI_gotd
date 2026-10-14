import {readFileSync} from 'node:fs';
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
  for (const job of ['policy', 'go', 'frontend', 'e2e', 'artifact']) {
    if (needs[job]?.result !== 'success') throw new Error(`${job}: ${needs[job]?.result || 'missing'} blocks CI Gate`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv[2] === 'gate') {
      validateGate(JSON.parse(process.env.CI_NEEDS || '{}'));
    } else if (process.env.GITHUB_EVENT_NAME === 'pull_request') {
      const pr = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')).pull_request;
      validatePR({base: pr.base.ref, head: pr.head.ref, title: pr.title, sameRepository: pr.head.repo?.full_name === pr.base.repo.full_name});
      const subjects = execFileSync('git', ['log', '--format=%s', '--no-merges', `${pr.base.sha}..${pr.head.sha}`], {encoding:'utf8'}).trim().split('\n').filter(Boolean);
      for (const subject of subjects) if (!conventional.test(subject)) throw new Error('Non-conventional commit subject detected');
    }
    console.log('CI policy passed');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
