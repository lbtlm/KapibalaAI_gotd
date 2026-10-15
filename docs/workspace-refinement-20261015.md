# Workspace refinement, 2026-10-15

User-approved scope; controller implements and verifies alone. Continue issue #4 / PR #5 on the existing branch, with incremental commits. Preserve actual credentials, history and the single TelegramAdapter connection owner. Do not introduce another client cache or simulate account/Gateway success. Real Telegram acceptance remains manual.

## Accepted behavior

- Account upload, detection, persistence, group discovery/join, grouping, logs and guarded deletion. An operation on one account must not disable other accounts or hold a global connection lock while waiting for storage/network. Duplicate connection attempts for the same account remain fenced; cancellation and shutdown wait for actual Run termination. Distinguish startup deadline expiry from an unavailable Gateway.
- Reuse lifecycle patterns from go_follow_live (00eb35a56ad1f4bf382e6e3906d35bacd1740376, backend/internal/online/manager.go) and go_tg_assistant (1d7ca2240ec8c41650c27b4c7965baf84cf36a35, backend/internal/tg/online_manager_lease.go and online_manager_updates.go), adapted into the existing owner. No wholesale manager transplant.
- Keep composite group creation: creator, multiple ordered members, first member defaults to administrator; an explicitly selected member may replace that default. User-supplied group name/address, actionable address-conflict fallback, confirmed joins before promotion, visible owner/admin roles.
- Each group has its own Agent context, pending messages and runs. Pin tool actions to its designated administrator; never silently substitute another account. At most one running Agent per group, different groups progress independently. Only external-member messages trigger an enabled Agent. Script messages, Agent replies and manual messages from any owned account never trigger it. Administrator changes must not alter the identity of an in-flight action.
- Manage reusable JSON scripts separately; admin/member sender roles, delays and variables support visual editing and resolved preview. Bind a script and participating accounts within each group; each group runs independently.
- Group workspace displays live/history messages, media, member roles, scripts, Agent activity and traceable logs. Highlight external messages and provide a separate view.
- End marketing disables new Agent/script work and settles in-flight effects before ordered B2 leave: nonowners sequentially, continue after a nonowner failure, retain owner if any failure, owner last otherwise. Preserve history and connections used by other groups.

## Delivery slices and verification

1. Account concurrency and timeout chain: actual PostgreSQL blocked-load regression, same-account fencing/cancellation, pure UI pending states, transport-code/budget tests, focused race/static/build checks. Then update local Docker at 5173.
2. Group administrator identity, external-only triggering and end-marketing admission: actual PostgreSQL behavior tests and affected worker checks; preserve existing idempotency/recovery paths.
3. Group setup, script binding/editor and live workspace/media: validate public schemas and user flows against actual empty services; manual real Telegram acceptance remains explicit.

Remove confirmed replaced paths within affected responsibilities; do not perform unrelated cleanup. Each slice records its actual evidence and limitations. No full release, merge or tag is authorized here.

## R22 evidence

The old Connect mutex held the entire storage/Self/update-readiness wait. A real PostgreSQL table lock reproduced cross-account blocking; reserving the existing session slot and releasing the mutex before I/O fixed it. Same-account duplicate ownership and cancellation checks pass. Gateway targeted normal/race, vet and build pass. Frontend row pending-state regression plus deletion/auth checks: 23 passes, typecheck/build pass. Backend connection-budget and transport-error tests pass including race; vet/build pass.

Connection startup has an internal 30-second deadline; the old backend HTTP timeout was 12 seconds and the browser timeout 15 seconds. Connect now has a 40-second outer HTTP budget and check/connect a 60-second browser budget, without extending unrelated requests. Only actual timeout errors map to NETWORK_TIMEOUT; other transport failures map to GATEWAY_UNAVAILABLE.

Both stored Telegram DC addresses accepted TCP from the Gateway network namespace. This does not establish successful MTProto authentication or update readiness. No real account retry or send was performed. Local evidence resides under ignored `.local/inspection-20261015/root-r22/`.

## R23: group identity, script execution and end marketing

