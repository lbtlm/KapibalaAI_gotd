# Account management inspection — 2026-10-15

Status: approved implementation, revision 4. Tracking: GitHub issue #4, PR #5 into dev, multiple focused commits. Branch: fix/account-inspection-20261015; baseline: dc0db77. Controller owns integration, Git and acceptance.

## Approved behavior

- Make the account workspace fill the available width and height using existing Ant Design styles, meaningful columns, search, group/status filters and pagination. Full phone numbers remain visible/copyable.
- Import matching Telethon SQLite `.session` + metadata `.json` file pairs, supporting multiple pairs with explicit per-account results. Reject ambiguous pairing, unsupported formats and conflicting credentials; never silently overwrite a Session.
- Persist imported accounts and their gateway registration so they survive restart without requiring source files. Import begins idle and does not connect implicitly.
- Reuse the existing TelegramAdapter lifecycle; do not introduce a second online manager/client pool. Online checks reuse the managed client; an offline check explicitly connects through the existing A1 state machine. Terminal accounts cannot be revived.
- Show account details in a modal: basic profile, joined groups, operation logs. Opening details reads persisted data only.
- Sync joined groups only for an online account. Fetch all pages before replacing the last successful snapshot; show creator/admin/member/unknown correctly. Discovery must not automatically import managed groups or enable AI.
- Single nullable account group, group CRUD, bulk move, filtering; deleting a group returns accounts to ungrouped without deleting accounts.
- Persist account management operation logs with actor, time, action, outcome and safe error summary. Never expose Session, API hash, private config or live message content.

## Lifecycle baseline

Preserve the examination A1 states/transitions, CAS, atomic terminal consequences and persisted frontend events. Ordinary gotd Run termination currently sets only gateway offline. Add a durable, stale-safe path that makes the corresponding backend account disconnected (only from an eligible live state), without reviving manual offline/terminal accounts or allowing an old connection result to overwrite a newer one. Preserve the existing client shutdown, request cancellation and writer-fencing boundaries. No unsolicited automatic connection on detail/group-sync reads.

Upgrade legacy online/rate-limited accounts without an incarnation to disconnected with an atomic pending disconnect intent. Gateway startup persists those legacy connections offline instead of automatically resuming them. The next explicit user connection obtains a fresh ID. Preserve groups, business records and terminal accounts; connections with an existing ID retain their normal recovery semantics. Check and group-sync RATE_LIMITED failures apply the existing A2 account deadline/backoff and retain retry metadata, failed operation logs and the previous successful group snapshot.

## Ownership and dispatch

