# Implementation contract

Revision 8, 2026-10-15 Asia/Shanghai. Owner: current controller. User approved implementation, copying reusable modules then adapting, private repository commits, OpenAI-compatible APIs with DeepSeek for real testing, TDD and CI/CD. The user has authorized completing the planned repairs and explicitly split CI by PR destination: development PRs into dev run lightweight tests without build checks; dev-to-main release PRs run complete tests and builds. Subsequent usability findings will be supplied separately. Do not change repository visibility. PR #1 is merged into dev; the subsequent requirements audit and daytime work are recorded in [the 2026-10-15 daytime work plan](upgrade-plan-2026-10-15.md).

## Account inspection override (2026-10-15)

User explicitly removed account/gateway simulation, including related test doubles. [Account inspection Revision 5](account-inspection-20261015.md) supersedes historical fake-mode requirements and test coverage below. Gateway is Telegram-only, starts empty, and uses durable uploads; old manifests require an explicit offline adoption before startup. Backend and gateway databases are separate. Keep pure logic and actual PostgreSQL tests; no simulated connect/check/group/send success. Retired tests do not count as passed. Provider product behavior and unrelated authentication tests are outside this removal.

## Sources and precedence

`docs/全栈开发工程师笔试题-候选人版.md` is the normative HTTP/SSE/WS and behavioral contract. User overrides: Go backend, Telegram gotd and real Session acceptance; OpenAI-compatible DeepSeek instead of Claude/Gemini. Keep the examination's external agent tool-block envelope by translating inside the provider service. Do not contact Anthropic. Original source documents remain unchanged.

## Ownership

2026-10-15 user override: all subsequent implementation, verification and review are performed directly by the controller. Historical worker assignments below are no longer active. Keep issue #4 and PR #5, with focused incremental commits. The clarified group/script/Agent requirements are recorded in `workspace-refinement-20261015.md`; account concurrency and timeout fixes take priority. No additional live account experiments are authorized by this development instruction.

- Niko: `gateway/` Go module, all gateway source/tests, gotd adapter, durable credential registry, gateway README/Dockerfile, `docs/reuse-gateway.md` and `docs/tdd-gateway.md`.
- Orion: `backend/` Go module including business API, PostgreSQL migrations, domain workers, agent executor and standalone `cmd/agent` OpenAI/scripted provider; backend tests/README/Dockerfile; `docs/reuse-backend.md` and `docs/tdd-backend.md`.
- Iris: `frontend/`, UI tests and browser test scaffolding, `docs/reuse-frontend.md`, `docs/tdd-frontend.md` and `docs/ui-design.md`.
- Rowan: root workflows, scripts, Compose, `.env.example`/`.dockerignore`, README and operations documentation. Existing component Dockerfiles remain with their original authors unless explicitly handed over.
- Controller: shared contract/plan/ledger/readme integration, isolated local PostgreSQL, Git commits/push and acceptance. No concurrent writer to worker-owned source paths.

Each worker is not alone in the codebase. Preserve others' changes; no recursive delegation, no commits/push from workers, no real account operations until designated test resources are supplied.

## Build and runtime contract

- Separate Go modules `github.com/lbtlm/KapibalaAI_gotd/backend` and `github.com/lbtlm/KapibalaAI_gotd/gateway`, Go 1.24 compatible. This isolates large gotd compilation and dependency ownership while HTTP provides the boundary.
- Backend `go run ./cmd/migrate` applies migrations explicitly; `go run ./cmd/server` checks schema and refuses stale databases. PORT default 8080, DATABASE_URL, GATEWAY_URL default http://127.0.0.1:8081, AGENT_URL default http://127.0.0.1:8082.
- Gateway `go run ./cmd/gateway`, PORT default 8081, GATEWAY_MODE=telegram (also the default), DATABASE_URL in a separate database. Empty databases contain no accounts. Upload Session+JSON before explicitly connecting; startup uses the durable registry. Preserve section 2.1 lifecycle, SSE history and query-by-client-id; no simulator endpoints.
- Standalone Agent service `go run ./cmd/agent`, PORT default 8082, AGENT_MODE=scripted|openai. OPENAI_API_KEY, OPENAI_MODEL, OPENAI_BASE_URL and OPENAI_API_STYLE=chat_completions|responses only in provider service. DeepSeek test defaults: https://api.deepseek.com and deepseek-flash; user will fill key in ignored local configuration. Use strict function schemas where supported, one tool call per turn, fail closed on audit; preserve provider reasoning/function-call state correctly across turns. No API keys or reasoning content in frontend, fixtures of live content, logs or git.
- React 18 + TypeScript + Vite, frontend dev port 5173, proxy /api and /ws to backend. Scripts: dev, build, typecheck, test, test:e2e. Credential-free browser tests run actual backend+empty Telegram gateway+existing provider+PostgreSQL; real account acceptance is separately authorized and recorded.
- API identifiers are strings. Times ISO8601 UTC or null. Follow endpoint response envelopes from spec (including bare GET /api/accounts array). GET /api/groups uses a bare array of group objects. Backend seeds admin/admin and viewer/viewer; no service accounts. User credentials for demo are documented as demo-only.
- Login and refresh additionally return role: `{accessToken,role}`; role=admin|viewer. Backend remains permission authority.
- Additional UI supporting APIs allowed: GET /api/sequences returns array of sequence definitions; POST /api/groups/:id/sequence-runs/preview accepts same input and returns `{steps:[{index,text,resolvedVars,varSources}]}` with identical preflight errors and no effects. GET /api/health remains unauthenticated. Other domain endpoints require auth. Do not assume group IDs match gateway IDs.

