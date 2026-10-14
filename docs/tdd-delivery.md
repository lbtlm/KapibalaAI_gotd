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

Pending: remote CI actual execution, Linux container build/start, same immutable candidate functional acceptance, two-version upgrade/rollback, production deployment. Real Telegram/LLM evidence belongs to the controller and component records; this delivery scope does not claim it.

Local frontend image build reached Docker's base-image pull (Node 22.14.0 alpine and nginx alpine), then was deliberately cancelled after observing slow registry transfer (~9 MB of the 50 MB Node layer in 270 seconds) to hand off the stable delivery changes without delaying combined acceptance. No image build success or container startup is claimed. The existing Docker credential helper was made available only through process-local PATH; no global Docker configuration or unrelated containers were changed. CI still requires the complete image build and artifact job to pass.

Controller integration handoff: after rebuilding the native fake stack with the invite-body fix, the targeted actual-stack C3 test passed (1 test, 8.1s). The controller reported the viewer check passed in its preceding run. These are controller-provided local integration results; remote CI and stable candidate acceptance remain separate. The owned slow Docker build session29799 was cancelled and exited; native fake-stack PTY session18220 remains running by explicit controller request for continued acceptance.

## R2 targeted CI wiring

- Skip policy test first failed because `allowedSkip` was absent; after the in-place runner adaptation the exact dormant `TestServerProcessHelper` is allowed only in its backend platform package. Enabled helper, wrong package, missing test name and `TestGatewayServiceGroupJobContract` remain refused. Opt-in live provider exception also requires its exact package and disabled live flag.
- Observed actual `go -C backend test -json ./cmd/server`: Go emits `[no test files]` output followed by package-level skip. Added regression failed (`false !== true`), then passed after accepting that skip only when the matching package had already emitted the official compilation-only marker. A named skipped test remains refused even with that package marker.
- Actual wrapper command filtered to `^TestServerProcessHelper$`: exit 0, dormant helper and compilation-only entrypoints accepted. Actual wrapper filtered to `^TestGatewayServiceGroupJobContract$` with missing integration URL: exit 1 and `Unexpected skipped test` even though Go itself reported PASS. Neither command reran business suites or used real accounts.
- Image smoke test first failed because the module was absent; after implementation, controlled HTTP fixtures prove gateway/provider/backend health plus root/built-JS/login contracts are required. A healthy proxy with missing access token is rejected. Tests use no Docker downloads, real provider or Telegram resources.
- Go CI now creates a separate integration database, builds/starts the actual fake gateway, supplies `GATEWAY_INTEGRATION_URL` to both normal and race backend runs, and stops only its owned PID. No exception was added for the real gateway contract test.
- Artifact CI now builds images, starts the same SHA images with an isolated CI Compose override, runs the HTTP smoke and always cleans only that disposable project. Packaging occurs only after successful runtime verification and cleanup. Actual remote run/container verification remains for the controller; no local slow image download was retried in R2.

R2 validation: `node --test scripts/*.test.mjs` **11/11 passed, zero skips**; actionlint 1.7.7 for both workflows passed; `docker compose -f compose.yaml -f compose.ci.yaml config --quiet` and scoped `git diff --check` passed. The first HTTP-fixture test attempt under restricted loopback permissions could not connect; rerunning only the script suite with approved loopback execution passed. These are script/configuration checks, not an actual container smoke or remote CI success.
