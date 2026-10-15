# Kapibala AI

多账号群组消息平台与 React 控制台。Go 后端维护账号状态、群成员、可靠消息时间线、定时序列和自动回复 Agent；消息网关和模型提供方分别运行在独立进程。规范以 [笔试题](docs/全栈开发工程师笔试题-候选人版.md) 和 [已批准实现契约](docs/implementation-contract.md) 为准。用户覆盖项为 Go + gotd、独立 OpenAI-compatible/DeepSeek provider，仓库保持私有。

```text
React :5173 ── HTTP / WebSocket ── backend :8080 ── PostgreSQL 16
                                      ├── HTTP / SSE ── gateway :8081 (Telegram)
                                      └── tool / audit ── provider :8082 (scripted / OpenAI-compatible)
```

演示账号为 `admin/admin`、`viewer/viewer`，仅适合本地受控演示。admin 可写，viewer 只读，后端执行权限检查。默认原生监听地址为 `127.0.0.1`，Compose 仅公开本机控制台端口。此项目尚无生产部署目标或生产身份管理配置。

## 快速启动：空账号与真实 Telegram 网关

需要 Go 1.24、Node.js 22.14.0、pnpm 10.18.1 和 PostgreSQL 16。为 backend 和 gateway 分别准备专用数据库。脚本编译、显式迁移、启动三个 Go 进程并等待健康检查；Vite 在另一个终端运行。新数据库的账号列表为空，登录后上传同名 Session + JSON，再主动连接、检测和同步所在群。启动空系统不会连接 Telegram。

Windows PowerShell，仓库根目录：

```powershell
Set-Location D:\web_projects\kapibala_ai
# 按你的 PostgreSQL 安装修改连接地址和用户；只需创建一次。
createdb -h 127.0.0.1 -p 55432 -U kapibala kapibala_e2e
createdb -h 127.0.0.1 -p 55432 -U kapibala kapibala_gateway_e2e
$env:E2E_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_e2e?sslmode=disable'
$env:E2E_GATEWAY_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_gateway_e2e?sslmode=disable'
node scripts/local-stack.mjs
```

Linux/macOS，仓库根目录：

```bash
createdb -h 127.0.0.1 -p 5432 -U kapibala kapibala_e2e
createdb -h 127.0.0.1 -p 5432 -U kapibala kapibala_gateway_e2e
E2E_DATABASE_URL='postgres://kapibala:your-password@127.0.0.1:5432/kapibala_e2e?sslmode=disable' \
E2E_GATEWAY_DATABASE_URL='postgres://kapibala:your-password@127.0.0.1:5432/kapibala_gateway_e2e?sslmode=disable' node scripts/local-stack.mjs
```

第二个终端：

```powershell
Set-Location D:\web_projects\kapibala_ai\frontend
pnpm install --frozen-lockfile
pnpm dev
```

打开 <http://127.0.0.1:5173>。停止服务用其终端的 Ctrl+C；它仅停止自身启动的进程，不删除数据库。停止 Vite 用其独立终端的 Ctrl+C。端口占用会拒绝启动。可设置 `BACKEND_PORT`、`GATEWAY_PORT`、`AGENT_PORT`；更换后端端口时让 Vite 的 `VITE_BACKEND_URL` 指向对应地址，例如 `http://127.0.0.1:18080`。`.env.example` 供 Compose 使用；原生脚本不会自动加载 `.env`。

## Docker Compose 演示

需要 Docker Engine 和 Compose v2。复制 `.env.example` 为忽略的 `.env`，填入仅含字母、数字、`-`、`_` 的本地随机 PostgreSQL 密码。修改 `CONSOLE_PORT` 可以避开已有 Vite。

```powershell
Copy-Item .env.example .env
# 编辑 .env 后执行
docker compose -p kapibala-demo up --build -d
docker compose -p kapibala-demo logs --tail 100 backend gateway agent
docker compose -p kapibala-demo down
```

数据库保存在该专用 Compose 项目的 volume，`down` 保留它。迁移先于 backend/provider 执行，`gateway-db` 仅在缺失时创建独立的 `kapibala_telegram_gateway` 数据库。网关只运行 Telegram 实现，账号由上传创建；模型 provider 仍按其独立配置选择 scripted/openai。已有账号的升级先按 [网关说明](gateway/README.md) 完成必要的离线凭据迁移，不能直接把旧模拟库改成真实库。镜像交付和保留数据的回滚见 [运维说明](docs/operations.md)。

## 配置与独立进程

| 进程 | 必需配置 | 默认与可选配置 |
|---|---|---|
| `backend/cmd/migrate` | `DATABASE_URL` | 显式更新 schema；server 不会隐式迁移 |
| `backend/cmd/server` | `DATABASE_URL` | `PORT=8080`、`GATEWAY_URL=http://127.0.0.1:8081`、`AGENT_URL=http://127.0.0.1:8082` |
| `gateway/cmd/gateway` | 独立 `DATABASE_URL` | `PORT=8081`；只支持 Telegram，从持久注册恢复账号 |
| `backend/cmd/agent` | `DATABASE_URL` | `PORT=8082`、`AGENT_MODE=scripted`；openai 需 `OPENAI_API_KEY` |
| `frontend` | 无密钥 | Vite 5173，代理 `/api`、`/ws`；`VITE_BACKEND_URL` 可覆盖后端地址 |