## Approved testing seams and TDD

User approved the previous acceptance plan and explicitly requested TDD. The agreed public seams are HTTP API, gateway HTTP/SSE, agent turn/audit provider HTTP, WS protocol, browser UI, and pure public variable/state-transition contracts. Exercise PostgreSQL integration with real PostgreSQL and restarts; no in-memory-only production substitute. Each slice: write meaningful failing test, run and record the failure, implement/copy-adapt just enough, rerun; record concise red/green evidence per component. No need to ask again for these already approved seams.

Cover A0-A6/B1-B4, S1-S8, provider adapter and C3; add C1 only after core reliable. Include duplicate/out-of-order events, all 504/503 paths, actual process/repository reconstruction, CAS concurrency, terminal atomicity, single running group run across two service instances, audit fail-closed, effect crash reconciliation, refresh replay revocation, WS replay and stable pagination. Tests at external boundaries may count external requests when the spec explicitly limits calls; do not mock private collaborators. All skipped live tests must remain explicitly unverified, not reported as passed.

## Shared dependencies and difficult edges

Persist intent before external effects. Non-idempotent gateway retries must be preceded by reconciliation; outbox alone is not exactly-once. Distinguish platform update state from application SSE cursor and avoid advancing past unprocessed/out-of-order events. WebSocket seq must not skip late-committing lower sequence events. Preserve sender identity even after offline/terminal. Agent and sequence reuse one send path; epoch/lease reclamation cannot let stale workers execute effects.

Do not import source database backups, Sessions, production env, .git or unrelated business modules. Copy required module code and necessary dependencies, record original commit/path and local adaptation; source repositories remain read-only. No global software/config upgrades.

## Delivery and limits

Commit/push to https://github.com/lbtlm/KapibalaAI_gotd is authorized. No visibility change, no production deployment without destination, no tag/public release. Development PRs into dev run policy, lightweight Go tests and frontend unit/type checks, without release builds, Docker builds, PostgreSQL/race integration or real-stack E2E. Pure documentation PRs may use the policy-only profile. Dev-to-main release PRs run the complete PostgreSQL/race/static, frontend build, real E2E and immutable container candidate checks, including when the release diff contains only documentation. CI Gate must verify the jobs required by the selected profile and fail on unexpected skipped or failed jobs. Do not duplicate the complete suite on ordinary branch pushes. Explicit manual full verification remains available. CD uses verified immutable candidate artifacts and explicit manual promotion/deployment inputs; neither local Docker validation nor a lightweight PR pass is full release acceptance.

Runtime settings: preserve the controller's user-selected settings; Niko and Orion requested GPT-6.1 Sol/Ultra via explicit worker binding, Iris Sol/High. Native fixed profiles cannot silently replace these. Fast independently unverified; do not change it or claim verified inheritance. Independent review is required for the stable auth/concurrency/recovery changes; reviewer runtime read-only sandbox must be verified.

The October 15 repairs run on `fix/acceptance-gaps-20261015`, based on `0df05ba8b881729f37683d6fa9ef3d8399b187da`. Component tests use a separate PostgreSQL instance at `127.0.0.1:55433` and separate backend, gateway, and E2E databases. The user-facing Docker demo at `127.0.0.1:5173` is not an E2E target; browser tests use their own frontend and service ports. Only the controller switches the demo to a verified repair snapshot.

## Local test resources

Controller started isolated PostgreSQL 16.11 at 127.0.0.1:55432, database kapibala_test, local user kapibala; DSN `postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable`. Data lives in ignored .local/postgres. Tests use isolated schemas/databases and cannot touch existing application databases.

User designated https://t.me/test_kapibala and authorized choosing three live test accounts from their supplied local test installation. Exact credential paths remain in ignored local configuration, never in committed docs. Source Sessions remain untouched; inspect auth using isolated copies. Further group mutations are limited to selected test accounts and the designated test group, with capability checks before attempting owner/admin-only actions. No unrelated members may be kicked.

Provider docs verified 2026-10-14: https://api-docs.deepseek.com/guides/thinking_mode/ and https://api-docs.deepseek.com/ . DeepSeek tool turns using thinking require reasoning_content to be replayed; it must not be exposed as product UI text. The user has supplied the key in ignored local configuration. A real tool conversation and audit passed. The second combined Telegram/DeepSeek attempt passed on 2026-10-15: one incoming message, one run and one audited sent reply; the first failed attempt remains recorded. This does not cover real member removal, group creation or injected platform-failure windows; see the acceptance matrix.

