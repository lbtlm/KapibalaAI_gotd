# Development status

Observed 2026-10-15 00:06 Asia/Shanghai. Coordination record, not an acceptance report.

| Employee / assignment | Runtime ID | Scope | Requested runtime | Status |
| --- | --- | --- | --- | --- |
| Niko / Telegram gateway | `/root/niko_gateway_build` | `gateway/`, gateway reuse/TDD docs | GPT-6.1 Sol / Ultra | G3 active: four gateway recovery/lease review findings plus targeted process-kill recovery test |
| Orion / application backend | `/root/orion_backend_build` | `backend/`, backend reuse/TDD docs | GPT-6.1 Sol / Ultra | B2 active: five backend review findings. L1 live acceptance script preparation paused; no live execution authorized in that prep stage |
| Iris / frontend | `/root/iris_frontend_build` | `frontend/`, frontend reuse/TDD and design docs | GPT-6.1 Sol / High | F4 active: actual-browser HttpOnly cookie response ordering; F3 snapshot 5845963 retained, layout frozen |
| Rowan / CI/CD | `/root/rowan_ci_delivery` | Root workflows, Compose, verification/start scripts, README | GPT-6.1 Sol / High | R2 stopped and committed 18716c9; 11 script tests, actionlint and Compose config passed; actual container/remote CI pending |
| Sentry / stable review | CLI `01a13b03-d998-7cb2-85b8-b76547998730` | Stable gateway/frontend at fdebbf3 | GPT-6.1 Sol / High | Completed read-only; five concrete blockers accepted for repair, no optional scope expansion |
| Sentry / backend and CI review | CLI `01a13b1f-5615-7e81-98bf-792f5b17e943` | Frozen backend/CI at fd0a77d | GPT-6.1 Sol / High | Completed; five backend blockers accepted for repair, no extra CI source blocker. Runtime read-only/never/model/high and denied canary independently verified |

Controller owns final acceptance. Worker model/effort were explicitly requested through spawn; Fast is not independently observable. Maximum three active workers. No recursive delegation.

Native `/root/sentry_stable_review` was stopped before review: its live sandbox was workspace-write despite the readonly role file. User approved a separate CLI review. Global CLI 0.98 rejected the requested model; the already-installed desktop CLI 0.162.0-alpha.17.2 preserves the requested model and readonly sandbox. No global installation/configuration changed. The failed native probe was removed; review evidence remains in ignored .local/review.

Sentry RV2 reviewed gateway/frontend only at fdebbf3: persisted real send intent recovery, checkpoint acknowledgement/lease fencing, channel-too-long history recovery, queued stale gateway effects, and late frontend 401/refresh across logout/new login. The frontend interleavings were reproduced with the real source in a write-free transport harness; gateway findings identify static paths plus missing critical validation. Backend/CI remain pending a stable snapshot. Controller independently verified reviewer model/effort from runtime metadata; the reviewer's narrower self-report did not independently inspect those settings.

Controller's F3 acceptance found the browser cookie layer still crosses sessions: actual ApiClient source + isolated Edge + local HTTP fixture, A refresh held, B login succeeds, late A response installs HttpOnly cookie A2 despite JSON rejection, next refresh changes B/viewer to A/admin. Evidence `.local/acceptance/frontend-cookie-race.json` uses synthetic identities only. A same-client cookie-changing request-order repair is queued; no cross-tab feature expansion.

Sentry B-RV1 identified staggered membership event inversion, repeated kick after target rejoin, sent-before-sequence-advance restart scheduling, short downtime counted as online budget, and cross-account slow sends delaying the 504 deadline. Orion owns backend repairs; Niko owns the small durable kick idempotency-key gateway extension. Existing passing tests remain evidence only for their actual windows; these findings block final acceptance.

## Decisions and remaining gates

- Go/gotd and React 18; DeepSeek through OpenAI-compatible API, no Claude. User supplied the key in ignored local configuration. Real provider tool conversation and audit passed without Telegram effects.
- Real test resource paths stay in ignored local configuration. Three isolated Session copies passed live authentication; two are service accounts and the third remains an external trigger for Agent acceptance.
- Isolated PostgreSQL 16.11 on localhost port 55432; no existing application database modified.
- `main` stable, `dev` integration; all working branches PR into `dev`. No business code was committed before this rule was supplied.
- Remaining: complete A/B and S1–S8 recovery evidence, stable auth/recovery independent review with verified read-only runtime, combined Telegram/DeepSeek functional acceptance, remote CI and reviewed PR delivery. Actual-stack browser C3 and viewer cases passed individually.

## Git state

- Empty bootstrap: `4ce04a3`, both main/dev.
- Requirements/workflow commit: `99cfa86` on `feat/telegram-agent-platform`.
- Gateway stable snapshot: `480a89c`; no live credentials or Session files included.
- F2 interface and browser integration: `fdebbf3`.
- CI/CD scripts, integration wiring and container smoke: `18716c9`; not yet remotely exercised.
- Backend candidate: `fd0a77d`, pushed with UI and CI/CD. Remote CI run 38592943938 passed branch/commit policy, frontend, actual-stack E2E, Go/PostgreSQL/race/vet; image build and container smoke are the remaining remote stages. This candidate still has review blockers and is not accepted for promotion.
- Draft PR: https://github.com/lbtlm/KapibalaAI_gotd/pull/1, base `dev`.
- Remote main/dev protection is configured and read back: PR required, required CI Gate, admins enforced, no force push or deletion. Default branch main; repository private.

## Interface revision and CodeGraph

- User rejected the first UI visual direction. Functional baseline committed as `3403a4e`; Iris completed F2 with interface-design and design-taste-frontend. User subsequently accepted the F2 direction and requested continued acceptance. F3 is limited to the auth review repair.
- User explicitly requested CodeGraph initialization. `codegraph init .` and MCP explore succeeded; latest `codegraph status .` reports 115 files, 1,336 nodes, 2,856 edges and up to date. Local DB/WAL/SHM ignored; only .codegraph/.gitignore is tracked. Configuration committed in `353d55e`. Re-sync before stable review. All active workers notified to use CodeGraph first.
- Three isolated Telegram Session copies passed authentication and resolved the designated group. Each joined exactly once successfully. Subsequent read-only profile verification confirmed account-1 has admin/canKick permission; no role change was made by this task. G2 subsequently sent exactly one marked message; no kick/leave has occurred.
- F2 screenshots under ignored `.local/frontend-visual-v2/` use HTTP fixtures for visual inspection; they are not full-stack E2E evidence. User accepted this direction after viewing the group-detail screenshot.
- Full-stack E2E: viewer UI/403 passed; C3 exposed invite POST without {}, was fixed by Orion and verified against the actual gateway, then root's targeted C3 passed in 8.1 seconds. Fake stack is isolated on 18080/18081/18082 and kapibala_e2e; the existing application on 8080 was preserved.
- The prior worker-hosted fake stack and Vite later stopped. This invalidated only the external gateway contract case during backend race validation; the other 25 passing tests had no DATA RACE. Controller restored just the stable fake gateway on 18081, PID 65820, using kapibala_e2e and confirmed its health; Orion is repeating only that affected contract case. Final stack startup will use controller-owned processes.
- Real gateway on 28081 has only the two service aliases connected and the designated existing group imported. Completed G2 evidence in ignored `.local/acceptance/gateway-live.json`: HTTP 202, one unique own message across both service accounts, one sent receipt, query match, original event IDs on replay, no repeat beyond the exclusive cursor. One send attempt and no retry; no kick/leave/join/promotion in G2.
