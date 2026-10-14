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

G1 stable-snapshot validation completed on 2026-10-14: `go test ./...` passed with real PostgreSQL enabled (7.338 s reported for the gateway package); `go test -race ./...` passed with the same integration DSN (8.378 s reported for the package); `go vet ./...` and `go build ./cmd/gateway ./cmd/probe` both exited zero. Race was executed on Windows using the existing CGO/GCC installation. That snapshot's PostgreSQL coverage reconstructed a service/repository and separately disconnected the writer session; G3 adds actual OS-hardkill evidence below.

Live evidence on 2026-10-14:

- Controller prepared consistent SQLite backups into ignored `.local/telegram`, and kept the original installation untouched.
- Three isolated aliases passed Self authorization and ResolveUsername for the user-designated test group. Initial GetParticipant(self) returned USER_NOT_PARTICIPANT for each.
- Under the controller's explicit stage-2 contract, each alias made one JoinChannel call to only that group. All three succeeded and subsequent self-membership was `member`; no FloodWait or join-request retry occurred.
- A later account-1-only read-only probe saved display name/username/capability evidence at `.local/telegram/account-1-profile.json`. The controller observed that external administration had since granted it admin/BanUsers capability.
- G1 performed no real send, kick, leave, promotion, new-group creation or unrelated dialogue/history read. NoUpdates prevented update/difference ingestion during all probe runs. Original Session files were opened read-only; renewed data was written only to the isolated gateway PostgreSQL tables.

G2, under a separate single-send authorization, issued exactly one public HTTP send by account-1 in the designated group. HTTP 202, one distinct `message_sent`, one own `message` across both service accounts, by-client-id query, and exclusive-cursor reconnect replay all passed. Only marker-matching evidence was retained in ignored `.local/acceptance/gateway-live.json`. No kick/leave/promotion/new-group operation was performed. G3 uses no live accounts and leaves the controller's old running gateway untouched.

## G3 revision 1: focused recovery repair

All commands below run in `gateway` with process-local Go cache/temp settings. PG commands set `GATEWAY_TEST_DATABASE_URL=postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable`. A scripted public `tg.Invoker` models the external Telegram API and encodes/decodes actual TL replies; the production adapter methods, public HTTP/SSE, real PG and service reconstruction run normally. Failure injection is limited to external persistence/lease and platform boundaries.

| Slice / actual command | Red observation | Green observation |
|---|---|---|
| `go test -run TestTelegramSendRecoveryRetainsOneRemoteRandomID -v .` | Prepared-before-RPC and accepted-before-result-persist both remained 504 after reconstruction. | Original query resolves to msgId 1; one remote message and every attempt retains the same random ID. |
| PG `go test -run TestDifferenceCannotCheckpointBeforeDurableEvents -v .` | Difference returned successfully before the injected event-save failure could block acknowledgement. | Difference and following cursor write fail; reconstruction retains cursor 7, retries the difference and exposes the event through SSE before cursor 88. |
| PG `go test -run TestCheckpointCannotWriteAfterLeaseHandover -v .` | Stale checkpoint wrote through a surviving pool after lock-connection termination. | Checkpoint write is refused by the fixed lease connection. |
| PG `go test -run 'TestTooLong|TestLiveTooLong|TestGlobalTooLong' -v .` | Too-long replies had no recovery evidence; omitted ID2 was accepted; the callback/global gap only advanced/logged. | Exact-ID recovered messages appear through SSE once; missing slots stay blocked and reject checkpoint; callback and global paths reconcile registered groups. |
| PG `go test -run TestTooLongMembershipSnapshotDoesNotHoldServiceQueue -v .` | Unrelated public query exceeded its 200 ms timeout while a snapshot RPC held the service lock. | Query returns 404 immediately while snapshot is blocked; service cancellation releases the recovery RPC. |
| PG `go test -run TestNewGapFailureReplacesOldCompletedEvidence -v .` | Metadata had no deadline; a new failure retained old completed evidence. | Metadata deadline is bounded to 30 s and new failure is visible as blocked. |
| PG `go test -run TestQueuedEffectsCannotCrossWriterHandover -v .` | Queued join/kick/leave each invoked the old adapter once after handover. | Each old request returns 503; all three old adapter call counts are zero. |
| PG `go test -run TestKickKeyReplayAfterRejoinNeverRemovesAgain -v .` | Reconstructed key replay kicked the rejoined target again (calls 2). | Calls remain 1, target stays present, original success replays even with actor offline; changed target returns 409. |
| PG `go test -run TestKickUnknownResultCannotRetryAgainstRejoinedTarget -v .` | Coverage extension after keyed receipts; not a separate red cycle. | Accepted kick + failed result persistence + rejoin + reconstruction returns 504 on same key, calls 1 and target present. |
| `go test -run TestSendRecoveryWaitsForFloodDeadlineWithoutResettingIt -v .` | Background reconciliation made a second remote call during FloodWait. | Only one call; persisted deadline is unchanged by background attempts. |
| `go test -run TestInviteAcceptsOmittedBody -v .` | Empty no-parameter invite POST returned 400. | Empty POST returns 200 and supplies the invite link. |

