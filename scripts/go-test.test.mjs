import {test} from 'node:test';
import assert from 'node:assert/strict';
import {allowedSkip} from './go-test.mjs';

const platform = 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform';
const provider = 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider';

test('only the exact dormant subprocess helper can skip platform coverage', () => {
  const helper = {Package:platform,Test:'TestServerProcessHelper'};
  assert.equal(allowedSkip('backend',helper,{}),true);
  assert.equal(allowedSkip('backend',helper,{KAPIBALA_CHILD_SERVER:'true'}),false);
  assert.equal(allowedSkip('gateway',helper,{}),false);
  assert.equal(allowedSkip('backend',{...helper,Package:provider},{}),false);
  assert.equal(allowedSkip('backend',{Package:platform,Test:'TestGatewayServiceGroupJobContract'},{}),false);
  assert.equal(allowedSkip('backend',{Package:platform},{}),false);
});

test('live-provider skip is explicit and does not exempt enabled live acceptance', () => {
  const live = {Package:provider,Test:'TestLiveProviderToolConversationAndAudit'};
  assert.equal(allowedSkip('backend',live,{}),true);
  assert.equal(allowedSkip('backend',live,{RUN_LIVE_PROVIDER:'true'}),false);
});

test('a package-level skip requires observed no-test-files compilation evidence', () => {
  const pkg = 'github.com/lbtlm/KapibalaAI_gotd/backend/cmd/server';
  const compiled = new Set([pkg]);
  assert.equal(allowedSkip('backend',{Package:pkg},{},compiled),true);
  assert.equal(allowedSkip('backend',{Package:pkg},{},new Set()),false);
  assert.equal(allowedSkip('backend',{Package:pkg,Test:'TestMissingCoverage'},{},compiled),false);
});
