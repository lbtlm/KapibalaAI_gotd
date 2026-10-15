# Message gateway

Go 1.24 gateway for the section 2.1 HTTP/SSE contract. `fake` and `telegram` use the same routes and response envelopes. Production requires PostgreSQL; the memory store exists only for protocol tests. The default listener is `127.0.0.1:8081`. This service is intended for a private application network; Docker explicitly binds `0.0.0.0` inside its container.

## Run the simulator

```powershell
Set-Location gateway
$env:DATABASE_URL = 'postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
$env:GATEWAY_MODE = 'fake'
$env:GATEWAY_ADMIN_ENABLED = 'true'
go run ./cmd/gateway
```

`PORT` defaults to `8081`; `BIND_HOST` defaults to `127.0.0.1`. The entrypoint creates only `gateway_*` tables. It retains all event history, pending fake effects, earliest message results, group/account state, real send random-ID correlations and update cursors. A fixed PostgreSQL connection owns the writer lease and performs state writes; another gateway process is refused. Lost lease ownership cancels the service and platform clients. The stored mode rejects switching the same database between fake and Telegram.

The simulator seeds `account-1`, `account-2`, `account-3` with stable platform IDs `10001`, `10002`, `10003`. Connect sets an account online. Creating a group immediately includes its creator and produces no creator `member_joined`. Join applies membership after 100 ms, then emits its event 100 ms later. Successful kick/leave remove membership before returning and emit events later. Sending schedules a durable effect after 50 ms. Reusing `clientMsgId` produces another message; the query returns the first landed message.

The by-client-ID query compares platform `sentAt`, then numeric Telegram channel message ID when platform seconds are equal. Delayed recovery/persistence does not change which result is earliest. Equal-time simulator messages retain their actual landing order; `clientMsgId` still never deduplicates sends.

## Fake controls

Controls are registered only when mode is `fake` and `GATEWAY_ADMIN_ENABLED=true`. They do not exist in Telegram mode.

| Endpoint | Body |
|---|---|
| `POST /admin/faults` | `{operation, code, accountId?, groupId?, count?, retryAfterSeconds?, settleAfterMs?, effect?}` |
| `POST /admin/events` | `{type, data, delayMs?}` where data is the specified event payload |
| `POST /admin/delivery` | `{duplicate, reorder, disconnectAfter}` |
| `POST /admin/group` | `{groupId, writable?, inviteExpired?}` |
| `POST /admin/reset` | clears fake state and events; use only in isolated tests |

`count` defaults to one; `-1` remains active. Operations are `any`, `connect`, `disconnect`, `create`, `invite`, `join`, `promote`, `kick`, `leave`, `send`, `members`, `query`, `events`.

Supported failure controls:

- `GATEWAY_UNAVAILABLE` / `SERVICE_UNAVAILABLE`: 503, including SSE and message queries.
- `NETWORK_TIMEOUT`: send/kick return 504. `effect=true` completes after `settleAfterMs` (default 100 ms, capped at 1900 ms); `false` has no effect. Create with `effect=true` creates then returns 504; the optional internal `idempotencyKey` recovers the same group.
- `RATE_LIMITED`: send returns 429 and `retryAfterSeconds` (default one). Every attempt during the cooldown restarts the same duration.
- `ACCOUNT_SUSPENDED`, `SESSION_EXPIRED`: permanent account states, all group memberships removed, status/member events emitted.
- `GROUP_WRITE_FORBIDDEN`, `SENDER_NOT_IN_GROUP`, `NO_PERMISSION`, `NOT_MEMBER_YET`, `OWNER_LEFT`, `ALREADY_MEMBER`: corresponding protocol errors.
- `LEAVE_FAILED`: leave returns 500 without removing membership.
- `INVITE_DELAY`: next invite advertises `settleAfterMs`; join is rejected until ready. `INVITE_EXPIRED` or `/admin/group` expires an existing link.
- `JOIN_MISSING`: accepts a join without adding the member or producing an event.
- `ASYNC_FAILED`: accepts a send, then emits `message_failed`; `effect=false` means `GROUP_WRITE_FORBIDDEN`, `effect=true` means `ACCOUNT_SUSPENDED` and terminal account state.

Delivery duplicates original event IDs; reorder swaps adjacent frames in one delivery batch. `disconnectAfter` counts emitted frames. `since` is exclusive. Omitting it starts at the current cursor; specifying zero replays all history. The consumer is responsible for deduplication and out-of-order handling.

External-message example:

```json
{"type":"message","data":{"groupId":"group-1","msgId":"external-1","senderPlatformUserId":"90001","text":"Please reply","sentAt":"2026-10-14T15:00:00.000Z"}}
```

Member injection changes the member list immediately, with an optional delayed event. Account-status injection accepts only `suspended` / `session_expired`. Old message supplement events may preserve their original `msgId` / `sentAt` while receiving a new event ID.

## Telegram mode

Use a separate database from the simulator and an ignored private manifest. No original Session is written. The converted Session and renewed authorization data live in `gateway_sessions`.

