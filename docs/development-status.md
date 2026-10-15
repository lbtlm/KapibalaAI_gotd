# Development status

> **R34 文档收尾**：入口重整为 [README](../README.md)、[启动指南](getting-started.md) 和 [验收指南](acceptance-guide.md)，参考用户 open-jarvis README 的导航、折叠详情与验证边界结构。加入 6 张用户授权展示的本地测试页面截图；截图只读、群保持暂停，没有发送或模型调用。图片来源和版本见 [图片索引](images/README.md)。
>
> 最新业务基线 `e36d699` 的 [light CI](https://github.com/lbtlm/KapibalaAI_gotd/actions/runs/38767836550) 已通过。本地 backend 已更新且 `/api/health` 为 ok / schema12，gateway 保持原容器运行。文档收尾只核验链接、格式、截图与命令，不重跑业务测试；当前差距仍以要求审计为准。继续沿用 issue #4 / PR #5，不合并或发布。

> **R33 当前状态**：主控独立补关键异常测试并修复无队列限流到期；完整剩余项以 [当前要求审计](requirements-audit.md) 为准。历史“无阻塞/已全部验收”只适用于当时范围，不适用于当前未验证窗口。

> **C1/C3 更新（2026-10-15 晚间）**：本次已补媒体下载/展示/保留清理，并新增只读已有群 C3（实际通过）。历史未实现/模拟 C3 记录以 [当前接口与流程覆盖表](api-flow-coverage.md) 和 R32 TDD 为准。

> **当前范围覆盖历史记录（2026-10-15，账号巡检 Revision 5）：** 用户明确要求移除账号/网关模拟模块及相关测试替身。历史 fake/C3/故障注入通过记录仅证明原版本，不再是当前覆盖。新库为空、正常上传 Session+JSON、真实连接检测与同步群；当前验证以 [账号巡检记录](account-inspection-20261015.md) 及组件 TDD 的 N19/O19/I19 结果为准。删除的测试不计通过，未重新实测的外部失败与恢复场景保持未验证。

Observed 2026-10-15 01:34 Asia/Shanghai. Coordination record; acceptance boundaries are recorded in the acceptance matrix. Final commit-level automation is authoritative in the delivery PR's latest CI Gate.

| Employee / assignment | Runtime ID | Scope | Requested runtime | Status |
| --- | --- | --- | --- | --- |
| Niko / Telegram gateway | `/root/niko_gateway_build` | `gateway/`, gateway reuse/TDD docs | GPT-6.1 Sol / Ultra | G5 stopped and committed 92cae90. Both P1 findings red→green; 26 affected normal/race passes each and four additional incoming normal checks, vet/build passed. Root G5 real startup/reconnect without sends and independent scoped recheck passed |
| Orion / application backend | `/root/orion_backend_build` | `backend/`, backend reuse/TDD docs | GPT-6.1 Sol / Ultra | B2 stopped and committed efe61af; five review findings plus sequence receipt race repaired, scoped independent recheck passed |
| Orion / combined acceptance | `/root/orion_live_acceptance_prep` | Ignored acceptance harness; cross-module recovery inspection | GPT-6.1 Sol / Ultra | L1/L2 stopped. Root preserved first failure and passed an independent second one-trigger attempt after passive history recovery; no worker live writes |
| Iris / frontend | `/root/iris_frontend_build` | `frontend/`, frontend reuse/TDD and design docs | GPT-6.1 Sol / High | F4 stopped and committed 6cce759: four actual-cookie browser cases and eleven auth/WS/login cases passed; independent recheck passed; accepted layout frozen |
| Rowan / CI/CD | `/root/rowan_ci_delivery` | Root workflows, Compose, verification/start scripts, README | GPT-6.1 Sol / High | R3 stopped and committed 1b68317: exact inactive gateway child helper allowance, 4/4 targeted skip-policy tests. First remote CI/container smoke/Gate passed; final commit result is the PR's latest CI Gate |
| Sentry / stable review | CLI `01a13b03-d998-7cb2-85b8-b76547998730` | Stable gateway/frontend at fdebbf3 | GPT-6.1 Sol / High | Completed read-only; five concrete blockers accepted for repair, no optional scope expansion |
| Sentry / backend and CI review | CLI `01a13b1f-5615-7e81-98bf-792f5b17e943` | Frozen backend/CI at fd0a77d | GPT-6.1 Sol / High | Completed; five backend blockers accepted for repair, no extra CI source blocker. Runtime read-only/never/model/high and denied canary independently verified |
| Sentry / frontend targeted recheck | CLI `01a13b38-eaab-7f70-8a43-51749d219ac1` | Frozen frontend auth/Cookie and exact CI helper rule at 1b68317 | GPT-6.1 Sol / High | Completed PASS in scope. Runtime read-only/never/model/high and denied canary independently verified |
| Sentry / backend and gateway targeted recheck | CLI `01a13b47-482b-7042-87a9-bfdc5130decc` | Frozen backend/gateway at efe61af | GPT-6.1 Sol / High | Backend PASS; two gateway TooLong blockers remain. All 101 scoped tracked blobs matched. Runtime and denied canary independently verified |
| Sentry / G4 gateway recheck | CLI `01a13b64-0746-7221-9f9e-0873f0d0cc51` | Frozen G4 gateway at 4e9069a | GPT-6.1 Sol / High | Completed REWORK: missing SDK access hash and incomplete history boundary overridden by later max-ID. Runtime read-only/never/model/high and denied canary independently verified |
| Sentry / G5 narrow recheck | CLI `01a13b75-6f4e-7e10-b3da-73c8439a9229` | Frozen G5 gateway at 92cae90 | GPT-6.1 Sol / High | Completed PASS on both G-RV3 P1 findings; all 45 scoped tracked blobs matched. Runtime read-only/never/model/high and denied canary independently verified |

Controller owns final acceptance. Worker model/effort were explicitly requested through spawn; Fast is not independently observable. Maximum three active workers. No recursive delegation.

Native `/root/sentry_stable_review` was stopped before review: its live sandbox was workspace-write despite the readonly role file. User approved a separate CLI review. Global CLI 0.98 rejected the requested model; the already-installed desktop CLI 0.162.0-alpha.17.2 preserves the requested model and readonly sandbox. No global installation/configuration changed. The failed native probe was removed; review evidence remains in ignored .local/review.

Sentry RV2 reviewed gateway/frontend only at fdebbf3: persisted real send intent recovery, checkpoint acknowledgement/lease fencing, channel-too-long history recovery, queued stale gateway effects, and late frontend 401/refresh across logout/new login. The frontend interleavings were reproduced with the real source in a write-free transport harness; gateway findings identify static paths plus missing critical validation. Backend/CI were subsequently reviewed at fd0a77d in B-RV1. Controller independently verified reviewer model/effort from runtime metadata; the reviewer's narrower self-report did not independently inspect those settings.

Controller's F3 acceptance found a browser cookie race: an A refresh arriving after B login overwrote B's HttpOnly cookie. F4 serializes cookie-changing HTTP requests, checks the generation before dispatch, and has four real-cookie browser regressions. The original synthetic reproduction remains in `.local/acceptance/frontend-cookie-race.json`; Sentry F-RV2 accepted the repair without cross-tab scope expansion.

Sentry B-RV1's five backend findings were repaired and accepted in BG-RV2: membership ordering, durable kick identity, sequence receipt/restart ordering, short-downtime budget accounting, and independent account progress. Initial failed race evidence is preserved; affected cases were rerun after repair. The gateway findings retained at that stage were repaired in G4; G-RV3's subsequent subscription and interleaved-resume findings were repaired in G5 and accepted in G-RV4. No accepted review blocker remains open.

## Decisions and remaining gates

- Go/gotd and React 18; DeepSeek through OpenAI-compatible API, no Claude. User supplied the key in ignored local configuration. Real provider tool conversation and audit passed without Telegram effects.
- Real test resource paths stay in ignored local configuration. Three isolated Session copies passed live authentication; two are service accounts and the third remains an external trigger for Agent acceptance.
- Isolated PostgreSQL 16.11 on localhost port 55432; no existing application database modified.
- `main` stable, `dev` integration; all working branches PR into `dev`. No business code was committed before this rule was supplied.
- Local acceptance and scoped independent reviews are complete. Combined real Telegram/DeepSeek, G5 no-send startup, actual-stack browser C3, viewer cases, backend/frontend/gateway scoped reviews have passed within their recorded boundaries. Final submission must pass the delivery PR's latest complete CI Gate; production deployment is outside this acceptance.

## Git state

- Empty bootstrap: `4ce04a3`, both main/dev.
- Requirements/workflow commit: `99cfa86` on `feat/telegram-agent-platform`.
- Gateway stable snapshot: `480a89c`; no live credentials or Session files included.
- F2 interface and browser integration: `fdebbf3`.
- CI/CD scripts, integration wiring and container smoke: `18716c9`; exercised successfully in remote run 38592943938.
- Backend candidate: `fd0a77d`, pushed with UI and CI/CD. Remote CI run 38592943938 passed all six jobs, including SHA-bound image build, container startup/HTTP smoke and CI Gate. Tested merge SHA 5832fd70ea8c2be53e5d69250e3b77a1329538b2 and PR head were verified to have the same tree. This candidate still has review blockers and is not accepted for promotion.
- Delivery PR: https://github.com/lbtlm/KapibalaAI_gotd/pull/1, base `dev`.
- Remote main/dev protection is configured and read back: PR required, required CI Gate, admins enforced, no force push or deletion. Default branch main; repository private.

## Interface revision and CodeGraph

- User rejected the first UI visual direction. Functional baseline committed as `3403a4e`; Iris completed F2 with interface-design and design-taste-frontend. User subsequently accepted the F2 direction and requested continued acceptance. F3 is limited to the auth review repair.
- User explicitly requested CodeGraph initialization. `codegraph init .` and MCP explore succeeded; the G5 stable check reported 135 files, 1,626 nodes, 3,909 edges and up to date. Local DB/WAL/SHM ignored; only .codegraph/.gitignore is tracked. Configuration committed in `353d55e`. All active workers use CodeGraph first.
- Three isolated Telegram Session copies passed authentication and resolved the designated group. Each joined exactly once successfully. Subsequent read-only profile verification confirmed account-1 has admin/canKick permission; no role change was made by this task. G2 subsequently sent exactly one marked message; no kick/leave has occurred.
- F2 screenshots under ignored `.local/frontend-visual-v2/` use HTTP fixtures for visual inspection; they are not full-stack E2E evidence. User accepted this direction after viewing the group-detail screenshot.
- Full-stack E2E: viewer UI/403 passed; C3 exposed invite POST without {}, was fixed by Orion and verified against the actual gateway, then root's targeted C3 passed in 8.1 seconds. Fake stack is isolated on 18080/18081/18082 and kapibala_e2e; the existing application on 8080 was preserved.
- The prior worker-hosted fake stack and Vite later stopped. This invalidated only the external gateway contract case during backend race validation; the other 25 passing tests had no DATA RACE. Controller restored just the stable fake gateway on 18081, PID 65820, using kapibala_e2e and confirmed its health; Orion repeated only that affected contract case: normal 4.32s and race 5.92s passed. Final stack startup will use controller-owned processes.
- Real gateway on 28081 has only the two service aliases connected and the designated existing group imported. Completed G2 evidence in ignored `.local/acceptance/gateway-live.json`: HTTP 202, one unique own message across both service accounts, one sent receipt, query match, original event IDs on replay, no repeat beyond the exclusive cursor. One send attempt and no retry; no kick/leave/join/promotion in G2.
- First combined attempt used real Telegram and DeepSeek services. Exactly one external trigger was confirmed sent; no incoming message or Agent run reached the backend before the 80-second deadline. Cleanup verified Agent and auto-kick disabled. The failed evidence is preserved in `.local/acceptance/combined-live-first-failed.json`; root first recovered that existing trigger with Agent disabled before starting the later independent acceptance window.
- G4 root acceptance on 2026-10-15: both service aliases and the external alias reached positive channel pts and exact-ID verified progress through the existing trigger. Unavailable history is explicitly represented; no complete historical-membership claim. Agent stayed disabled during recovery, and the original trigger was unique/non-own with no pending run. Root then used a new state file/client ID for a second independent attempt: one send POST, one incoming, one run, one audited sent reply, finished/final and a three-second no-loop guard. Window start to verified cleanup took about 12 seconds. Agent and auto-kick were disabled afterward. Evidence: `.local/acceptance/combined-live-second-attempt.json` and `gateway-g4-live-recovery.json`. The public run API lacks trigger IDs; association is an exclusive-window inference, supported by exact message/response IDs and no unrelated incoming.
- Root switched both owned gateways to the G5 binary from 92cae90 after confirming no pending effects/jobs and disabled Agent policies. All three aliases restored connections with positive pts and valid SDK hashes; all four services were healthy. Backend retained exactly three messages and one finished run, with unique old trigger, new trigger and own reply; no pending/unprocessed work or additional sends. Evidence: `.local/acceptance/gateway-g5-live-startup.json` and `gateway-g5-live-recovery.json`. This verifies G5 startup/reconnect and preserved state, not a real injected failure for either scripted recovery interleaving.

## 2026-10-15 dev merge and requirements audit

The user authorized merging to dev, checking requirements for gaps, and preparing the daytime upgrade plan (execution date corrected by the user to 2026-10-15). PR #1 was merged at 2026-10-14T18:05:59Z (2026-10-15 02:05:59 Asia/Shanghai), merge commit `7020795cec8bc02cc8af27356332b1aa3f55600c`. Accepted head `6f6cbd834f5d84c894b62ada3814e7c65682afc8` is an ancestor; both trees equal `62c613919f72e3bed9a22ff57a5f5858c0ed7b42`. Final PR CI [38608574589](https://github.com/lbtlm/KapibalaAI_gotd/actions/runs/38608574589) passed all six jobs. The separate dev push CI is [38612272191](https://github.com/lbtlm/KapibalaAI_gotd/actions/runs/38612272191); its linked current result is authoritative. Main and private visibility were unchanged.

| Employee / audit contract | Runtime ID | Requested model / effort | Scope and handoff |
| --- | --- | --- | --- |
| Orion / DEV-AUDIT-20261015-v1 | `/root/orion_backend_build` reused | Existing GPT-6.1 Sol / Ultra binding | Backend/Agent requirements and recovery paths; read-only task completed, no new tests or changes |
| Niko / TG-AUDIT-20261015-v1 | `/root/niko_gateway_build` reused | Existing GPT-6.1 Sol / Ultra binding | Gateway fake/Telegram contracts and real evidence boundaries; read-only task completed |
| Iris / UI-AUDIT-20261015-v1 | `/root/iris_requirement_audit` | GPT-6.1 Sol / High explicitly requested | Five pages, B1/B3/B4 and tests; interface-design/design-taste-frontend read; read-only task completed |

Fast was not independently observable. These author-side requirements checks were scoped read-only tasks, not a replacement for the independent hardened read-only Sentry review. No worker wrote files or operated accounts. Root confirmed the key branches for stale disconnect, promote recovery, sequence preview/progress, real join, terminal persistence and message lookup. The new findings are static code evidence pending red-test reproduction, not newly executed fault acceptance. Existing scoped reviews remain historical evidence; their closure does not close these newly identified scenarios.

The 2026-10-15 daytime work, priorities, estimates, permissions and evidence gaps are in [upgrade-plan-2026-10-15.md](upgrade-plan-2026-10-15.md). Root owns only the new plan and related README/contract/matrix status alignment on `docs/upgrade-plan-2026-10-16`, created from the merged dev. No business source change or live operation belongs to this documentation batch. Future fixes retain TDD and PR-to-dev discipline. The earlier core acceptance statements in this ledger describe their original tested scope; current release readiness must also account for the new open findings.
