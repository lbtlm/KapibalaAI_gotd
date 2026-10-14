# Backend TDD and verification evidence

Owner: Orion, contract B1 attempt 1 revision 1. Approved seams: HTTP API, gateway HTTP/SSE, Agent turn/audit HTTP, WebSocket protocol, pure public variable/state contracts and real PostgreSQL reconstruction. Production state is PostgreSQL; in-memory gateway/model fixtures represent external transport services only.

Initial slice tests were written before implementation. Build/environment failures were excluded from behavioral red evidence: the sandbox initially could not access Go's standard `testing` package, and the copied source had not yet been adapted. The variable tracer initially failed because its module had no implementation; that compilation failure is disclosed below rather than claimed as a behavioral assertion red. Other recorded red cases are actual public-boundary assertions using the authorized installed toolchain.

| Slice | Behavioral red | Green evidence |
|---|---|---|
| Variable inheritance/preflight | Test preceded implementation; missing-module compilation failure only, no behavioral red claimed | `TestVariablesPersistFromOverrideAndEmptyDoesNotErase`, `TestUnresolvedThirdStepRejectsWholeSequence` |
| Auth/session family | `login 501` on real PostgreSQL HTTP fixture | Refresh rotation, old-token replay revokes newer access/refresh, immediate logout, viewer write 403 |
| Account CAS | Concurrent transition expected one winner, observed `0/0` | One 200 / one 409, terminal reconnect rejected |
| Group job | Create request `404` | Durable create → invite → member events → promote; creator/admin/member roles |
| Reliable send | Send request `404` | 504 exposed unknown, settled 1.5s later, query/event recovery; exactly one gateway send and one own row |
| Sequence start | Sequence request `404` | Third-step unresolved key returns 422 without running record; concurrent starts one 201 / one 409 |
| Existing import / WS | Import 405 and WS bad handshake | Viewer denied, membership verified, external owner untouched; committed monotonic seq replay |
| Agent protocol and effect | No run converged before timeout | Bad JSON → unknown tool → audited send → same-key retry → finish, five visible steps, one audit/one send |
| Provider context | Turn `501` | Real PostgreSQL provider reconstruction preserves raw assistant reasoning and four strict tools |
| Lost lock connection | Old worker HTTP context survived the lock connection | `pg_terminate_backend` kills only the run's advisory-lock connection; old HTTP effect is cancelled, another instance reconciles, kick/audit once |
| Final-block shape | Missing text accepted; empty text lost by serialization | Missing text rejects BAD_JSON while present empty text round-trips |
| Viewer logout | Viewer own logout returned 403 | Viewer logout 200 and former access 401; business writes still 403 |
| Full imported membership | External membership absent from imported group | All gateway members/roles visible; only selected accounts mapped, external owner stays null |

Additional actual boundary evidence:

- `TestAllDatabaseConnectionsLostReplayGatewayContentAndInconsistency`: an isolated TCP proxy cuts every backend PostgreSQL connection without disturbing other services. Gateway content appears within three seconds after recovery and a committed inconsistency notice is available through WS replay. Targeted green: 6.30s.
- `TestHardProcessKillAfterGatewayEffectRestoresSameAgentRunWithoutRepeatingSend`: an actual backend subprocess is killed after the gateway produces the effect, before send's 202 response. The reconstructed process finishes the same run, records sent, and observes exactly one send/one audit. Targeted green: 6.34s.
- `TestLostAdvisoryConnectionCancelsOldWorkerAndReconcilesKickOnce`: two backend instances, precise lock-connection termination and member reconciliation. Targeted green: 6.20s. PostgreSQL execution epochs prevent obsolete checkpoint writes/effect claims.
- Persistent rate limit/FIFO across repository reconstruction; terminal transition with an injected member-delete error fully rolls back; leave failure preserves the creator and failed member while others leave; invite readiness/expiry/already-member plus at most two promote calls; three malformed audit replies block without effects. Five-case targeted green: 26.89s.
- Reordered and duplicate SSE IDs, identical sentAt milliseconds, arbitrary old offline-own messages and stable pagination across late inserts. Targeted green: 6.76s.
- Sequence reconstruction schedules the first overdue step at restart + its delay; second scheduledAt is first received-sent timestamp + its delay. Targeted green: 8.17s.
- Actual 60-second run budget with slow legal eight-second turns terminates with `wall_clock`; the interrupted final turn is visible as `TURN_TIMEOUT`, and no more than twelve steps occur. Targeted green: 65.19s including group setup.
- `TestGatewayServiceGroupJobContract` uses the actual production fake gateway HTTP service rather than the fixture. It caught the empty invite-body mismatch; no-argument POST requests now send `{}`. Targeted green against isolated port 18081: 4.37s.
- Explicit live provider smoke loaded the user's local key silently, used an isolated PostgreSQL schema, and never connected Telegram. DeepSeek performed `get_recent_messages → send_message → finish`; audit returned pass. `TestLiveProviderToolConversationAndAudit`: 6.21s.