- Niko: gateway/**, docs/reuse-gateway.md, docs/tdd-gateway.md. Complex; requested GPT-6.1 Sol / Ultra, explicit worker binding.
- Orion: backend/**, docs/reuse-backend.md, docs/tdd-backend.md. Complex; requested GPT-6.1 Sol / Ultra, explicit worker binding.
- Iris: frontend/**, docs/reuse-frontend.md, docs/tdd-frontend.md, docs/ui-design.md. Standard/Complex UI integration; requested GPT-6.1 Sol / High.
- Controller: this shared contract, integration, isolated test services, commits, PR and final acceptance. Sentry independently reviews stable lifecycle/auth/persistence changes before acceptance.

Workers do not switch branches, commit, push, recurse or edit another owner's paths. Preserve other work. Current user-selected controller settings remain unchanged; worker Fast is not independently verified.

## Verification

Use public-boundary TDD with concise red/green records in component documents. Check ordinary disconnect and stale events; terminal/manual-offline invariants; import validation, duplicates and restart persistence; connected/explicit-connect checks; complete/failed group sync and roles; grouping transactions; logs and viewer permissions. Verify desktop layout and modal states in a browser, then one combined isolated real-stack fake-gateway E2E. Run affected PostgreSQL/race checks for lifecycle/persistence. Reuse still-valid evidence; no repetitive broad suites. Real Telegram evidence, remote CI and local tests are reported separately. No deployment, release or unrelated live mutations.

## Shared API

Revision 4: the following names are frozen for this batch. Preserve existing account/group list envelopes and A1 endpoints. All identifiers are strings; timestamps ISO8601 UTC or null. User-facing write operations require admin. The verification source addition below prevents simulated checks from claiming real Telegram verification. Duplicate authorization detection includes both dynamically imported and manifest-backed accounts, using persisted authorization first when available; no live connection is required to check identity.

### Gateway (internal, existing trusted network boundary)

- `POST /accounts/register`: `{accountId,session,apiId,apiHash,deviceModel?,systemVersion?,appVersion?}`. `session` is base64 Telethon SQLite bytes; backend parses metadata aliases, gateway independently validates credentials/session. Response `{accountId,status:"idle"}`. Same account ID and identical authorization/configuration fingerprint replays without replacing data; conflicting data is `409 ACCOUNT_IMPORT_CONFLICT`. Persist config and converted Session atomically. Do not log/echo credentials. Detect duplicate authorization registered under another account ID.
- `POST /accounts/:id/check`: online-only, fenced Self query; returns `{platformUserId,phone,username,firstName,lastName,premium,source}`. `source=telegram|simulated`; nullable unavailable strings; no implicit connection. Fake mode explicitly returns simulated evidence and cannot verify a real phone.
- `GET /accounts/:id/joined-groups`: online-only, all pages or error; returns `{items:[{id,title,username,type,role}]}`. `type=group|supergroup`; `role=creator|admin|member|unknown`; username nullable. No broadcasting channels or automatic managed-group registration.
- Existing connect accepts optional `{connectionId}` and echoes it alongside platformUserId; old body-less requests remain compatible. An ordinary final Run exit persists `account_status {accountId,status:"offline",connectionId}`. Only matching current incarnation may become disconnected in backend, and only from online/rate_limited. Clear/reject stale incarnations after explicit offline or terminal transitions. Persist connection intent before external connection; interrupted/ambiguous results must be safely reconcilable. Existing terminal events remain compatible.

### Business API (frontend contract)

- `GET /api/accounts` stays a bare Account array; `GET /api/accounts/:id` returns one Account. Existing fields remain. Add nullable `phone,username,firstName,lastName,groupId,lastCheckedAt`, boolean `phoneVerified`, nullable boolean `premium`, `createdAt`, nullable `lastCheckResult` (`success|failed`), `lastCheckError` (safe code/summary) and `verificationSource` (`telegram|simulated`). Missing optional values render as not acquired; a file name is not verified phone identity. Only a real Telegram result with a nonempty phone can set phoneVerified; simulated values must not overwrite an imported phone or be displayed as real verification. UI labels simulated checks explicitly.
- `POST /api/accounts/import`: multipart repeated `files` fields and optional `groupId`; returns `{results:[{basename,accountId:null|string,success:boolean,error:null|{code,message}}]}`. Require same-basename pairs, reject duplicate basenames and ambiguity. Backend persists a credential-free import intent/stable ID before registration, then completes account creation; retries can recover pending registration. Already completed imports report an explicit duplicate result without overwriting.
- `POST /api/accounts/:id/check`: `{connectIfNeeded:boolean}` -> Account. Only explicit true may connect idle/disconnected using A1; terminal remains terminal. Persist check result/profile and a safe operation log. Network errors cannot be misclassified as terminal errors.
- `GET /api/accounts/:id/joined-groups`, `POST /api/accounts/:id/joined-groups/sync` -> `{items:[gateway group DTO],lastSyncedAt:null|string}`. GET only reads cached data. Sync requires online; replace the snapshot only after complete success, retain old data on failure.
- `GET /api/accounts/:id/logs?cursor=...&action=...&result=...` -> `{items:[{id,actor,action,result,errorCode,createdAt}],nextCursor:null|string}`. Result is `success|failed`; actor is a safe display identity, action is stable machine name. Initial page size 20.
- `GET /api/account-groups` -> bare array `{id,name,accountCount,createdAt}`; POST `{name}` -> group; PATCH `/api/account-groups/:id` `{name}` -> group; DELETE same path -> 204, atomically unassign accounts.
- `POST /api/account-groups/move` `{accountIds:string[],groupId:null|string}` -> `{updated:number}`; validate whole selection and transact atomically.

Group CRUD/account moves and import/check/sync/connect/disconnect attempts produce account logs where an account exists. Display labels may be localized by frontend. No new settings panel or general-purpose job framework.