```json
[
  {
    "id":"account-1",
    "apiId":1,
    "apiHash":"<private value>",
    "sessionPath":"<absolute path to an isolated session copy>",
    "sessionFormat":"TelethonSQLite",
    "deviceModel":"Desktop",
    "systemVersion":"Windows 10",
    "appVersion":"1.0.0"
  }
]
```

Formats: `gotdJSON`, `TelethonSQLite`, `tdataZIP`. ZIP import is bounded and rejects unsafe paths, symlinks and multiple account identities; optional `passcode` is manifest-only. Telethon SQLite opens in read-only mode. API credentials are explicit; no default official-app credentials are copied from the source.

```powershell
Set-Location gateway
$env:DATABASE_URL = 'postgres://kapibala@127.0.0.1:55432/kapibala_live?sslmode=disable'
$env:GATEWAY_MODE = 'telegram'
$env:TELEGRAM_ACCOUNTS_FILE = '../.local/telegram/service-accounts.json'
go run ./cmd/gateway
```

After connecting the configured service aliases, `POST /groups/import {"username":"test_kapibala","accountIds":["account-1","account-2"]}` resolves only the selected existing group and records actual membership/roles. It performs no join, promotion or send. Gateway group IDs are `tg-<channel ID>`; backend group IDs remain separate. Members include the additional `role` field (`creator`, `admin`, `member`). An external group creator is never replaced with a service account.

Telegram operations reuse gotd v0.112.0. Updates are converted only for registered groups, including outgoing messages; duplicates across service clients produce one `message` event. User/channel SDK update state is distinct from the SSE cursor and persists in dedicated tables. Previously online configured accounts reconnect after reconstruction.

