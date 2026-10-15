# Backend and Agent provider

Go 1.24 and PostgreSQL 16+. The backend checks the schema and refuses stale/dirty databases. Migrations run explicitly and are repeatable.

```powershell
cd D:\web_projects\kapibala_ai\backend
$env:DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/server
```

Defaults: `BIND_HOST=127.0.0.1`, `PORT=8080`, `GATEWAY_URL=http://127.0.0.1:8081`, `AGENT_URL=http://127.0.0.1:8082`. Docker explicitly binds `0.0.0.0`. Demo-only users are `admin/admin` and `viewer/viewer`. All domain writes require admin. Access tokens expire after 15 minutes; refresh tokens are HttpOnly cookies and rotate atomically. Replay revokes the entire session immediately.

New databases contain no service accounts. Upload matching real Telethon SQLite Session/JSON pairs to register accounts without connecting; connecting, checking Self and syncing joined groups use the sole real Telegram gateway. Successful checks have `verificationSource:"telegram"`; null means not yet verified. The backend never treats a simulated response as a successful check.

Reuploading a completed legacy import checks the gateway's durable registration evidence without RPC. Ready remains an explicit duplicate. Absent configuration and Session reopen the existing import intent before re-registration and preserve the account's profile, logs and joined-group snapshot. Incomplete credentials or unavailable evidence fail closed without replacing authorization. Normal startup neither deletes historical accounts nor resets the paired gateway cursor; a controller must inventory and back up old data before a one-time cleanup.

Migration 7 retires proof explicitly recorded as simulated: its live accounts become disconnected, and its check/cache success indicators are cleared. Existing profile fields, logs and cached group items remain stored for recovery, while the API hides the unverified cached items until a real successful sync. Telegram-verified profiles, states and snapshots are preserved. Downgrade keeps cache times nullable so retained unverified data can survive the migration cycle. Retired proof cannot be recreated by rolling the migration back; restore the pre-upgrade backup when that historical evidence is needed.

Run `cmd/agent` in a separate terminal with the same `DATABASE_URL`, `PORT=8082` and `AGENT_MODE=scripted` or `openai`. Only this provider reads `OPENAI_API_KEY`. DeepSeek defaults are `OPENAI_API_STYLE=chat_completions`, `OPENAI_BASE_URL=https://api.deepseek.com`, `OPENAI_MODEL=deepseek-flash`. To use OpenAI Responses set `OPENAI_API_STYLE=responses`, its `/v1` base URL and an appropriate model. Persisted assistant reasoning/tool items are replayed across provider restarts. No Anthropic connection is used.

For deterministic faults enable `AGENT_ADMIN_ENABLED=true` in scripted mode and `POST /admin/script` with `{turns:["<raw response>"],audits:["<raw audit verdict>"],delayMs:0}`. Empty arrays restore the default read → send → finish script. Invalid JSON strings intentionally exercise backend protocol errors. The admin endpoint is unavailable in openai mode.

Existing group: connect the selected service accounts, use the gateway's `POST /groups/import` to resolve the designated existing group, then backend `POST /api/groups/import {gatewayGroupId,accountIds}`. All members and their roles come from gateway membership; only selected service accounts receive an accountId, while external members retain null. An imported group with an external owner exposes `creatorAccountId:""`; leave-all acts only on registered service members. After those accounts leave, status is left and external members remain recorded consistently with the gateway.

```powershell
$env:TEST_DATABASE_URL=$env:DATABASE_URL
go test ./...
go test -race ./...
go vet ./...
```

Integration tests create unique schemas on the supplied real PostgreSQL. Missing `TEST_DATABASE_URL` explicitly skips PG integration; CI must provide it. Database business-row inputs validate transactions, pagination, Agent read/protocol/clock behavior and permissions; they provide no gateway listener, Session, Telegram response or platform effect. No account/gateway HTTP/SSE/RPC substitute remains. `TestServerProcessHelper` is a subprocess entrypoint and intentionally skips direct execution; the Agent budget recovery test invokes it as a real process. Model-provider protocol tests remain separate from account/gateway acceptance; real-key provider smoke is opt-in via `RUN_LIVE_PROVIDER=true`.

Tests formerly relying on gateway substitutes are retired rather than reported as passed. Real uploaded-account connection/check/group discovery, complete Telegram pagination, rate limits, message sending, moderation, ordinary-disconnect fencing and external-effect crash recovery require designated real-resource evidence. An empty real-service CI smoke only covers startup, authentication, account upload guidance and permission rejection. Production still retains its existing durable effects, ordered gateway inbox/cursor, account backoff and recovery rules; removing substitutes does not remove those behaviors.