Additional executed recovery coverage, added after implementation and not claimed as new red-first cycles:

- `TestGatewayHardKillRetainsAcceptedEffectsAndSSEHistory`: real PG, separate `kapibala_gateway_crash` database, actual owned child `Process.Kill`, replacement child. Previously settled msgId/sentAt remain identical, accepted pending send converges, SSE contains exactly two distinct send receipts/messages and no creator join. Only this test's child is killed; controller services are untouched.
- PG/race `TestTooLongResumesAfterLastPersistedBatch`: range 1–205, omitted response slot 150 blocks the second batch. After PG/service reconstruction, recovery continues from ID101 rather than re-reading the committed first batch; final boundary is ID205. Actual output is ignored `.local/acceptance/gateway-g3-batch-resume.txt`.

Final G3 production-source validation on 2026-10-15: `go test -count=1 -json ./...` passed with real PG, package time 10.780 s; `go test -count=1 -race -json ./...` passed, package time 13.120 s; `go vet ./...` and `go build -o ../.local/acceptance/gateway-g3-bin/ ./cmd/gateway ./cmd/probe` exited zero. The later batch-resume test adds coverage only, changes no production source, and separately passed under race (1.905 s package time). An initial race attempt failed before compilation because the unquoted `CC` value with a space was split by Go; changing only process-local `CC=gcc` fixed it. That environment failure is not behavioral red evidence. No global Go/compiler setting changed.

Ignored local evidence: `gateway-g3-tests.jsonl`, `gateway-g3-race.jsonl`, `gateway-g3-race-environment-failure.jsonl`, `gateway-g3-vet.txt`, `gateway-g3-build.txt` under `.local/acceptance`. Normal-suite skip is exactly package `github.com/lbtlm/KapibalaAI_gotd/gateway`, test `TestGatewayCrashChild`, reason `subprocess helper only; GATEWAY_CRASH_CHILD=1`, when that environment switch is absent. Its real parent hardkill case executes. Packages with no tests report package skips. CI must supply PG; missing-DSN skips are not success evidence.

Boundaries: scripted Telegram tests prove adapter recovery mechanics, not a newly executed live outage/kick. Real Telegram removal and OS-killed real Telegram reconciliation remain unverified in this round. Recovery keeps the original random ID under the [official Telegram preconditions](https://core.telegram.org/api/updates); arbitrary platform effects have no general exactly-once guarantee. Too-long recovery batches exact IDs, has a 30 s deadline/10,000-ID cap, persists verified progress, and reports unavailable content and incomplete historical membership. Kick's accepted-before-result-persist window has no platform operation ID and stays unknown/fail-closed. The simulator's timing guarantees remain independent of platform outages and timestamp precision. Media materialization remains outside the delivered core slice.
