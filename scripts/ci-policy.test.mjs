import {test} from 'node:test';
import assert from 'node:assert/strict';
import {validatePR, validateGate} from './ci-policy.mjs';

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