Integration tests create unique schemas on the controller's real PostgreSQL 16. `TEST_DATABASE_URL` is mandatory for complete verification. The production gateway contract test requires `GATEWAY_INTEGRATION_URL` pointing to an isolated fake gateway; CI starts it and must not exempt that case. `TestServerProcessHelper` is a re-execution entrypoint, so its normal-suite skip is intentional and exact-name exempted; its real child invocation is exercised by the hard-kill case. Only real-key smoke may be skipped in ordinary CI. No skipped live Telegram test is claimed as passed here.

Final stable source verification used Go 1.24.4 and the controller's isolated PostgreSQL 16:

- `go test -race -json -count=1 ./...`: 196.70s wall time, 25 passing tests, no data-race report, and one production-gateway contract failure because the externally hosted fake process on port 18081 had stopped. No backend source change was needed. Exactly two skips: `TestLiveProviderToolConversationAndAudit` (explicit real-key opt-in, independently passed above) and `TestServerProcessHelper` (non-test child-process entrypoint exercised by the hard-kill case).
- Controller restored the same stable fake gateway on port 18081 in its isolated `kapibala_e2e` database. Only `TestGatewayServiceGroupJobContract` reran: normal 4.32s and race 5.92s, both passed. The source stayed unchanged, so the other 25 race results remain valid. Effective coverage is 26 passed test cases, with the two precise skip explanations above; this is not represented as one uninterrupted all-green run.
- Final `go vet ./...` and `go build ./cmd/...`: exit 0. No worker commit, branch switch, shared process stop or real Telegram operation was performed.

Local machine-readable evidence, deliberately ignored by Git:

- `backend/.local/evidence/backend-race.jsonl` and `backend-race-summary.json`: complete original race stream and preserved external-dependency failure.
- `backend/.local/evidence/gateway-contract.jsonl`, `gateway-contract-race.jsonl`, `gateway-contract-summary.json`: restored-dependency targeted results.
- `backend/.local/evidence/static-build-summary.json`: final vet/build exit codes.

The preceding results describe the B1 snapshot. Its two-second downtime heuristic and additional recovery findings were repaired in B2 below. Hard-kill evidence covers the tested send, kick and sequence windows rather than an exhaustive kill at every checkpoint; the real OpenAI Responses endpoint was not exercised with a live key. Controller still owns combined Telegram + DeepSeek business acceptance, CI/CD and final release/PR acceptance. The separate real provider and gateway evidence does not establish a completed combined business flow.

## B2 targeted recovery repairs

The controller's fixed-snapshot Sentry review identified five blockers. Each received an observable red before its implementation, through authenticated backend HTTP, gateway/provider HTTP opponents and real PostgreSQL. The actual subprocess helper performs process kills rather than graceful shutdowns.

