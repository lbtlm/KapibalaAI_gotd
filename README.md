# Kapibala AI

多账号群组消息平台与 React 控制台。Go 后端维护账号状态、群成员、可靠消息时间线、定时序列和自动回复 Agent；消息网关和模型提供方分别运行在独立进程。规范以 [笔试题](docs/全栈开发工程师笔试题-候选人版.md) 和 [已批准实现契约](docs/implementation-contract.md) 为准。用户覆盖项为 Go + gotd、独立 OpenAI-compatible/DeepSeek provider，仓库保持私有。

```text
React :5173 ── HTTP / WebSocket ── backend :8080 ── PostgreSQL 16
                                      ├── HTTP / SSE ── gateway :8081 (fake / Telegram)
                                      └── tool / audit ── provider :8082 (scripted / OpenAI-compatible)
```

演示账号为 `admin/admin`、`viewer/viewer`，仅适合本地受控演示。admin 可写，viewer 只读，后端执行权限检查。默认原生监听地址为 `127.0.0.1`，Compose 仅公开本机控制台端口。此项目尚无生产部署目标或生产身份管理配置。

## 快速启动：真实数据库 + fake 网关 + scripted Agent

需要 Go 1.24、Node.js 22.14.0、pnpm 10.18.1 和 PostgreSQL 16。准备一个专用空数据库 `kapibala_e2e`，不要使用真实 Telegram 数据库。脚本自动编译、显式迁移、启动三个 Go 进程、等待健康检查；Vite 在另一个终端运行。

Windows PowerShell，仓库根目录：

```powershell
Set-Location D:\web_projects\kapibala_ai
# 按你的 PostgreSQL 安装修改连接地址和用户；只需创建一次。
createdb -h 127.0.0.1 -p 55432 -U kapibala kapibala_e2e
$env:E2E_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_e2e?sslmode=disable'
node scripts/fake-stack.mjs
```

Linux/macOS，仓库根目录：

```bash
createdb -h 127.0.0.1 -p 5432 -U kapibala kapibala_e2e
E2E_DATABASE_URL='postgres://kapibala:your-password@127.0.0.1:5432/kapibala_e2e?sslmode=disable' node scripts/fake-stack.mjs
```

第二个终端：

```powershell
Set-Location D:\web_projects\kapibala_ai\frontend
pnpm install --frozen-lockfile
pnpm dev
```

打开 <http://127.0.0.1:5173>。停止 fake 栈用其终端的 Ctrl+C；它仅停止自身启动的进程，不删除数据库。停止 Vite 用其独立终端的 Ctrl+C。端口占用会拒绝启动。可设置 `BACKEND_PORT`、`GATEWAY_PORT`、`AGENT_PORT`；更换后端端口时让 Vite 的 `VITE_BACKEND_URL` 指向对应地址，例如 `http://127.0.0.1:18080`。`.env.example` 供 Compose 使用；原生脚本不会自动加载 `.env`。

## Docker Compose 演示

需要 Docker Engine 和 Compose v2。复制 `.env.example` 为忽略的 `.env`，填入仅含字母、数字、`-`、`_` 的本地随机 PostgreSQL 密码。修改 `CONSOLE_PORT` 可以避开已有 Vite。

```powershell
Copy-Item .env.example .env
# 编辑 .env 后执行
docker compose -p kapibala-demo up --build -d
docker compose -p kapibala-demo logs --tail 100 backend gateway agent
docker compose -p kapibala-demo down
```

数据库保存在该专用 Compose 项目的 volume，`down` 保留它。迁移作为一次性服务先于 backend/provider 执行。网关和 provider 默认 fake/scripted，模拟控制端点在容器内关闭。此演示不连接 Telegram 或模型服务。镜像交付、手动推广与保留数据的回滚见 [运维说明](docs/operations.md)。

## 配置与独立进程

| 进程 | 必需配置 | 默认与可选配置 |
|---|---|---|
| `backend/cmd/migrate` | `DATABASE_URL` | 显式更新 schema；server 不会隐式迁移 |
| `backend/cmd/server` | `DATABASE_URL` | `PORT=8080`、`GATEWAY_URL=http://127.0.0.1:8081`、`AGENT_URL=http://127.0.0.1:8082` |
| `gateway/cmd/gateway` | `DATABASE_URL` | `PORT=8081`、`GATEWAY_MODE=fake`；Telegram 需 `TELEGRAM_ACCOUNTS_FILE` |
| `backend/cmd/agent` | `DATABASE_URL` | `PORT=8082`、`AGENT_MODE=scripted`；openai 需 `OPENAI_API_KEY` |
| `frontend` | 无密钥 | Vite 5173，代理 `/api`、`/ws`；`VITE_BACKEND_URL` 可覆盖后端地址 |

