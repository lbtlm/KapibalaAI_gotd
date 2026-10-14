# Implementation contract

Revision 4, 2026-10-15 Asia/Shanghai. Owner: current controller. User approved implementation, copying reusable modules then adapting, private repository commits, OpenAI-compatible APIs with DeepSeek for real testing, TDD, complete testing and CI/CD. Target: deliver as soon as possible, within 24 hours where feasible. Do not change repository visibility.

## Sources and precedence

`docs/全栈开发工程师笔试题-候选人版.md` is the normative HTTP/SSE/WS and behavioral contract. User overrides: Go backend, Telegram gotd and real Session acceptance; OpenAI-compatible DeepSeek instead of Claude/Gemini. Keep the examination's external agent tool-block envelope by translating inside the provider service. Do not contact Anthropic. Original source documents remain unchanged.

## Ownership

- Niko: `gateway/` Go module, all gateway source/tests, gotd adapter, deterministic simulator, gateway README/Dockerfile, `docs/reuse-gateway.md` and `docs/tdd-gateway.md`.
- Orion: `backend/` Go module including business API, PostgreSQL migrations, domain workers, agent executor and standalone `cmd/agent` OpenAI/scripted provider; backend tests/README/Dockerfile; `docs/reuse-backend.md` and `docs/tdd-backend.md`.
- Iris: `frontend/`, UI tests and browser test scaffolding, `docs/reuse-frontend.md`, `docs/tdd-frontend.md` and `docs/ui-design.md`.
- Rowan: root workflows, scripts, Compose, `.env.example`/`.dockerignore`, README and operations documentation. Existing component Dockerfiles remain with their original authors unless explicitly handed over.
- Controller: shared contract/plan/ledger/readme integration, isolated local PostgreSQL, Git commits/push and acceptance. No concurrent writer to worker-owned source paths.

Each worker is not alone in the codebase. Preserve others' changes; no recursive delegation, no commits/push from workers, no real account operations until designated test resources are supplied.

## Build and runtime contract

- Separate Go modules `github.com/lbtlm/KapibalaAI_gotd/backend` and `github.com/lbtlm/KapibalaAI_gotd/gateway`, Go 1.24 compatible. This isolates large gotd compilation and dependency ownership while HTTP provides the boundary.
- Backend `go run ./cmd/migrate` applies migrations explicitly; `go run ./cmd/server` checks schema and refuses stale databases. PORT default 8080, DATABASE_URL, GATEWAY_URL default http://127.0.0.1:8081, AGENT_URL default http://127.0.0.1:8082.
- Gateway `go run ./cmd/gateway`, PORT default 8081, GATEWAY_MODE=fake|telegram, DATABASE_URL. Persist gateway event log independently from application tables (prefix gateway_ or own schema). Fake mode requires no Telegram credentials. Accounts seed IDs account-1/account-2/account-3, stable platform IDs in fake. Gateway implements exact section 2.1 including SSE history and query-by-client-id; simulator control endpoints must be explicitly enabled and unavailable by default in real mode.
- Standalone Agent service `go run ./cmd/agent`, PORT default 8082, AGENT_MODE=scripted|openai. OPENAI_API_KEY, OPENAI_MODEL, OPENAI_BASE_URL and OPENAI_API_STYLE=chat_completions|responses only in provider service. DeepSeek test defaults: https://api.deepseek.com and deepseek-flash; user will fill key in ignored local configuration. Use strict function schemas where supported, one tool call per turn, fail closed on audit; preserve provider reasoning/function-call state correctly across turns. No API keys or reasoning content in frontend, fixtures of live content, logs or git.
- React 18 + TypeScript + Vite, frontend dev port 5173, proxy /api and /ws to backend. Scripts: dev, build, typecheck, test, test:e2e. Browser tests eventually run actual backend+fake gateway+scripted agent+PostgreSQL, not only mocked page APIs.
- API identifiers are strings. Times ISO8601 UTC or null. Follow endpoint response envelopes from spec (including bare GET /api/accounts array). GET /api/groups uses a bare array of group objects. Backend seed admin/admin, viewer/viewer and 3 idle service accounts. User credentials for demo are documented as demo-only.
- Login and refresh additionally return role: `{accessToken,role}`; role=admin|viewer. Backend remains permission authority.
- Additional UI supporting APIs allowed: GET /api/sequences returns array of sequence definitions; POST /api/groups/:id/sequence-runs/preview accepts same input and returns `{steps:[{index,text,resolvedVars,varSources}]}` with identical preflight errors and no effects. GET /api/health remains unauthenticated. Other domain endpoints require auth. Do not assume group IDs match gateway IDs.

## Approved testing seams and TDD

