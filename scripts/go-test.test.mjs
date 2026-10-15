import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {allowedSkip} from './go-test.mjs';

const platform = 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform';
const provider = 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider';
const gateway = 'github.com/lbtlm/KapibalaAI_gotd/gateway/internal/gateway';

test('gateway tests have no dormant helper skip exemption', () => {
  const helper = {Package:gateway,Test:'TestGatewayCrashChild'};
  assert.equal(allowedSkip('gateway',helper,{}),false);
  assert.equal(allowedSkip('gateway',helper,{GATEWAY_CRASH_CHILD:'0'}),false);
  assert.equal(allowedSkip('gateway',helper,{GATEWAY_CRASH_CHILD:'1'}),false);
  assert.equal(allowedSkip('backend',helper,{}),false);
  assert.equal(allowedSkip('gateway',{...helper,Package:platform},{}),false);
  assert.equal(allowedSkip('gateway',{...helper,Test:'TestGatewayCrashChildOther'},{}),false);
  assert.equal(allowedSkip('gateway',{Package:gateway,Test:'TestGatewayHardKillRetainsAcceptedEffectsAndSSEHistory'},{}),false);
});

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
  for (const name of ['TestLiveProviderToolConversationAndAudit','TestLiveAmbiguousMessageRequestsAuditedReply']) {
    const live = {Package:provider,Test:name};
    assert.equal(allowedSkip('backend',live,{}),true,name);
    assert.equal(allowedSkip('backend',live,{RUN_LIVE_PROVIDER:'false'}),true,name);
    assert.equal(allowedSkip('backend',live,{RUN_LIVE_PROVIDER:'true'}),false,name);
    assert.equal(allowedSkip('gateway',live,{}),false,name);
    assert.equal(allowedSkip('backend',{...live,Package:platform},{}),false,name);
  }
  assert.equal(allowedSkip('backend',{Package:provider,Test:'TestLiveOtherProviderCoverage'},{}),false);
  assert.equal(allowedSkip('backend',{Package:provider,Test:'TestLiveAmbiguousMessageRequestsAuditedReply/subtest'},{}),false);
});

test('missing database or real gateway configuration never exempts integration coverage', () => {
  for (const [module,Package,Test] of [
    ['backend',platform,'TestAccountDeleteAndLeaveAdmissionSerializeAgainstRealEmptyGateway'],
    ['backend',provider,'TestChatProviderPersistsReasoningAcrossRestartAndValidatesTools'],
    ['gateway',gateway,'TestPostgresDeletedAccountRejectsLateSDKStorageWrites'],
  ]) assert.equal(allowedSkip(module,{Package,Test},{}),false,Test);
});

test('every opt-in live provider test is registered with the CI skip guard', () => {
  const source = readFileSync(new URL('../backend/internal/provider/live_test.go',import.meta.url),'utf8');
  const liveTests = [...source.matchAll(/^func (Test\w+)\(t \*testing\.T\) \{/gm)];
  assert.ok(liveTests.length > 0,'live provider tests must be discovered');
  for (const [,Test] of liveTests) {
    assert.equal(allowedSkip('backend',{Package:provider,Test},{}),true,Test);
    assert.equal(allowedSkip('backend',{Package:provider,Test},{RUN_LIVE_PROVIDER:'true'}),false,Test);
  }
});

test('a package-level skip requires observed no-test-files compilation evidence', () => {
  const pkg = 'github.com/lbtlm/KapibalaAI_gotd/backend/cmd/server';
  const compiled = new Set([pkg]);
  assert.equal(allowedSkip('backend',{Package:pkg},{},compiled),true);
  assert.equal(allowedSkip('backend',{Package:pkg},{},new Set()),false);
  assert.equal(allowedSkip('backend',{Package:pkg,Test:'TestMissingCoverage'},{},compiled),false);
});