三个 Go 服务均支持 `BIND_HOST`；本机默认为 `127.0.0.1`，容器显式为 `0.0.0.0`。网关健康检查 `/health`，provider `/health`，backend `/api/health`。网关每数据库只允许一个 writer，因此同时运行测试与演示时必须使用不同数据库。

真实 Telegram 使用独立数据库和 `.local/telegram/` 下的私有账号 manifest、隔离 Session 副本；原 Session 不修改。manifest 格式与只读导入支持见 [gateway/README.md](gateway/README.md)。API ID/hash、设备资料、账号身份与 Session 路径只存本地私有配置，禁止写入 Git、日志或 CI。只连接明确授权的账号/测试群；不要把真实 Session 交给 fake 栈或 CI。

真实模型只在 provider 进程配置 `AGENT_MODE=openai`、`OPENAI_API_KEY`、`OPENAI_BASE_URL=https://api.deepseek.com`、`OPENAI_MODEL=deepseek-flash`、`OPENAI_API_STYLE=chat_completions`。Responses provider 使用相应服务的 `/v1` base 和 `OPENAI_API_STYLE=responses`。`AGENT_URL` 把后端连接到这个独立 provider，前端不接触 key 或推理内容。真实模式详细说明见 [backend/README.md](backend/README.md)。

## 验证

Go 集成必须提供专用真实 PostgreSQL DSN，并先启动使用另一数据库的 fake 网关（可复用前述 fake 栈）。`GATEWAY_INTEGRATION_URL` 运行生产 HTTP 网关组合测试，不能指向 Telegram 模式。CI 包装拒绝缺 DSN 和意外 skipped tests；仅明确 opt-in 的真实模型测试、未激活的精确子进程辅助入口可跳过。Go 报告的 `[no test files]` 包只编译，不计为行为测试覆盖。

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:TEST_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_test?sslmode=disable'
$env:GATEWAY_TEST_DATABASE_URL=$env:TEST_DATABASE_URL
$env:GATEWAY_INTEGRATION_URL='http://127.0.0.1:8081'
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

Playwright 实际启动四进程栈（包含 Vite），执行登录 → 群 → 外部 fake 消息触发 Agent → 步骤视图，以及 viewer 写权限拒绝：

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:E2E_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_e2e?sslmode=disable'
$env:E2E_STACK_COMMAND='node scripts/fake-stack.mjs'
pnpm --dir frontend exec playwright install chromium
pnpm --dir frontend test:e2e
```

已有服务会在本地被 Playwright 复用；最终验收应停止旧候选并启动待验收版本。CI 每次使用全新 PostgreSQL 和自身服务。真实 Telegram、真实 LLM 与人工界面验收分别记录，fake/单元/CI 通过不代表这些外部验收通过。

需求、真实验收与限制见 [验收矩阵](docs/验收矩阵.md)，协作状态见 [development-status.md](docs/development-status.md)，各组件 TDD/reuse 文档见 `docs/tdd-*.md`、`docs/reuse-*.md`。可选 C2 provider 接入和 C3 浏览器测试已提供；C1 媒体持久化不在当前核心范围，gateway 媒体端点返回 404。真实 Telegram 的服务器原时间戳为秒，模拟契约为毫秒。真实平台无法保证网关规范中的两秒故障收敛，未知外部效果会保持待核实而不盲目重复。

开发分支全部 PR 到 `dev`，验收后单独 `dev` → `main`；固定 required check 为 `CI Gate`。流程见 [CONTRIBUTING.md](CONTRIBUTING.md)。CI 构建绑定 SHA 的镜像，在专用 Compose 项目中验证三个服务健康、前端页面/资源和登录，再打包镜像与 SHA256 清单；受控手动流程复用同一产物。尚未宣称生产部署、Release 或标签发布。

PR #1 已合入 `dev`。合并后的需求查漏、已定位的边界问题及明日优先级见 [2026-10-16 升级计划](docs/upgrade-plan-2026-10-16.md)；既有 CI 通过不代表新发现的场景已经验证。