## Git workflow (user confirmed)

main is stable; dev is the integration branch. All feat/*, fix/*, hotfix/*, docs/* and other development branches must PR into dev. No direct application commits to main/dev. One empty bootstrap commit establishes the initially empty branches. Controller owns shared-checkout Git operations. See AGENTS.md and CONTRIBUTING.md.

## Existing test group import

Gateway GET /groups/:id/members additionally returns role=creator|admin|member. Admin-only POST /api/groups/import accepts {gatewayGroupId,accountIds[]} and verifies the selected online accounts against actual gateway membership/roles. Imported groups may have an external creator, represented by empty creatorAccountId; no creator privilege is fabricated. Leave-all only operates selected service members and never the external owner. Original group creation behavior remains unchanged.

## Recovery repair contract

Gateway POST /groups/:id/kick additionally accepts optional idempotencyKey, generated by backend from runId and toolUseId. A repeated key with the same target/actor identifies the same logical removal. Persisted completion must be returned without removing a member again, even if that member has subsequently rejoined. Requests without a key retain the original protocol. When the gateway itself cannot establish whether an external effect completed, retain an explicit unknown outcome and reconcile without blindly removing a new membership. Backend and gateway authors own their separate paths; no new business API or generic job framework is requested.

For U8, POST /groups/:id/promote additionally accepts an optional `idempotencyKey` identifying one durable job attempt (`jobId:promote:attempt`). Repeated keys must match the original group, actor, and target; they replay persisted success or a definitive failure rather than consuming another logical attempt. An unresolved dispatched attempt reconciles the member role: an established admin role can confirm success; otherwise it remains unknown without blindly repeating the platform promotion. A backend crash after recording an attempt but before reaching the gateway may safely deliver that same key on recovery. The gateway's own unknown-state persistence and external RPC cannot be atomic; an ambiguous window remains explicitly unresolved rather than being reported as attempts exhausted. Existing unkeyed callers retain their protocol. Implementations reuse the existing effect/receipt paths and are verified separately at HTTP, SDK, and PostgreSQL reconstruction boundaries.

Frontend F2 visual direction was explicitly accepted by the user. Remaining auth repairs preserve this layout. Same-client refresh/login/logout response ordering must prevent a late HttpOnly cookie from changing the account represented by a new session; generation guards alone are insufficient. This does not introduce an unsolicited cross-tab session-management feature.

## Account inspection batch (approved 2026-10-15)

The next approved work is specified in [the account inspection contract](account-inspection-20261015.md), tracked by issue #4 and PR #5 on `fix/account-inspection-20261015` from `dc0db77`. It extends seeded-only accounts with paired Session/JSON imports, profile checks, joined-group discovery, logs and single-group organization, while retaining A1 and the existing sole TelegramAdapter lifecycle. The shared API section there governs these additive endpoints. Niko/Orion/Iris retain their existing module ownership and execution settings; the controller owns the user-selected Astra/Ultra/Fast setting. Source references are read-only. Component tests use separate inspection databases on the existing isolated PostgreSQL port 55433; the user demo at 5173 is not a component test target. Dynamic import/lifecycle/persistence changes require independent stable-snapshot review and combined acceptance before readiness.

## R27 主控：群工作区、真实 AI 与营销暂停（2026-10-15）

用户补充明确暂停与彻底结束的差异，继续沿用 #4 / #5，由主控独立实现。群级 Agent 身份由持久 group ID 确定，绑定持久管理员；每次新外部消息仍启动一个有预算的处理 run，同群最多一个。每次处理显式加载已有 recentMessages 的有界上下文（最多20条、8KiB）及上一成功处理摘要（最多2000字符），跨服务重启恢复。不是取消单次run预算、无限保留请求，或向模型回放私有推理。

POST /api/groups/{id}/marketing/pause|resume 仅管理员可用。暂停持久保存 marketing_paused，保留群/成员/剧本配置和 Agent 启用偏好，取消本次剧本、待触发消息及尚未发出的自动消息；手动发送与群管理保留。旧在途请求继续确认，恢复在 Agent/自动消息/移除成员结果未确定时返回 MARKETING_DRAINING。恢复使原 Agent 待命；同一剧本由用户预览确认后重新从第1步运行。取消任务的迟到重试不能重新发送。彻底结束继续使用既有 leave-all / B2 drain 与依次退群流程。暂停、恢复日志复用 jobs.logs。

GET /api/agent/status 暴露运行模式、模型名、是否配置，不暴露密钥。POST /api/agent/reply-preview 为管理员提供真实模型的无工具模拟回复，复用现有生成协议与分层超时；无消息、run或发送意图入库。用户明确批准本机已启用群的自动回复切换为 AGENT_MODE=openai，使用既有私有 DeepSeek 配置；无固定文案回退。正式自动回复继续经过既有工具执行、审计和管理员身份约束。
