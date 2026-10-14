# Gateway reuse record

Owner: Niko. Source repository remains read-only: `D:/web_projects/card_code_workspace/go_tg_assistant`, verified HEAD `1d7ca2240ec8c41650c27b4c7965baf84cf36a35` (`git rev-parse HEAD`, 2026-10-14). Source dependency lock: gotd/td v0.112.0. Gateway uses that exact version and Go 1.24.

| Source | Destination | Reuse and adaptation |
|---|---|---|
| `backend/internal/tg/ops/group.go` | `gateway/internal/ops/group.go` | Direct file copy, then retain original `JoinChannel`, `LeaveChannel`, `ImportChatInvite`, `CreateChannel`, `GetParticipants` bodies only. |
| `backend/internal/tg/ops/channel.go` | `gateway/internal/ops/channel.go` | Direct copy, then retain `ExportChatInvite`, `EditChatAdmin` only. |
| `backend/internal/tg/ops/message.go` | `gateway/internal/ops/message.go` | Direct copy, then retain original `SendMessage` only. Removed copied media builders, reactions, history, bot callbacks and their imports. |
| `backend/internal/tg/telethon.go:224` | `gateway/session.go` | Reuses SQLite row decoding, 256-byte key validation and gotd key/ID conversion. Changes SQLite DSN to read-only; no source-file writes or source database import. |
| `backend/internal/tg/tdata.go:61` | `gateway/session.go` | Reuses ZIP extraction → `tdesktop.Read` → `session.TDesktopSession` conversion. Adds path/size bounds and requires exactly one identity. Drops debugging output and hard-coded credentials. |
| `backend/internal/tg/online_manager_lifecycle.go:19,197,395` | `gateway/telegram.go` | Adapts client.Run, Self authorization, update-manager lifetime and cancellation; removes source tenant/task/UI/profile/health side effects. A single per-alias client and fixed PostgreSQL writer lease own the lifecycle. |
| `backend/internal/tg/online_manager_updates.go:11,121` | `gateway/telegram_updates.go` | Reuses Updates/Combined/Short dispatch and NewMessage/NewChannelMessage decoding. Retains own outgoing messages, filters registered groups, adds membership updates, durable event deduplication and random-ID correlation. |

Necessary new code supplies this examination's public HTTP/SSE envelopes, deterministic fault controls and delayed effects, PostgreSQL state/event persistence, SDK update-state storage, group/role mapping, the standalone entrypoint and the bounded private probe. The source application has no section-2.1 simulator or this independent gateway database model, so copying its HTTP/business handlers would introduce unrelated features and credentials.

No `.git`, production `.env`, source logs, database backups or original Sessions were copied from this source repository. The controller separately prepared user-authorized isolated test sessions under ignored `.local/telegram`; only those files were opened for live checks. Source AGENTS rules were read before source exploration; the source had no CodeGraph index. Once the controller indexed this repository, subsequent gateway understanding/edits called CodeGraph first.
