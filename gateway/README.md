# Telegram gateway

Go 1.24 gateway for the section 2.1 HTTP/SSE contract, using pinned gotd v0.112.0 and PostgreSQL. New databases contain no accounts. Upload a real Telethon SQLite `.session` plus JSON API metadata through the application. Import persists credentials without connecting; an explicit connection and Self check establish actual identity.

## Startup

```powershell
Set-Location gateway
# Supply DATABASE_URL privately for the gateway PostgreSQL database.
$env:GATEWAY_MODE = 'telegram'
go run ./cmd/gateway
```

An unset mode also selects Telegram. Other modes are rejected. Health reports `mode:"telegram"`. PORT defaults to 8081 and BIND_HOST to 127.0.0.1; Docker binds 0.0.0.0 inside its private application network. There is no simulator, seeded account, admin control API or artificial delayed effect.

Startup loads only durable gateway_account_configs and database Sessions. It does not read TELEGRAM_ACCOUNTS_FILE or import source Sessions. Missing durable configuration fails startup without pruning account state. Stored non-Telegram or untagged snapshots are refused: preserve that database and use the controlled migration/reupload procedure. An old simulated upload is not evidence of persisted real credentials.

A fixed PostgreSQL connection owns the writer advisory lock. Another gateway or offline command cannot acquire it. Lease loss cancels the service and managed clients. State writes, SDK checkpoints and RPC dispatch remain fenced. Real account/group state, events, send intents, receipts, update cursors and history recovery remain durable.

## Account API

POST /accounts/register accepts `{accountId,session,apiId,apiHash,deviceModel?,systemVersion?,appVersion?}` with base64 Telethon SQLite. It validates structure and atomically stores the converted authorization and private config, returning `{accountId,status:"idle"}` without starting a client. Same-ID/authorization/config replay is idempotent only while existing durable credentials remain complete. Different identity/config, aliasing an existing authorization, or replacing incomplete credentials returns 409 ACCOUNT_IMPORT_CONFLICT. Source files are unnecessary after registration.

GET /accounts/:id/registration returns exactly `{accountId,status,source}` without RPC or credential disclosure:

| Status | Meaning |
| --- | --- |
| absent | Config and Session are both absent; source is null. |
| ready | Both are structurally valid and stored fingerprints agree; source is upload or manifest. |
| incomplete | A side is missing, corrupt or inconsistent; existing data is not overwritten. |

Database failure is 503, never absent. Ready means locally prepared to attempt a connection, not proof that Telegram accepts authorization. Only successful Self/check establishes verified identity. Registry provenance defaults to upload; manifest is reserved for explicit offline adoption.

POST /accounts/:id/connect accepts optional `{connectionId}` and returns `{platformUserId,connectionId}` only when the sole managed client is ready. Intent is saved before RPC. Same-ID retries reuse the client; ended IDs return 409 CONNECTION_ENDED and a competing active ID returns 409 CONNECTION_CONFLICT. Body-less legacy callers remain supported. Ordinary final Run exits persist account_status with `{accountId,status:"offline",connectionId}`. Stale callbacks cannot overwrite newer generations. Explicit disconnect and terminal outcomes end that ID. Pre-generation online snapshots without a shared ID become offline before Resume; existing valid IDs retain normal recovery.

POST /accounts/:id/check requires online and returns `{source:"telegram",platformUserId,phone,username,firstName,lastName,premium}` from Self over the existing fenced client. Unavailable strings are null. GET /accounts/:id/joined-groups also requires online and returns `{items:[{id,title,username,type,role}]}` only after all dialog pages succeed. Type is group/supergroup; role is creator/admin/member/unknown. Discovery does not create managed groups or enable AI. These two gateway routes never connect implicitly.

## Existing manifest accounts: one-time offline adopt

Back up first and stop the gateway so its writer lease is released. Select only approved aliases from the existing private manifest. Keep original Session copies read-only. Do not adopt a fake database or change the backend cursor/database pairing.