Controller-only implementation continues under the user's override. Reused existing durable jobs, Agent effects/audit, sequence snapshots, Gateway promotion receipts and B2 leave worker. Migration10 adds group title/address/designated administrator, saved script input, immutable run/step actors and job logs. Existing group administrator/member bindings are backfilled from current membership. No live platform operation was executed for validation.

Red/green: persisted create input lost selected administrator/title; PATCH silently ignored administrator; leave-all kept Agent enabled and queued work; script preview did not resolve sender identity. All reproduced against isolated PostgreSQL before repair. New tests cover immutable running script content after editing, unavailable pinned administrator without owner fallback, rejection of identity change during a run, external-only trigger filtering, and bounded leave admission against a locked group. Prior historical migration tests now rewind all newer migrations before applying the older data migration; the repeated hand-written version-only rewind paths were replaced.

Cross-group worker red: a locked first leave job prevented an unrelated second group from completing. Shared launchWorker reuses the existing advisory lease and application cancellation/WaitGroup, with per-kind limits reserving sending capacity (4 sends,4 Agents,2 group jobs,2 sequence workers). Removed duplicated goroutine/map bookkeeping and serial group/sequence dispatch. Gateway still owns all account connections through its existing adapter. Generic job member administration/join endpoints reuse existing phase/receipt logic; no extra worker framework.

End marketing immediately disables new Agent work, cancels scripts and undispatched messages, and waits for running/uncertain effects before B2 leave. Membership-changing jobs block admission until their membership snapshot settles. Remaining in-group accounts supply member reads after previous accounts leave; stopped accounts are not chosen for that purpose. A nonowner failure still prevents owner departure. Ambiguous external effects remain explicit rather than being assumed finished.

UI: group creation selects title, address/fallback, owner, member order and designated administrator; group workspace separates live chat, highlighted external messages, per-group script/member configuration and job logs. Script library supports JSON and visual editing. The replaced sequence-page execution form was removed. Preview exposes the resolved actor and text; ongoing runs keep their original snapshot.

Evidence: 16 affected backend integration top-level tests passed, including schema reconstruction; final worker/identity targeted race19 top+sub cases passed. Final capacity/admission follow-up separately recorded. Frontend46 tests passed and typecheck/build passed; Gateway address-error mapping test and vet/build passed. Raw files remain ignored under root-r22. These are local component/integration checks, not real Telegram creation/join/promotion/leave or media acceptance. Full live media/download and direct account link-join are still pending the next slice.

## R24: concrete Telegram startup failure and account link join

User explicitly authorized diagnosing and verifying only the same selected offline account, with no sends or group mutations. Live reproduction returned NETWORK_TIMEOUT after30.03s. Unauthenticated req_pq probes in the Gateway network namespace received genuine resPQ replies from DC4/DC5; server clock difference was3s. Whitelisted SDK startup diagnostics then proved Telegram returned AUTH_KEY_DUPLICATED during initConnection; gotd v0.112 internally retried that permanent failure until the outer startup deadline. No evidence identifies which earlier process caused the duplication, and the other two accounts were not tested.

Repair: the existing ingestGate cancels the same managed client when the pinned SDK reports an explicit permanent authorization failure. Both Run completion and cancellation return that original error before a generic timeout. AUTH_KEY_DUPLICATED remains the public code and maps consistently to session_expired in Gateway/backend; frontend explains that newly authenticated Session credentials are required. Removed the duplicated backend terminal classification in favor of terminalForError. Safe SDK diagnostics discard all original fields and permit only fixed stages and classified causes. The original Session cannot be repaired by restarting: Telegram's error contract says the authorization is already invalidated (https://core.telegram.org/api/errors).

Pure SDK error-boundary regression went red on continued retry, then green; no fake client or simulated Gateway success. Focused gateway race checks cover cross-account startup, duplicate ownership, cancellation and terminal classification. Backend terminal/API regressions and frontend21 affected tests passed. The earlier direct account link-join slice reuses inspection's managed Session/fences and account lock/log path; supports public/private links and explicit approval_pending without claiming membership, and preserves concrete link errors. Link join has parser/terminal-boundary coverage only, no live join acceptance. Group/script progress now includes the immutable sender and text.
