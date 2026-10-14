# Backend and Agent provider

Go 1.24 and PostgreSQL 16+. The backend checks the schema and refuses stale/dirty databases. Migrations run explicitly and are repeatable.

```powershell
cd D:\web_projects\kapibala_ai\backend
$env:DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/server
```

Defaults: `BIND_HOST=127.0.0.1`, `PORT=8080`, `GATEWAY_URL=http://127.0.0.1:8081`, `AGENT_URL=http://127.0.0.1:8082`. Docker explicitly binds `0.0.0.0`. Demo-only users are `admin/admin` and `viewer/viewer`. All domain writes require admin. Access tokens expire after 15 minutes; refresh tokens are HttpOnly cookies and rotate atomically. Replay revokes the entire session immediately.

Run `cmd/agent` in a separate terminal with the same `DATABASE_URL`, `PORT=8082` and `AGENT_MODE=scripted` or `openai`. Only this provider reads `OPENAI_API_KEY`. DeepSeek defaults are `OPENAI_API_STYLE=chat_completions`, `OPENAI_BASE_URL=https://api.deepseek.com`, `OPENAI_MODEL=deepseek-flash`. To use OpenAI Responses set `OPENAI_API_STYLE=responses`, its `/v1` base URL and an appropriate model. Persisted assistant reasoning/tool items are replayed across provider restarts. No Anthropic connection is used.

For deterministic faults enable `AGENT_ADMIN_ENABLED=true` in scripted mode and `POST /admin/script` with `{turns:["<raw response>"],audits:["<raw audit verdict>"],delayMs:0}`. Empty arrays restore the default read → send → finish script. Invalid JSON strings intentionally exercise backend protocol errors. The admin endpoint is unavailable in openai mode.

Existing group: connect the selected service accounts, use the gateway's `POST /groups/import` to resolve the designated existing group, then backend `POST /api/groups/import {gatewayGroupId,accountIds}`. All members and their roles come from gateway membership; only selected service accounts receive an accountId, while external members retain null. An imported group with an external owner exposes `creatorAccountId:""`; leave-all acts only on registered service members. After those accounts leave, status is left and external members remain recorded consistently with the gateway.

```powershell
$env:TEST_DATABASE_URL=$env:DATABASE_URL
$env:GATEWAY_INTEGRATION_URL='http://127.0.0.1:18081' # isolated fake gateway only
go test ./...
go test -race ./...
go vet ./...
```

Integration tests create unique schemas on the supplied real PostgreSQL. Missing `TEST_DATABASE_URL` explicitly skips PG integration; CI must provide it. The production gateway contract case also requires `GATEWAY_INTEGRATION_URL`; CI starts an isolated fake gateway before running that case. `TestServerProcessHelper` is a subprocess entrypoint and intentionally skips normal direct execution; the hard-kill test invokes it as a real process. Real-key provider smoke is opt-in via `RUN_LIVE_PROVIDER=true`. Transport fixtures implement external HTTP/SSE model/gateway seams; production has no in-memory business repository. Gateway history plus a conservative persisted SSE cursor preserves events through database outages. Outage alerts are buffered as transport notices and committed for WS replay after database recovery.
