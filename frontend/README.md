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

预置 Web 登录 admin/admin 与 viewer/viewer 仅用于本地验收。新环境没有 Telegram 账号；管理员上传真实账号的同名 Telethon SQLite `.session` 与 `.json` 后，再显式连接并检测。打开详情只读取已有资料；同步所在群需要账号在线，不会自动托管群组或启用 AI。viewer 只读取，服务器强制鉴权。

## 验证边界

`pnpm test` 保留认证客户端与登录、通用 WS 游标、Agent 展示测试，以及真实核验来源的类型契约。认证与 Agent 使用它们自己的公共 HTTP 测试边界；通用 WS 测试仅覆盖认证和游标，不构造 Telegram 事件。账号、群与网关的 HTTP/route/RPC 测试替身、合成 Session 成功路径和相关浏览器用例已删除。删除覆盖不能计作通过。

`e2e/empty-system.spec.ts` 使用实际前后端及真实空数据库，不拦截业务响应。覆盖登录、实际空账号数组、真实文件上传引导、三个桌面宽度，以及 viewer 写入得到 403。不上传文件、不创建账号、不连接 Telegram。`e2e/auth-cookies.spec.ts` 独立验证无关的浏览器 HttpOnly 认证边界。

## 真实空系统浏览器验收

先准备两个独立、专用于验收的空 PostgreSQL 数据库，分别设置 `E2E_DATABASE_URL` 和 `E2E_GATEWAY_DATABASE_URL`。仓库根目录的 `scripts/local-stack.mjs` 负责真实服务启动与迁移；不得指向已有账号或人工验收环境。

```powershell
cd D:\web_projects\kapibala_ai\frontend
# 先在本终端设置上述两个隔离数据库 URL。
$env:E2E_STACK_COMMAND = 'node scripts/local-stack.mjs'
$env:E2E_BASE_URL = 'http://127.0.0.1:15173'
$env:E2E_API_URL = 'http://127.0.0.1:18080'
pnpm exec playwright install chromium
pnpm exec playwright test e2e/empty-system.spec.ts
```

Playwright 等待后端 health，再由 Vite 将 `/api`、`/ws` 代理到 `E2E_API_URL`。未设置启动命令时，要求真实后端已经启动。浏览器测试默认使用 15173/18080；不使用正在人工验收的 5173。本机可设置 `E2E_BROWSER_CHANNEL=msedge`，CI 使用 Chromium。截图与附加证据写入 Playwright 标准 artifact 目录，不依赖预建的本地文件夹。

空系统通过不证明真实 Session 导入、连接、资料查询、群同步、消息发送或 Telegram 故障恢复。真实账号验收由主控使用指定资源单独授权；只读 UI 验收仅记录脱敏布尔、计数和样式，不将真实身份、群资料或凭据写入 Git/CI artifact。当前没有替身覆盖的限流、同步失败缓存、迟到结果和断线窗口等实际故障，必须如实标为未验证。
