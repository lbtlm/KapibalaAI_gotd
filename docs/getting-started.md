# 启动与配置

[返回 README](../README.md) · [验收指南](acceptance-guide.md) · [运维与回滚](operations.md)

选择 Docker 或原生开发方式即可。两者都使用真实 Telegram 网关；新数据库账号为空。无账号启动不会登录 Telegram，已有账号的网关启动可能恢复先前连接意图。

## Docker Compose

需要 Docker Engine / Docker Desktop 和 Compose v2。Go、Node.js 和 pnpm 由镜像构建阶段使用，本机不必另装。当前待评审代码在 [PR #5](https://github.com/lbtlm/KapibalaAI_gotd/pull/5)，请先检出正确分支。

```powershell
Set-Location D:\web_projects\kapibala_ai
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
```

编辑 `.env`：将 `POSTGRES_PASSWORD` 改为仅含字母、数字、`-`、`_` 的随机密码；已有数据库继续使用原密码。默认 `CONSOLE_PORT=5173`，若被占用可改为其他本机端口。`.env` 已被 Git 忽略。

```powershell
docker compose -p kapibala-demo build
docker compose -p kapibala-demo up -d --no-build
Invoke-RestMethod http://127.0.0.1:5173/api/health
```

启动顺序为 PostgreSQL 健康 → 显式业务迁移与独立网关库初始化 → 服务 → 控制台。健康接口应返回 `ok:true` 和 `schemaVersion`；当前业务基线为 schema 12。打开 [控制台](http://127.0.0.1:5173)。若调整端口，上述 URL 同步调整。

| 服务 | 容器内端口 | 数据与暴露范围 |
| --- | --- | --- |
| frontend | 80 | 本机 `127.0.0.1:5173`；代理 `/api`、`/ws` |
| backend | 8080 | 业务库 `kapibala`；媒体在 `media-data` 卷 |
| gateway | 8081 | 独立库 `kapibala_telegram_gateway`；Session 与 SDK 状态持久化 |
| agent | 8082 | 独立进程，复用业务库保存 provider 会话 |
| postgres | 5432 | `postgres-data` 卷；不公开宿主端口 |

```powershell
docker compose -p kapibala-demo ps
docker compose -p kapibala-demo logs --tail 100 backend gateway agent
# 停止并保留数据。
docker compose -p kapibala-demo down
```

不要用 `down --volumes` 停止需要保留的环境。镜像先构建成功再切换服务；升级迁移前备份，不能把停止运行中的 gateway 当作构建前置步骤。原制品部署与回滚使用 [运维说明](operations.md) 中的流程。

## 账号与真实 AI

演示网页用户为 `admin/admin`、`viewer/viewer`。它们是操作台用户，不是 Telegram 服务账号；仅适用于本地受控演示。

上传同名 `.session` 与 `.json`，确认资料后点击“连接并检测”。上传不自动连接，检测成功才是 Telegram 身份验证。账号同步群不会自动创建管理工作区或开启 Agent。已有 manifest 安装需要迁移时，使用 [Gateway 离线采用流程](../gateway/README.md)，不要把旧模拟数据当作真实凭据。

默认 `AGENT_MODE=scripted` 用于无密钥的固定协议回复。真实模型在 `.env` 中配置：

```dotenv
AGENT_MODE=openai
OPENAI_API_STYLE=chat_completions
OPENAI_BASE_URL=https://api.deepseek.com
OPENAI_MODEL=deepseek-flash
OPENAI_API_KEY=替换为自己的密钥
```

```powershell
docker compose -p kapibala-demo up -d --no-deps agent
```

配置值只转发到 provider，前端和网关不接触模型密钥。模型和服务地址以自己可用的配置为准；上表是项目当前默认配置。Responses 适配见 [Backend / Agent 说明](../backend/README.md)，真实 Responses 端点尚未实测。

群内自动回复还需要：管理员在线且属于该群、Agent 开关开启、群未暂停或结束、新消息来自外部成员。剧本、自有账号手动发送和 Agent 回复不会再触发 Agent。AI 回复预演和 AI 剧本生成会调用真实模型，但预览本身不会发送群消息。

## 原生开发

需要 **Go 1.24、Node.js 22.14.0、pnpm 10.18.1、PostgreSQL 16**。先准备 PostgreSQL 用户和两个独立专用数据库；下面的主机、端口和用户是示例，需匹配自己的安装。创建数据库需要相应权限。

<details>
<summary>Windows PowerShell</summary>

```powershell
Set-Location D:\web_projects\kapibala_ai
# 只创建一次；用户 kapibala 需事先存在。
createdb -h 127.0.0.1 -p 55432 -U kapibala kapibala_e2e
createdb -h 127.0.0.1 -p 55432 -U kapibala kapibala_gateway_e2e
$env:E2E_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_e2e?sslmode=disable'
$env:E2E_GATEWAY_DATABASE_URL='postgres://kapibala@127.0.0.1:55432/kapibala_gateway_e2e?sslmode=disable'
node scripts/local-stack.mjs
```

另开一个终端启动前端：

```powershell
Set-Location D:\web_projects\kapibala_ai\frontend
pnpm install --frozen-lockfile
pnpm dev
```

</details>

<details>
<summary>Linux / macOS</summary>

在仓库根目录执行；若数据库要求密码，替换 DSN 中的示例密码。

```bash
createdb -h 127.0.0.1 -p 5432 -U kapibala kapibala_e2e
createdb -h 127.0.0.1 -p 5432 -U kapibala kapibala_gateway_e2e
E2E_DATABASE_URL='postgres://kapibala:your-password@127.0.0.1:5432/kapibala_e2e?sslmode=disable' \
E2E_GATEWAY_DATABASE_URL='postgres://kapibala:your-password@127.0.0.1:5432/kapibala_gateway_e2e?sslmode=disable' \
node scripts/local-stack.mjs
```

另开终端：

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

</details>

`local-stack.mjs` 编译并启动 migrate / server / gateway / agent，等待健康检查。它**不读取 `.env`，并固定启动 scripted provider**；要测试真实模型，用 Compose，或按组件 README 分别启动服务并配置 provider 环境变量。

脚本终端 Ctrl+C 仅关闭它启动的进程，前端终端单独 Ctrl+C；数据库保留。端口占用会拒绝启动，不会抢占已有服务。

## 配置索引

| 配置 | 使用者 | 含义 |
| --- | --- | --- |
| `DATABASE_URL` | 原生 migrate / server / agent / gateway | Gateway 必须使用独立数据库；其余三个进程使用业务库 |
| `PORT` / `BIND_HOST` | 三个 Go 服务 | server 8080、gateway 8081、agent 8082；原生绑定 127.0.0.1，容器绑定 0.0.0.0 |
| `GATEWAY_URL` / `AGENT_URL` | backend | 默认本机 8081 / 8082；Compose 使用内部服务名 |
| `BACKEND_PORT` / `GATEWAY_PORT` / `AGENT_PORT` | local-stack | 覆盖脚本启动的三个端口 |
| `VITE_BACKEND_URL` | Vite | 后端不在 8080 时指定代理目标；浏览器不直连网关 |
| `MEDIA_DIR` | backend | 原生默认 `./media`；建议指向受控本地路径，Docker 为 `/app/media` |
| `MEDIA_RETENTION_DAYS` | backend | 默认 30；运行中 Agent 所在群的媒体受保护 |
| `CANDIDATE_SHA` | Compose | 本地默认 local；原制品流程使用不可变 SHA 标签，不要填入本机不存在的标签后使用 `--no-build` |

完整起点为 [`.env.example`](../.env.example)、[Backend README](../backend/README.md)、[Gateway README](../gateway/README.md)。数据库测试与浏览器验收使用另外的隔离库，不能指向实际账号库；执行入口见 [验收指南](acceptance-guide.md)。