三个 Go 服务均支持 `BIND_HOST`；本机默认为 `127.0.0.1`，容器显式为 `0.0.0.0`。网关健康检查 `/health`，provider `/health`，backend `/api/health`。网关每数据库只允许一个 writer，因此同时运行测试与演示时必须使用不同数据库。

Telegram 账号通过控制台上传同名 Session + JSON，凭据与转换后的 Session 保存到独立网关数据库；正常启动不再读取 manifest。旧 manifest 只供显式离线迁移使用，原 Session 不修改，详见 [gateway/README.md](gateway/README.md)。API ID/hash、账号身份和 Session 禁止写入 Git、日志或 CI。只连接明确授权的账号；真实账号验收与空系统 CI 分开。

真实模型只在 provider 进程配置 `AGENT_MODE=openai`、`OPENAI_API_KEY`、`OPENAI_BASE_URL=https://api.deepseek.com`、`OPENAI_MODEL=deepseek-flash`、`OPENAI_API_STYLE=chat_completions`。Responses provider 使用相应服务的 `/v1` base 和 `OPENAI_API_STYLE=responses`。`AGENT_URL` 把后端连接到这个独立 provider，前端不接触 key 或推理内容。真实模式详细说明见 [backend/README.md](backend/README.md)。

## 验证

Go 集成必须提供两个专用真实 PostgreSQL DSN，不能使用存放真实账号的数据库。账号/网关测试保留纯逻辑与实际数据库行为，不提供模拟 Telegram 响应。CI 包装拒绝缺 DSN 和意外 skipped tests；明确 opt-in 的真实模型测试单独记录。Go 报告的 `[no test files]` 包只编译，不计为行为测试覆盖。

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:TEST_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
$env:GATEWAY_TEST_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_gateway_test?sslmode=disable'
node --test scripts/*.test.mjs
node scripts/go-test.mjs backend
node scripts/go-test.mjs gateway
node scripts/go-test.mjs backend -race
node scripts/go-test.mjs gateway -race
go -C backend vet ./...
go -C gateway vet ./...
pnpm --dir frontend test
pnpm --dir frontend typecheck
pnpm --dir frontend build
```

Playwright 启动真实空系统（包含 Vite），验证登录、零账号上传引导和 viewer 写权限拒绝；不创建虚构账号或提供模拟 API 响应。请先创建两个专用空数据库：

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:E2E_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_e2e?sslmode=disable'
$env:E2E_GATEWAY_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_gateway_e2e?sslmode=disable'
$env:BACKEND_PORT='18080'
$env:GATEWAY_PORT='18081'
$env:AGENT_PORT='18082'
$env:E2E_BASE_URL='http://127.0.0.1:15173'
$env:E2E_API_URL='http://127.0.0.1:18080'
$env:E2E_STACK_COMMAND='node scripts/local-stack.mjs'
pnpm --dir frontend exec playwright install chromium
pnpm --dir frontend test:e2e
```

已有服务会在本地被 Playwright 复用；最终验收应停止旧候选并启动待验收版本。CI 每次使用全新 PostgreSQL 和自身服务。真实 Telegram、真实 LLM 与人工界面验收分别记录，空系统/单元/CI 通过不代表这些外部验收通过。旧模拟故障用例已按用户要求移除，其历史结果不作为当前真实故障覆盖。

需求、真实验收与限制见 [验收矩阵](docs/验收矩阵.md)，协作状态见 [development-status.md](docs/development-status.md)，各组件 TDD/reuse 文档见 `docs/tdd-*.md`、`docs/reuse-*.md`。C1 媒体持久化不在当前核心范围，gateway 媒体端点返回 404。真实 Telegram 的服务器原时间戳为秒，真实平台无法保证规范中的两秒故障收敛，未知外部效果会保持待核实而不盲目重复。

开发分支全部 PR 到 `dev`，执行 **light**：策略/脚本测试、Go domain 纯单元测试、前端 unit/typecheck，不做前端构建、PG/race、E2E 或镜像构建；明确纯文档 PR 使用 **docs**，仅策略/脚本测试与 Gate。验收后的同仓 `dev` → `main` 发布 PR 执行 **full**，包括 Go/PostgreSQL/race/vet、前端 build、真实空系统 E2E、同批镜像冒烟与 SHA256 原制品。普通 dev/main push 不重复跑 CI；显式手动 full 为可选重验。

固定 required check 仍为 `CI Gate`，只接受所选档位的精确成功/跳过组合，普通文档提交无需 `[skip ci]`。轻量通过不代表 gateway/PG/恢复或完整构建验收通过。发布 PR 的原 artifact 可直接用于受控准备流程，绑定其实际 merge-test SHA、父提交与被测 source tree，无需为准备包再跑一次 full；历史 push 候选仍可回滚。分支流程见 [CONTRIBUTING.md](CONTRIBUTING.md)，档位与制品规则见 [运维说明](docs/operations.md)。尚未宣称生产部署、Release 或标签发布。

PR #1 已合入 `dev`。合并后的需求查漏、已定位的边界问题及 10 月 15 日白天优先级见 [2026-10-15 白天工作计划](docs/upgrade-plan-2026-10-15.md)；既有 CI 通过不代表新发现的场景已经验证。