```powershell
Set-Location gateway
# DATABASE_URL and these variables are supplied privately by the operator.
go run ./cmd/adopt -manifest $privateManifest -accounts $approvedAliases
```

The Docker image includes /adopt. With the existing private override and read-only manifest/Session mounts:

```powershell
docker compose --project-name $composeProject -f $composeFile -f $privateOverride stop gateway
docker compose --project-name $composeProject -f $composeFile -f $privateOverride run --rm --no-deps --entrypoint /adopt gateway -manifest $containerManifest -accounts $approvedAliases
```

Preconditions: an existing Telegram-mode snapshot, an existing positive Self identity for each selected alias, a valid current database Session, matching authorization in its selected source, unique authorization across all database Sessions, and no conflicting registry entry. The command owns the normal writer lease and uses one serializable transaction. It inserts only missing config rows with manifest provenance, clearing source paths/passcodes in durable config. It never writes Session/state/event rows, connects or calls Telegram. Any rejection rolls back the whole batch. Identical repeated adoption returns zero additions. Output contains only counts and selected aliases.

Privately compare Session row counts/digests, state/event cursor and business references before/after; then query registration readiness for each alias. No event ID/backend cursor reset is needed with the same database. Normal startup subsequently needs no manifest or source Session mount. Real validity still requires separately authorized Self acceptance after restart. Never delete or adopt data using an alias pattern alone.

## Real group operations and recovery

Managed-group import is separate and explicit. POST /groups/import `{username,accountIds[]}` resolves an existing group and records actual membership/roles without joining, promoting or sending. Gateway IDs use tg- followed by channel ID; backend IDs are separate. External creators are preserved.

Updates are accepted only for registered groups. Account-specific SDK state is separate from SSE history. Missing hashes are repaired from registered metadata; absent/zero pts require bounded exact-ID history before gotd OnStart reports ready. verifiedHistoryThrough records a proven contiguous prefix, not the maximum observed message. Unfinished recovery resumes its committed range, including member-snapshot failures. No simulated convergence deadline is claimed for real outages.

Sends persist intent/random ID before RPC. Recovery uses the same account, peer, text and random ID, respecting persisted FloodWait deadlines. Repeating public clientMsgId creates another send; query the earliest landed result after unknown outcomes. Query orders by platform sentAt, then numeric message ID. Unresolved effects return 504 rather than false absence/success.

Create keys retain read-only reconciliation of unknown creation. Join verifies a concrete invite peer through an existing online member before importing it; different or unprovable peers are rejected. Keyed kick/promote retain durable receipts and reject changed group/actor/target. Completed receipts replay without repeating mutations; unknown promotion confirms current role or remains unknown without blind retry. Explicit permanent actor errors retain durable terminal/member cleanup; target/peer/network errors do not terminate the actor.

GET /events?since=N replays durable IDs greater than N; omitting since starts at the current cursor. SSE sends heartbeat comments and closes on service/lease shutdown. No duplicate/reorder/disconnect injection controls remain.

## Verification boundaries

```powershell
# Supply only an isolated test/inspection PostgreSQL URL.
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

The retained suite uses actual PostgreSQL, actual offline registration, pure parser inputs and pure state transitions. Generated Session bytes establish only structure/persistence and are never connected or reported as Telegram authorization. Simulator, scripted Adapter/Invoker/RPC tests and the crash-child harness are removed; their SDK/external-failure coverage is retired, not a current passing result. Credential-free CI cannot establish real network behavior, platform mutation recovery or real Self/groups results. Component TDD/reuse documents retain historical evidence with current limits.

Preexisting cmd/probe is an explicitly authorized maintenance tool excluded from CI/normal startup. It uses NoUpdates and requires an already stored Session; it cannot import new authorization from a source file. Its optional join still requires separate real-account/group authorization and is outside upload/check/sync acceptance.