| Boundary | Observed red | Resulting behavior |
|---|---|---|
| Ordered inbox prefix | Higher left ID arrived first; lower join arrived 0.9s later. After duplicates and restart the visitor incorrectly remained a member. | A known younger lower ID holds every later ID until the entire executable prefix is eligible. Join then left restores the final absent membership. |
| Completed kick / rejoin | Gateway effect completed, backend died before its checkpoint, target rejoined, opponent HTTP server/PG pool were reconstructed. Old recovery produced two effects and removed the target again, despite one audit. | Stable `runId:toolUseId` and the original actor/target are persisted before POST. Same-key replay returns the durable prior result; exactly one effect/one audit and the rejoined target remains present. Membership and the old 14-second retry heuristic no longer decide whether to kick again. |
| Sent before sequence advance | A PG transaction parks advancement while the receipt persists, then the backend is killed. Next scheduledAt used the old receipt and preceded restart by 241.6ms. | The next overdue pending step is scheduled at restart plus its own delay, including a current sent/skipped checkpoint whose run index has not advanced. |
| Half-second stopped runtime | Near exhaustion, an actual 500ms process outage incorrectly produced `wall_clock`. | Shared PostgreSQL session presence distinguishes first-instance recovery from startup alongside a live peer. Only first-instance recovery excludes stopped time. With a peer online the same run still reaches `wall_clock`. The time-boundary fixture sets accumulated elapsed to 59600ms before killing the real process; it does not claim another physical 59.6-second wait. |
| Independent account deadlines | A six-second HTTP wait for one account prevented another account's 504 from completing within five seconds. | Per-account FIFO/advisory locking remains; separate accounts progress concurrently with an in-process busy guard. The ambiguous message queries first and performs at most one confirmed retry within its deadline. |

Two additional findings were closed within these same recovery paths:

- The first race batch exposed a logical sequence race: public HTTP showed `message.deliveryStatus=sent` and a non-null step sentAt, but a stale worker overwrote the step to accepted and left the run stuck. Accepted now updates only pending steps, preserving sent. Existing timeouts and assertions were kept intact.
- The controller identified that disabling autoKick during the interruption incorrectly recorded the already-audited original step as `POLICY_DENIED`. The new public rejoin subcase first reproduced that exact error, then verified original-step success, one effect/one audit and refusal of a subsequent new kick under the disabled policy. A persisted intent is recovered before new-tool policy/audit evaluation.

Go 1.24.4 / real isolated PostgreSQL 16 verification on the repaired scope:

- Initial affected normal batch: 16 top-level tests plus two budget subtests passed, zero skips, 121.874s.
- Initial affected race batch: 14 top-level tests plus two budget subtests passed; the two sequence tests failed with the accepted regression above. No `DATA RACE` report. Elapsed 147.116s; the failures remain in the evidence rather than being rewritten as an all-green run.
- After the narrow sequence/policy repairs, only the two sequence tests and the completed-kick test (both policy subcases) reran: normal 43.917s and race 54.420s, all passed, zero skips and no detected data race. The other 14 race results are retained. Effective affected coverage is 16 top-level tests and four subtests across these batches, not one uninterrupted green batch.
- Final `go vet ./...`, `go build ./cmd/...` and `git diff --check -- backend`: exit 0. The old continuous 60-second budget evidence is reused because its charging formula did not change; the new first-instance/peer recovery boundaries were tested separately. Unaffected auth/import/job checks were not repeated. Full CI at the controller's new candidate SHA remains required.

Ignored evidence: `backend/.local/evidence/b2-normal.jsonl`, `b2-race.jsonl`, `b2-targeted-normal.jsonl`, `b2-targeted-race.jsonl` and `b2-summary.json`. The summary records the red excerpts, exact batch counts, time-boundary setup and frozen source hashes. An initial test-opponent cleanup hang was corrected by consuming the HTTP request body; it is not counted as a behavioral red.

Orion's production/tests/docs are stopped for targeted Sentry re-review and controller integration. No live Telegram/DeepSeek operation, commit, branch change or existing shared-service restart was performed during B2. A legacy inflight kick without a durable key fails closed because current membership cannot prove its original outcome. Combined live acceptance, independent re-review and remote CI on the final candidate are still controller gates.
