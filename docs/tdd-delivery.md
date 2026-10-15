# Delivery TDD evidence

Rowan, 2026-10-14; scripts/workflows/Compose/documentation scope. Reused this repository's backend and gateway Dockerfiles and entrypoints; no business implementation copied or duplicated. Native stack uses explicit migrate, gateway fake, provider scripted, backend server and the frontend's existing Playwright webServer seam.

| Behavior | Red evidence | Green evidence |
|---|---|---|
| Missing E2E DSN refuses before build/migrate | `node --test scripts/fake-stack.test.mjs`: script absent, expectation failed | Dedicated DSN guard, same test passed |
| Existing listener is preserved | Same test suite: absent script, expectation failed | Port preflight refuses; existing test listener remains listening |
| Missing Go terminates promptly | Added regression: actual exit 13 (unsettled top-level await), expected 1 failed | Remove never-started child from tracked set on error: exit 1, ~100ms |
| Work branches target dev, main accepts repository dev only | `node --test scripts/ci-policy.test.mjs`: missing policy module | All approved prefixes accepted on dev, rejected on main; foreign dev rejected |
| CI Gate refuses skip/cancel/fail/missing | Same red policy module failure | Assertions passed for all four blocking states |
| Candidate SHA/checksum verified before Docker | `node --test scripts/deploy.test.mjs`: missing deploy module | Untampered SHA accepted; changed image archive refused |

R1 `node --test scripts/*.test.mjs`: 6/6 passed, no skips. `node scripts/go-test.mjs backend` without DSN: exit 1 with explicit integration DSN requirement. R2 updates the precise skip rules below without exempting real contract tests.

Native actual stack: created only isolated `kapibala_e2e` on local PostgreSQL 16.11; built shared development snapshot through Node runner; explicit migration logged schemaVersion=5; gateway/provider/backend started and all health probes passed on 18081/18082/18080. Default 8080 belonged to another process and was preserved. Go environment was process-local `GOCACHE=tmp/go-cache-gateway`, `GOTMPDIR=tmp`, `GOTOOLCHAIN=local`. No real account, provider key or live database used. Runtime evidence is development composition feedback, not final candidate acceptance because backend/frontend were still changing.

`docker compose config --quiet`: passed using local URI-safe test password. Docker Engine server version readback 29.8.2. `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/ci.yml .github/workflows/promote.yml`: passed. Workflow syntax, immutable artifact and needs result handling checked against the official GitHub documentation linked in operations.md. Recursive Docker exclusions prevent frontend's local Windows node_modules, test output and CodeGraph data being copied into Linux images.

At R1 handoff, remote CI execution, Linux container build/start, same immutable candidate functional acceptance, two-version upgrade/rollback and production deployment were pending. Subsequent remote evidence is recorded under R3 below. Real Telegram/LLM evidence belongs to the controller and component records; this delivery scope does not claim it.

Local frontend image build reached Docker's base-image pull (Node 22.14.0 alpine and nginx alpine), then was deliberately cancelled after observing slow registry transfer (~9 MB of the 50 MB Node layer in 270 seconds) to hand off the stable delivery changes without delaying combined acceptance. No image build success or container startup is claimed. The existing Docker credential helper was made available only through process-local PATH; no global Docker configuration or unrelated containers were changed. CI still requires the complete image build and artifact job to pass.

Controller integration handoff: after rebuilding the native fake stack with the invite-body fix, the targeted actual-stack C3 test passed (1 test, 8.1s). The controller reported the viewer check passed in its preceding run. These are controller-provided local integration results; remote CI and stable candidate acceptance remain separate. The owned slow Docker build was cancelled and exited. The temporary native fake stack later ceased listening, as verified by the controller; its employee tool session is not a persistent startup entrypoint. The controller will rebuild and own the final acceptance stack.

## R2 targeted CI wiring

- Skip policy test first failed because `allowedSkip` was absent; after the in-place runner adaptation the exact dormant `TestServerProcessHelper` is allowed only in its backend platform package. Enabled helper, wrong package, missing test name and `TestGatewayServiceGroupJobContract` remain refused. Opt-in live provider exception also requires its exact package and disabled live flag.
- Observed actual `go -C backend test -json ./cmd/server`: Go emits `[no test files]` output followed by package-level skip. Added regression failed (`false !== true`), then passed after accepting that skip only when the matching package had already emitted the official compilation-only marker. A named skipped test remains refused even with that package marker.
- Actual wrapper command filtered to `^TestServerProcessHelper$`: exit 0, dormant helper and compilation-only entrypoints accepted. Actual wrapper filtered to `^TestGatewayServiceGroupJobContract$` with missing integration URL: exit 1 and `Unexpected skipped test` even though Go itself reported PASS. Neither command reran business suites or used real accounts.
- Image smoke test first failed because the module was absent; after implementation, controlled HTTP fixtures prove gateway/provider/backend health plus root/built-JS/login contracts are required. A healthy proxy with missing access token is rejected. Tests use no Docker downloads, real provider or Telegram resources.
- Go CI now creates a separate integration database, builds/starts the actual fake gateway, supplies `GATEWAY_INTEGRATION_URL` to both normal and race backend runs, and stops only its owned PID. No exception was added for the real gateway contract test.
- Artifact CI now builds images, starts the same SHA images with an isolated CI Compose override, runs the HTTP smoke and always cleans only that disposable project. Packaging occurs only after successful runtime verification and cleanup. Actual remote run/container verification remains for the controller; no local slow image download was retried in R2.

