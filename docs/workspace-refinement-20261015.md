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