User approved the previous acceptance plan and explicitly requested TDD. The agreed public seams are HTTP API, gateway HTTP/SSE, agent turn/audit provider HTTP, WS protocol, browser UI, and pure public variable/state-transition contracts. Exercise PostgreSQL integration with real PostgreSQL and restarts; no in-memory-only production substitute. Each slice: write meaningful failing test, run and record the failure, implement/copy-adapt just enough, rerun; record concise red/green evidence per component. No need to ask again for these already approved seams.

Cover A0-A6/B1-B4, S1-S8, provider adapter and C3; add C1 only after core reliable. Include duplicate/out-of-order events, all 504/503 paths, actual process/repository reconstruction, CAS concurrency, terminal atomicity, single running group run across two service instances, audit fail-closed, effect crash reconciliation, refresh replay revocation, WS replay and stable pagination. Tests at external boundaries may count external requests when the spec explicitly limits calls; do not mock private collaborators. All skipped live tests must remain explicitly unverified, not reported as passed.

## Shared dependencies and difficult edges

Persist intent before external effects. Non-idempotent gateway retries must be preceded by reconciliation; outbox alone is not exactly-once. Distinguish platform update state from application SSE cursor and avoid advancing past unprocessed/out-of-order events. WebSocket seq must not skip late-committing lower sequence events. Preserve sender identity even after offline/terminal. Agent and sequence reuse one send path; epoch/lease reclamation cannot let stale workers execute effects.

Do not import source database backups, Sessions, production env, .git or unrelated business modules. Copy required module code and necessary dependencies, record original commit/path and local adaptation; source repositories remain read-only. No global software/config upgrades.

## Delivery and limits

Commit/push to https://github.com/lbtlm/KapibalaAI_gotd is authorized. No visibility change, no production deployment without destination, no tag/public release. CI must execute tests, race/static checks where supported, frontend build and real E2E. CD should build immutable deliverable/container artifacts with checksums and explicit manual promotion/deployment inputs, with no simulated deployment claimed as live.

Runtime settings: controller user-selected Astra/Ultra/Fast; Niko and Orion requested GPT-6.1 Sol/Ultra via explicit worker binding, Iris Sol/High. Native fixed profiles cannot silently replace these. Fast independently unverified; do not change it or claim verified inheritance. Independent review is required for the stable auth/concurrency/recovery changes; reviewer runtime read-only sandbox must be verified.

## Local test resources

Controller started isolated PostgreSQL 16.11 at 127.0.0.1:55432, database kapibala_test, local user kapibala; DSN `postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable`. Data lives in ignored .local/postgres. Tests use isolated schemas/databases and cannot touch existing application databases.

User designated https://t.me/test_kapibala and authorized choosing three live test accounts from their supplied local test installation. Exact credential paths remain in ignored local configuration, never in committed docs. Source Sessions remain untouched; inspect auth using isolated copies. Further group mutations are limited to selected test accounts and the designated test group, with capability checks before attempting owner/admin-only actions. No unrelated members may be kicked.

Provider docs verified 2026-10-14: https://api-docs.deepseek.com/guides/thinking_mode/ and https://api-docs.deepseek.com/ . DeepSeek tool turns using thinking require reasoning_content to be replayed; it must not be exposed as product UI text. The user has supplied the key in ignored local configuration. A real tool conversation and audit passed; combined Telegram/LLM acceptance remains pending.

## Git workflow (user confirmed)

main is stable; dev is the integration branch. All feat/*, fix/*, hotfix/*, docs/* and other development branches must PR into dev. No direct application commits to main/dev. One empty bootstrap commit establishes the initially empty branches. Controller owns shared-checkout Git operations. See AGENTS.md and CONTRIBUTING.md.

## Existing test group import

Gateway GET /groups/:id/members additionally returns role=creator|admin|member. Admin-only POST /api/groups/import accepts {gatewayGroupId,accountIds[]} and verifies the selected online accounts against actual gateway membership/roles. Imported groups may have an external creator, represented by empty creatorAccountId; no creator privilege is fabricated. Leave-all only operates selected service members and never the external owner. Original group creation behavior remains unchanged.

## Recovery repair contract

Gateway POST /groups/:id/kick additionally accepts optional idempotencyKey, generated by backend from runId and toolUseId. A repeated key with the same target/actor identifies the same logical removal. Persisted completion must be returned without removing a member again, even if that member has subsequently rejoined. Requests without a key retain the original protocol. When the gateway itself cannot establish whether an external effect completed, retain an explicit unknown outcome and reconcile without blindly removing a new membership. Backend and gateway authors own their separate paths; no new business API or generic job framework is requested.

Frontend F2 visual direction was explicitly accepted by the user. Remaining auth repairs preserve this layout. Same-client refresh/login/logout response ordering must prevent a late HttpOnly cookie from changing the account represented by a new session; generation guards alone are insufficient. This does not introduce an unsolicited cross-tab session-management feature.