R2 validation: `node --test scripts/*.test.mjs` **11/11 passed, zero skips**; actionlint 1.7.7 for both workflows passed; `docker compose -f compose.yaml -f compose.ci.yaml config --quiet` and scoped `git diff --check` passed. The first HTTP-fixture test attempt under restricted loopback permissions could not connect; rerunning only the script suite with approved loopback execution passed. These are script/configuration checks, not an actual container smoke or remote CI success.

## R3 gateway subprocess helper and remote evidence

Rowan runtime ID: `/root/rowan_ci_delivery`. Owned changes are limited to `scripts/go-test.mjs`, `scripts/go-test.test.mjs` and this document. CodeGraph supplied the current helper and skip policy before implementation.

Added one precise rule: module `gateway`, package `github.com/lbtlm/KapibalaAI_gotd/gateway`, test `TestGatewayCrashChild`, and `GATEWAY_CRASH_CHILD != "1"`. This matches the dormant subprocess helper's source guard and its reason `subprocess helper only; GATEWAY_CRASH_CHILD=1`. The activated helper, wrong module/package/name and actual `TestGatewayHardKillRetainsAcceptedEffectsAndSSEHistory` remain rejected as skips; the real crash test uses its own disposable database and receives no exemption.

TDD: first `node --test scripts/go-test.test.mjs` failed on inactive gateway helper (`false !== true`, 3 pass/1 fail). The three-line implementation then passed the same targeted suite **4/4, zero skips**; scoped diff check passed. No business suite, real account operation, Docker download or Git mutation was performed in R3.

The controller reported [CI run 38592943938](https://github.com/lbtlm/KapibalaAI_gotd/actions/runs/38592943938) passed all six jobs, including actual image build, Compose startup/HTTP smoke and CI Gate. This is controller-provided remote evidence for that earlier candidate, not independently re-fetched by Rowan and not evidence for subsequent gateway/backend/frontend repairs. Final repaired source requires a fresh CI run and stable acceptance. No production deployment, Release or tag publication is claimed; two-version upgrade/rollback remains unverified.

## R15 attempt1 revision1: U16 WebSocket delivery

Rowan, 2026-10-15, runtime `/root/rowan_repairs_20261015`. Reused the existing `scripts/compose-smoke.mjs` HTTP/login seam and its in-memory login token; no tokens are printed or saved. The Nginx `/ws` location now forwards `$http_host`, including the original port. Backend origin/authentication rules are unchanged. Node 22.14's built-in WebSocket client cannot set Origin, so the smoke adds a bounded HTTP Upgrade and first-auth-frame probe with Node's built-in HTTP/crypto modules; no package or second framework was added.

- Red: `node --test scripts/compose-smoke.test.mjs`, exit 1, 2 passed / 4 failed. The previous smoke falsely accepted same-origin 403, cross-origin 101, invalid-token success and valid-token failure. Evidence: `.local/repairs-20261015/rowan/ws-red.txt`.
- Green: same command, exit 0, 6 passed / zero skips. The existing healthy fixture now requires same-origin 101 and `{type:"auth",success:true}`, invalid-token `{type:"auth",success:false}`, and cross-origin HTTP 403. Each bad behavior fails smoke. Evidence: `.local/repairs-20261015/rowan/ws-green.txt`.
- Prior real-container red/green proof supplied by the controller remains `.local/acceptance/docker-ws-20261015-rowan/{red,green}.json`: original same-origin 403, temporary corrected configuration same-origin 101, cross-origin 403 in both. This is prior controller evidence; this task did not restart/build/stop the user demo or claim a new container test.

## R15 attempt1 revision1: lightweight documentation CI

Adapted the existing policy script and required `CI Gate` in place. Policy still checks PR routing/title/commit subjects and runs delivery script tests. Its explicit docs/full output controls the existing four expensive jobs; no workflow path filter, protection change, replacement check or automatic push was introduced. The Gate permits only successful policy plus exactly four skipped jobs in verified docs mode. Every other mode requires all five successful results.

- Red: `node --test scripts/ci-policy.test.mjs`, exit 1, 2 passed / 2 failed: docs skips were refused and the CLI produced no classification output. Evidence: `.local/repairs-20261015/rowan/ci-red.txt`.
- Green: same command, exit 0, 4 passed / zero skips. The public CLI runs against an isolated temporary Git repository, covering PR and push doc diffs, empty/null/missing/unknown/zero baselines, dispatch, moved code with its old path, config deletion, scripts, dependencies, workflows, unknown Markdown and executable MDX. The Gate exercises failure/cancel/skip/missing results for both modes. Evidence: `.local/repairs-20261015/rowan/ci-green.txt`.
- `node --test scripts/*.test.mjs`: exit 0, 18 passed / zero skips, evidence `.local/repairs-20261015/rowan/scripts-green.txt`. The final null-event guard touched only policy; its targeted 4-test rerun above passed afterward. Restricted execution initially blocked loopback/temp Git metadata; the stopped initial fixture run was rerun with approved local access, without contacting real accounts or services.
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/ci.yml .github/workflows/promote.yml`: exit 0, no diagnostics; `.local/repairs-20261015/rowan/actionlint.txt`. The initial restricted run could not resolve the configured Go proxy; the approved syntax-check rerun passed. Workflow output/condition semantics were checked against the first-party GitHub docs linked in operations.md. Scoped `git diff --check` passed.

The `edited` PR trigger remains because PR title/base routing is part of policy. Remote docs/full CI outcomes and the final same-image WS smoke require the controller's stable snapshot and next CI run; they are not claimed by these local checks. No production deployment, tag, Release, Telegram/LLM call or two-version upgrade/rollback was performed.
