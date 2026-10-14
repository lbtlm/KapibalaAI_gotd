# Kapibala 前端操作台

React 18 + TypeScript + Vite。dev 监听 127.0.0.1:5173，/api 与 /ws 代理至 8080。access token 只保存在内存；refresh cookie 由浏览器自动发送，登录与续期读取服务器的 role。

```powershell
cd D:\web_projects\kapibala_ai\frontend
pnpm install --frozen-lockfile
pnpm dev
pnpm typecheck
pnpm test
pnpm build
```

预置账号 admin/admin 与 viewer/viewer 仅用于演示。viewer 不显示写操作，服务器仍须强制鉴权。群列表支持新建群和导入指定既有测试群，外部群主时不假设 creatorAccountId 有值。

## 真实栈 E2E

`pnpm test` 只证明 REST/WS 与组件合同。MSW 仅模拟浏览器之外的 HTTP，WS 测试仅替换网络 Socket；两者不能证明 PostgreSQL、网关和 Agent 的完整运行。

`pnpm test:e2e` 使用真实 API，没有 route/mock 拦截。C3 会连接两个 fake 账号、创建群、开启 Agent、向 fake 网关注入一条外部消息，再通过浏览器登录和进入群，核验 Agent 运行步骤。另一个用例验证 viewer 无写按钮且直调写 API 得到 403。仅用于隔离测试数据库，不能指向真实 Telegram 网关。

先准备隔离 PostgreSQL 并启动以下进程（root 栈脚本由集成负责人提供）：

- backend: DATABASE_URL 指向隔离库；先 `go run ./cmd/migrate`，再 `go run ./cmd/server`（8080）。
- gateway: 同一个隔离库、`GATEWAY_MODE=fake`、`GATEWAY_ADMIN_ENABLED=true`，`go run ./cmd/gateway`（8081）。
- Agent: `AGENT_MODE=scripted`，`go run ./cmd/agent`（8082）。

```powershell
cd D:\web_projects\kapibala_ai\frontend
pnpm exec playwright install chromium
pnpm test:e2e
```

可设置 `E2E_STACK_COMMAND` 为仓库根目录的栈启动命令；Playwright 会执行它并等待后端 health，然后启动 Vite。未设置则要求后端栈已经启动，不会以 mocks 代替。CI 应由 Rowan 的根脚本提供 PostgreSQL、迁移与三服务。配置支持 `E2E_API_URL`、`E2E_GATEWAY_URL`、`E2E_BASE_URL`。本机有 Edge 时可设置 `E2E_BROWSER_CHANNEL=msedge`，CI 使用安装好的 Chromium。

首次交接状态：前端构建与组件测试通过；真实栈 E2E 等待集成栈启动后运行。不会将未启动服务下的失败报告为验收通过。

