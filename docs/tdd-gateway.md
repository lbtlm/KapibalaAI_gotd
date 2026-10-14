# Gateway TDD and acceptance evidence

Owner Niko; G1 attempt 1 revision 1. Approved seams: gateway HTTP/SSE, public session conversion/configuration lifecycle and real PostgreSQL reconstruction. Protocol tests invoke HTTP and observe response/SSE results; they do not query application tables to prove behavior. The lease test uses PostgreSQL administration only to inject a connection failure, then verifies the service through HTTP.

The first standard-library attempt failed because the sandbox default Go cache was inaccessible; the first local socket attempt was also denied. These environment errors are not counted as behavioral red evidence. Subsequent commands used process-local `GOCACHE=D:/web_projects/kapibala_ai/tmp/go-cache-gateway`, `GOTMPDIR=D:/web_projects/kapibala_ai/tmp`, `GOTOOLCHAIN=local` and approved loopback/network execution. Global Go settings were not changed. Go is 1.24.4; Windows CGO is enabled and the existing GCC is `D:/Program Files/mingw64/bin/gcc.exe`.

| Slice | Actual red observation | Actual green observation |
|---|---|---|
| HTTP account/group skeleton | After cache correction, `go test ./...`: module had no non-test Go files. | Stable connect identity, offline send 409 and immediate creator membership passed. |
| Permanent suspension | `go test -run TestSuspensionOnConnectIsPermanent -v .`: second connect got 200, wanted 403. | Same command passed after terminal state propagation. |
| Delayed member events | `go test -run TestMemberListChangesBeforeDelayedEvent -v .`: member event contained empty group/user IDs. | Passed after delayed payload IDs were retained. |
| PostgreSQL reconstruction | `go test -run TestPostgresRestart -v .`: OpenPostgres missing. | Real PostgreSQL passed: timeout-accepted send survives immediate service/store reconstruction and resolves by client ID. |
| Session conversion | `go test -run TestSessionImport -v .`: ImportSession missing. | Synthetic valid gotd JSON remains unchanged; one-byte authorization key is rejected. |
| Asynchronous suspension | `go test -run TestAsyncSuspension -v .`: reconnect got 200, wanted 403. | Passed; async failure makes account terminal and removes its membership. |
| Create recovery key | `go test -run TestCreateTimeout -v .`: controlled unknown create returned 200, wanted 504. | Passed; key replay returns one persisted group. |
| Database mode separation | `go test -run TestGatewayRefuses -v .`: fake state silently accepted in Telegram mode. | Passed with persisted mode guard. |
| Writer lease disconnection | Real-PG `go test -run TestLostPostgresWriterLease -v .`: old process still created a group with 200 after its lock session was terminated. | Passed: old service returns 503/health 503; replacement writer can acquire the lease. |

Additional protocol coverage passes: simulator admin disabled by default; all-endpoint 503 including query/SSE; each rate-limited retry resets its cooldown; accepted/rejected send 504 and kick convergence within two seconds; invitation ready delay; join/promote-before-member/duplicate-join; no-permission kick; leave 500 without effect; owner-left errors; full SSE history, exclusive cursor, duplicate original event IDs, adjacent disorder, own send/message pairing and two actual messages for one clientMsgId. These coverage extensions were added after their earlier implementation; they are not represented as independent red-first cycles.

Final full test commands use `GATEWAY_TEST_DATABASE_URL=postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable`. PostgreSQL integration is explicitly skipped when that variable is absent. The production entrypoint never substitutes in-memory persistence.

Stable-snapshot validation completed on 2026-10-14: `go test ./...` passed with real PostgreSQL enabled (7.338 s reported for the gateway package); `go test -race ./...` passed with the same integration DSN (8.378 s reported for the package); `go vet ./...` and `go build ./cmd/gateway ./cmd/probe` both exited zero. Race was executed on Windows using the existing CGO/GCC installation; it was not merely delegated to future Linux CI. The PostgreSQL coverage reconstructs a service and repository and separately disconnects the writer session; an OS-killed gateway child process is not yet part of this worker's tests.

Live evidence on 2026-10-14:

- Controller prepared consistent SQLite backups into ignored `.local/telegram`, and kept the original installation untouched.
- Three isolated aliases passed Self authorization and ResolveUsername for the user-designated test group. Initial GetParticipant(self) returned USER_NOT_PARTICIPANT for each.
- Under the controller's explicit stage-2 contract, each alias made one JoinChannel call to only that group. All three succeeded and subsequent self-membership was `member`; no FloodWait or join-request retry occurred.
- A later account-1-only read-only probe saved display name/username/capability evidence at `.local/telegram/account-1-profile.json`. The controller observed that external administration had since granted it admin/BanUsers capability.
- No real send, kick, leave, promotion, new-group creation or unrelated dialogue/history read was performed by this worker. NoUpdates prevented update/difference ingestion during all probe runs. Original Session files were opened read-only; renewed data was written only to the isolated gateway PostgreSQL tables.

Real HTTP send/event correlation, real member removal, media handling and crash reconciliation with Telegram remain unverified until the controller's complete-stack live acceptance. The real adapter implements operations, but an unknown platform effect without a random-ID acknowledgement cannot be proven absent; it stays a 504 query result and is never blindly resent. The simulator's timing guarantees are tested independently from real Telegram availability and timestamp precision.