A channel access-hash row with `pts=0` is metadata, not a valid checkpoint. Before starting gotd for an already registered group whose cursor is absent/zero, the pipeline reconciles bounded exact-ID history and only then stores the dialog's valid pts. Connect readiness is reported through gotd's `OnStart`, after this initialization succeeds. For a group newly registered while the manager is running, hash-only rows do not override gotd's first-update cursor initialization. [Telegram's getChannelDifference reference](https://core.telegram.org/method/updates.getChannelDifference) lists empty/invalid persistent timestamps as errors.

The startup pipeline also transfers the registered group's known snapshot access hash into SDK storage when that SDK hash is absent/zero, including rows that already have positive pts. Existing valid SDK hashes are retained. This ensures a completely absent SDK row becomes a subscribable channel after history initialization, rather than only a positive-pts row that gotd excludes from its channel workers.

`historyThrough` is the largest message ID delivered through the SDK or processed by a history batch; it alone does not prove every earlier ID was checked. The optional per-account `verifiedHistoryThrough` records only a contiguous exact-ID checked prefix from ID1. Missing/zero-cursor startup uses that verified prefix rather than the largest seen ID; legacy data without prefix evidence starts at ID1 within the existing bounds. A saved recovery record that proves an ID1-based committed prefix can retain that progress. Events already seen are deduplicated during rechecking. Pending/blocked recovery retains its counts and unavailable IDs across metadata failure, reconstruction and a reduced top-message target.

With an established SDK cursor, a same-account unfinished recovery resumes from its committed `throughId` before considering a later ordinary maximum-seen ID. This also applies when all message batches committed but the member snapshot failed. An ordinary inbound message159 cannot make an incomplete range through100 skip IDs101–158 or clear its recorded limitations.

Each send stores an intent and cryptographic random ID before the RPC. Returned/pushed random-ID mappings recover `message_sent`, even when the public message arrived first. After reconstruction, an online account reconciles pending sends by repeating the same account/peer/text/random ID, without allocating a new logical send. Telegram documents that a completed random ID returns its original message across MTProto sessions; an in-flight `RANDOM_ID_DUPLICATE` remains unknown for a later attempt. Background reconciliation respects a persisted FloodWait deadline. Public callers must query after a timeout: a repeated public send, even with the same `clientMsgId`, is still a new send. If the platform cannot yet establish the result, the query remains 504. These conditions follow [Telegram's update and random-ID contract](https://core.telegram.org/api/updates) and [messages.sendMessage](https://core.telegram.org/method/messages.sendMessage); they do not guarantee exactly-once behavior for arbitrary external effects. The simulator guarantees the specified two-second convergence; a real platform outage cannot provide that guarantee. Internal create keys persist intent and use a deterministic group title for read-only reconciliation; an unreconciled create returns 504 and is not blindly repeated.

Telegram joins first call `messages.checkChatInvite` through an existing online member of the requested group. The pinned schema exposes no peer ID in an ordinary nonmember `ChatInvite`; titles cannot prove identity. A different concrete peer returns `400 INVITE_GROUP_MISMATCH`; no online member or no provable peer returns `503 GATEWAY_UNAVAILABLE`, without importing the invite. After import, the response must contain the requested channel and a real member snapshot supplies membership/joined events. An unrelated channel never creates a membership in the requested group. Existing members still return `409 ALREADY_MEMBER`; expired platform hashes retain `410 INVITE_EXPIRED`. Simulator wrong-link behavior remains its existing 410.

An explicit `SESSION_EXPIRED` or `ACCOUNT_SUSPENDED` from initial connect or a platform operation enters the existing durable terminal state and removes known memberships. Subsequent account operations retain the same 401/403 after reconstruction and do not call Telegram again. A verifier account's terminal error is applied to that verifier, rather than the account attempting to join. Network and peer/permission failures do not become account terminal states.

Connect/disconnect recheck request cancellation after obtaining the adapter lifecycle lock. Account HTTP work also rechecks cancellation after queueing and after the adapter returns, so a canceled old success cannot overwrite a later account state. Shared lease probes preserve request metadata, use a two-second bound and follow service shutdown; client cancellation still cancels the actual operation without canceling the writer-lease query. Real lease loss fences the service. This handles observed request cancellation; it does not add a durable lifecycle-generation protocol for arbitrary uncanceled requests arriving later.

`POST /groups/:id/promote` accepts optional `idempotencyKey` alongside `byAccountId` and target `accountId`. The backend uses one key per logical attempt (`jobId:promote:1` / `jobId:promote:2`). A saved prepared operation can execute the same attempt after reconstruction. Completed results and definitive 4xx failures, excluding rate limiting, replay durably; a new key denotes another attempt. Changed group, actor or target under the same key returns `409 IDEMPOTENCY_CONFLICT`. Before the Telegram mutation, the existing effect is saved as unknown. Unknown replay reads current platform roles: target admin confirms completion; otherwise, including an unreadable snapshot, it stays `504 NETWORK_TIMEOUT` and never repeats `channels.editAdmin`. The interval between saving unknown and dispatching the RPC cannot be resolved atomically; a crash there can retain 504 despite no platform mutation. Fake mode persists keyed preparation/results using the same effects; calls without a key retain their original behavior. Permanent account errors still take precedence over receipt replay.

`POST /groups/:id/kick` accepts an optional `idempotencyKey` with the existing `byAccountId` and `targetPlatformUserId` fields. Completed results persist and replay as `200 {"kicked":true}` before current actor online/role/member checks, including when the target has rejoined, while permanent account errors retain precedence. A key reused with different group, actor or target returns 409 `IDEMPOTENCY_CONFLICT`. Intent is persisted before the platform call; an unresolved effect returns 504 `NETWORK_TIMEOUT` on replay and does not remove the member again. Telegram exposes no kick operation ID, so a gateway crash between platform acceptance and result persistence cannot be determined from current membership after rejoin. Calls without a key retain the original protocol.

SDK difference replies are ingested durably before gotd receives them; handler failure cancels the account pipeline and prevents subsequent checkpoint writes. Every update-state and channel-access-hash write uses the same leased PostgreSQL connection as snapshots. Queued HTTP work checks ownership again after acquiring the service lock, and every production Telegram RPC also checks ownership and is cancelled with service shutdown.

Too-long recovery reads only registered groups. It obtains the selected peer's top message ID, then uses exact `channels.getMessages` IDs in batches of 100; events and the verified per-account ID boundary persist together after each complete batch. A restart continues at the last persisted boundary. Recovery is cancelled after 30 seconds and refuses spans exceeding 10,000 IDs. Missing response slots, inaccessible history or excessive spans remain visibly blocked, with SDK checkpoint writes refused. `GET /groups/:id/recovery` exposes the latest range (`fromId`, `throughId`, `targetId`), status, recovered-message count and unavailable IDs. A successful pass reports `recovered_with_limits`: `messageEmpty` proves unavailable content, not its former text, and a current member snapshot cannot reconstruct every transient join/leave during downtime. Historical membership completeness remains false. Accounts with a blocked pipeline require explicit reconnect after the underlying issue is resolved. [Telegram's gap-recovery guidance](https://core.telegram.org/api/updates) explicitly requires exact message IDs rather than relying on `messages.getHistory` for very old channel gaps.

`GET /media/:id` returns 404: optional media materialization is outside the delivered core slice. Telegram event timestamps retain platform seconds as ISO8601 UTC; fake timestamps have millisecond precision. Real Telegram does not expose sub-second original server timestamps.

## Verification and authorized probe

```powershell
Set-Location gateway
$env:GATEWAY_TEST_DATABASE_URL = 'postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/gateway ./cmd/probe
```

The PostgreSQL tests skip with an explicit reason if the test DSN is absent; CI must provide it. They exercise reconstruction and disconnection of the writer-lock session against a real database. Use an isolated database: the lease failure test terminates only the session holding this gateway's advisory lock. The OS-hardkill test creates/reuses the separate `kapibala_gateway_crash` database and kills only its own child test process. `TestGatewayCrashChild` is a subprocess entrypoint and skips in the normal suite unless `GATEWAY_CRASH_CHILD=1`; its parent recovery case must execute.

`cmd/probe` uses `NoUpdates` and only calls Self, ResolveUsername and GetParticipant(self). `-account=<alias>` selects one account; `-out=<ignored local path>` avoids printing private display names. The explicit `-join` option makes one join attempt per configured alias and requires an authorized test group; it stops each failed account without retry. Its live invocation is never part of CI. See `docs/tdd-gateway.md` and `docs/reuse-gateway.md` for evidence and source lineage.
