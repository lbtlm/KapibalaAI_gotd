# 启动与配置

[返回 README](../README.md) · [验收指南](acceptance-guide.md) · [运维与回滚](operations.md)

选择 Docker 或原生开发方式即可。两者都使用真实 Telegram 网关；新数据库账号为空。无账号启动不会登录 Telegram，已有账号的网关启动可能恢复先前连接意图。

## Docker Compose

需要 Docker Engine / Docker Desktop（Linux 容器）和 Compose v2。Go、Node.js、pnpm 和 PostgreSQL 都由容器提供，本机不必另装。网络需要能拉取镜像、下载构建依赖，以及访问 Telegram 和选定模型 API。新机器先克隆 `dev` 分支，或检出指定交付提交；[PR #5](https://github.com/lbtlm/KapibalaAI_gotd/pull/5) 已合入 `dev`。

### 配置文件如何读取

统一使用仓库根目录 `.env`，与 `compose.yaml` 放在一起；所有命令都在仓库根目录执行。路径按自己的克隆位置替换：

```powershell
Set-Location D:\web_projects\kapibala_ai
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
```

这一步只创建不存在的文件，不能覆盖已有数据库对应的配置。编辑 `.env` 后再继续：

| 项目 | 配置要求 |
| --- | --- |
| `POSTGRES_PASSWORD` | 新库填写随机密码，仅含字母、数字、`-`、`_`，以便用于数据库连接 URL；已有库保持原密码 |
| `AGENT_MODE` | 模板显式设置 `openai`，需要真实密钥；无密钥的固定回复演示才使用 `scripted` |
| `OPENAI_API_KEY` | 真实 AI 必填，只填密钥值；已有 `.local` 私有配置中的值也需填入此项 |
| `OPENAI_BASE_URL` / `OPENAI_MODEL` / `OPENAI_API_STYLE` | 使用密钥实际支持的地址、模型与协议，具体见下方真实 AI 说明 |
| `CANDIDATE_SHA` | 新机器本地构建保持 `local`；不要填没有构建或载入过的 SHA 标签后使用 `--no-build` |
| `CONSOLE_PORT` / `MEDIA_RETENTION_DAYS` | 默认 `5173` / `30`；端口冲突时修改端口并同步修改访问 URL |

`.env.example` 仅为模板；`.local` 中的 JSON、纯密钥文本、其他 env 文件不会被自动读取。`OPENAI_API_KEY` 不能填写文件路径，也没有 `OPENAI_API_KEY_FILE` 自动读取机制。无需改代码或把私有文件打进镜像。

Compose 从根目录 `.env` 插值配置，再将 `compose.yaml` 声明的环境变量传入对应容器；当前终端的同名环境变量优先于 `.env`，若修改看似未生效，应检查并清除旧终端覆盖。规则见 [Docker 环境变量文档](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)。`.env` 已在 Git 和镜像构建中忽略，不把真实配置写进 `.env.example`。

### 构建与启动

```powershell
Set-Location D:\web_projects\kapibala_ai
docker compose -p kapibala-demo config --quiet
docker compose -p kapibala-demo build
# 构建成功后才继续；不需要先停止正在运行的服务。
docker compose -p kapibala-demo up -d --no-build
docker compose -p kapibala-demo ps -a
Invoke-RestMethod http://127.0.0.1:5173/api/health
```

启动顺序为 PostgreSQL 健康 → 业务迁移与独立网关库初始化 → 服务 → 控制台；无需手动建库或单独运行迁移。`config --quiet` 只检查 Compose 配置，不验证 API key；缺少密钥时 `openai` Agent 会拒绝启动。启动可能需要数秒，健康接口应返回 `ok:true` 和 `schemaVersion:12`，然后打开 [控制台](http://127.0.0.1:5173)。若调整端口，上述 URL 同步调整。

| 服务 | 容器内端口 | 数据与暴露范围 |
| --- | --- | --- |
| frontend | 80 | 本机 `127.0.0.1:5173`；代理 `/api`、`/ws` |
| backend | 8080 | 业务库 `kapibala`；媒体在 `media-data` 卷 |
| gateway | 8081 | 独立库 `kapibala_telegram_gateway`；Session 与 SDK 状态持久化 |
| agent | 8082 | 独立进程，复用业务库保存 provider 会话 |
| postgres | 5432 | `postgres-data` 卷；不公开宿主端口 |

`ps -a` 中这五个服务应处于运行状态，postgres 应为 healthy。`migrate` 和 `gateway-db` 为一次性任务，`Exited (0)` 是成功；非零退出时先查它们的日志。业务 `/api/health` 正常不代表模型密钥可用，真实模型要通过页面预演验证。

```powershell
Set-Location D:\web_projects\kapibala_ai
docker compose -p kapibala-demo logs --tail 100 backend gateway agent
# 停止并保留数据。
docker compose -p kapibala-demo down
```

不要用 `down --volumes` 停止需要保留的环境。项目名 `kapibala-demo` 决定卷名称，已有环境不要随意换名。新机器仅复制配置会创建空数据库，不会带入原账号、群、消息、Agent 上下文和媒体；迁移这些数据需要恢复数据库与媒体卷，按 [运维说明](operations.md) 执行。镜像先构建成功再切换服务；升级迁移前备份，不能把停止运行中的 gateway 当作构建前置步骤。

## 账号与真实 AI

演示网页用户为 `admin/admin`、`viewer/viewer`。它们是操作台用户，不是 Telegram 服务账号；仅适用于本地受控演示。

上传同名 `.session` 与 `.json`，确认资料后点击“连接并检测”。上传不自动连接，检测成功才是 Telegram 身份验证。账号同步群不会自动创建管理工作区或开启 Agent。已有 manifest 安装需要迁移时，使用 [Gateway 离线采用流程](../gateway/README.md)，不要把旧模拟数据当作真实凭据。

根目录 `.env` 中的真实 AI 配置如下。`openai` 是 OpenAI-compatible 适配模式名，DeepSeek 同样使用它；不是必须购买 OpenAI 服务：

```dotenv
AGENT_MODE=openai
OPENAI_API_STYLE=chat_completions
OPENAI_BASE_URL=https://api.deepseek.com
OPENAI_MODEL=deepseek-flash
OPENAI_API_KEY=替换为自己的密钥
```

`OPENAI_BASE_URL` 是 API 基础地址，代码会追加 `/chat/completions`（或 Responses 模式的 `/responses`），不要填写完整请求端点。模型名必须是该密钥可用的模型，不保证所有兼容服务都支持模板中的默认模型。值含 `$` 或 `#` 时用单引号包住密钥，避免 dotenv 插值或注释解析。

配置值只转发到 provider，前端和网关不接触模型密钥。模板现在显式使用 `openai`：未填 key 会拒绝启动，填错或额度不足则在调用时失败，不会偷偷改成固定回复。仅想无密钥启动页面时可主动设置 `AGENT_MODE=scripted`，但固定文案不是 AI；AI 回复预演和 AI 剧本生成仍需要有效密钥。省略 `AGENT_MODE` 时，Compose 为兼容旧配置默认使用 scripted。Responses 适配见 [Backend / Agent 说明](../backend/README.md)，真实 Responses 端点尚未实测。

群内自动回复按以下顺序启用：

1. 上传有效 Session + JSON，主动连接并检测成功。
2. 创建或导入群，绑定在线、已入群且具有发送权限的管理员；执行身份不会回退到其他账号。
3. 在群管理开启“Agent 自动响应”，确保群未暂停、未结束；暂停后需明确恢复。
4. 由外部成员发送一条新消息。只有外部新消息触发；剧本、自有账号手动消息和 Agent 回复不触发。恢复不会回放暂停期间的消息，剧本也需重新预检确认。

同群同时最多一个 run，run 完成后群 Agent 继续待命。先用“AI 回复预演”确认真实模型调用，再检查群内 Agent 步骤、审计和实际投递状态；预演不发群消息，也不能代替群内投递验证。

### 修改配置后如何生效

只修改 `AGENT_MODE` 或 `OPENAI_*` 时，运行以下命令重新创建 agent 容器。适用于整套服务已经启动的环境，不会连带重启 gateway：

```powershell
Set-Location D:\web_projects\kapibala_ai
docker compose -p kapibala-demo config --quiet
docker compose -p kapibala-demo up -d --no-build --no-deps agent
docker compose -p kapibala-demo logs --tail 100 agent
```

**不要用 `docker compose restart` 加载新配置**，它只重启旧容器，不更新环境变量；[Docker 官方说明](https://docs.docker.com/reference/cli/docker/compose/restart/)明确了这个区别。`up` 会检测配置变化并重建对应容器。无需为了换 key 重新编译镜像。

| 改动 | 根目录执行的命令 / 处理方式 |
| --- | --- |
| `CONSOLE_PORT` | `docker compose -p kapibala-demo up -d --no-build --no-deps frontend`，之后访问新端口 |
| `MEDIA_RETENTION_DAYS` | `docker compose -p kapibala-demo up -d --no-build --no-deps backend` |
| 更新应用代码 | 先 `docker compose -p kapibala-demo build`，成功后 `docker compose -p kapibala-demo up -d --no-build`，由 Compose 执行所需迁移 |
| 服务停止后重新启动 | `docker compose -p kapibala-demo up -d --no-build`；没有本地镜像时先构建 |
| `POSTGRES_PASSWORD` | 已有数据库不会因改 env 自动改密码；开发验证保留原值，密码轮换需同时修改数据库角色与连接配置 |

遇到 `OPENAI_API_KEY is required`，检查根目录 `.env` 的值是否为空；`PROVIDER_ERROR` / `AUDIT_UNAVAILABLE` 则检查模型地址、协议、模型名、密钥权限和容器网络。保留原文件，不要重复复制模板覆盖已填写配置。排查时使用 `config --quiet` 校验结构，避免将含真实密钥的完整 `config` 输出发到日志或截图。

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
