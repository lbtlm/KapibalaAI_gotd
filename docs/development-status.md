# Development status

Observed 2026-10-14 22:26 Asia/Shanghai. Coordination record, not an acceptance report.

| Employee / assignment | Runtime ID | Scope | Requested runtime | Status |
| --- | --- | --- | --- | --- |
| Niko / Telegram gateway | `/root/niko_gateway_build` | `gateway/`, gateway reuse/TDD docs | GPT-6.1 Sol / Ultra | Fake protocol initial red/green tests passed; persistence and gotd in progress |
| Orion / application backend | `/root/orion_backend_build` | `backend/`, backend reuse/TDD docs | GPT-6.1 Sol / Ultra | Real PostgreSQL auth rotation/replay/logout and variable preflight slices passed |
| Iris / frontend | `/root/iris_frontend_build` | `frontend/`, ignore files, frontend reuse/TDD docs | GPT-6.1 Sol / High | Copied/adapted shell and five required views in progress |
| Rowan / CI/CD | Not dispatched | Root workflows, Compose, verification/start scripts | Pending explicit binding | Queued until a slot is free; approved release-engineer profession matches |

Controller owns final acceptance. Worker model/effort were explicitly requested through spawn; Fast is not independently observable. Maximum three active workers. No recursive delegation.

## Decisions and remaining gates

- Go/gotd and React 18; DeepSeek through OpenAI-compatible API, no Claude. Live API key pending; scripted tests continue.
- Real test resource paths stay in ignored local configuration. Recent healthy metadata does not prove current login validity.
- Isolated PostgreSQL 16.11 on localhost port 55432; no existing application database modified.
- `main` stable, `dev` integration; all working branches PR into `dev`. No business code was committed before this rule was supplied.
- Remaining: full A/B and S1–S8 behaviors, combined browser E2E, stable auth/recovery independent review with verified read-only runtime, real Telegram probe/acceptance, keyed DeepSeek acceptance, remote CI and reviewed PR delivery.

## Git state

- Empty bootstrap: `4ce04a3`, both main/dev.
- Requirements/workflow commit: `99cfa86` on `feat/telegram-agent-platform`.
- Draft PR: https://github.com/lbtlm/KapibalaAI_gotd/pull/1, base `dev`.
- Remote main/dev protection is configured and read back: PR required, required CI Gate, admins enforced, no force push or deletion. Default branch main; repository private.

## Interface revision and CodeGraph

- User rejected the first UI visual direction. Functional baseline committed as `3403a4e`; Iris resumed F2 with interface-design and design-taste-frontend. Visual acceptance remains pending, despite F1 unit/build passing. Rowan dispatch deferred until a worker slot is released.
- User explicitly requested CodeGraph initialization. `codegraph init .` succeeded: 86 files, 1,062 nodes, 2,389 edges. MCP explore verified. Local DB/WAL/SHM ignored; only .codegraph/.gitignore is tracked. Re-sync before stable review. All active workers notified to use CodeGraph first.
- Three isolated Telegram Session copies passed authentication and resolved the designated group. Each joined exactly once successfully; all are ordinary members. No send/kick/leave/role change has occurred. Further live acceptance requires the appropriate group permissions.
