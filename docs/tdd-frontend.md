# 前端测试与交接证据

Iris · 前端工程师，F1 attempt1 revision1；模型/强度按主控显式 Sol High 派发，Fast未独立核验。所有代码仅frontend与授权root忽略文件/本记录；未checkout/commit/push，遵守feat/*经PR至dev再main。

已批准seams：用户操作/REST/WS。Testing Library以用户可见页面、表单和按钮为入口；MSW只替代浏览器外HTTP边界，没有mock项目内部模块。Socket仅在网络边界替换。测试无数据库侧通道。

## 红 → 绿记录

| slice | 首次运行red | 实现/适配后的green |
|---|---|---|
| 并发401单飞cookie续期 | 22:16:27 ApiClient is not a constructor（拷贝版本无本项目公共接口） | 22:17:51 两请求都恢复、refresh只有一次、body为空、不存localStorage |
| 账号合法转移、viewer、403 | 22:18:49 Accounts模块缺失；22:19:55 中文按钮accessible name不匹配再次red | 22:20:36 3项pass；新增精确aria-label |
| 登录role | 22:21:29 Login模块缺失 | 22:21:40 pass，保留服务器role |
| Agent详情 | 22:21:40 AgentRun模块缺失 | 22:22:56 pass，blocked/rawResponse/BAD_JSON |
| 群时间线 | 22:22:56 GroupDetail模块缺失 | 22:24:28 pass，cursor较早加载、重复一行、成员角色、viewer |
| 序列预检与群导航 | 22:25:08 各页面模块缺失 | 22:26:34 pass，裸数组/跳转与422 stepIndex/key |
| WS认证/回放 | 22:27:21 realtime模块缺失 | 22:28:29 pass；随后主控纠正合法seq跳号契约 |
| WS合法跳号10,12 | 22:32:37 sinceSeq期望12实际0，暴露连续整数假设 | 22:32:51 pass；游标取合法已处理最大seq，<=cursor去重，后端保证单调提交推送 |
| 导入既有测试群 | 22:31:03 ImportGroup模块缺失 | 22:31:18 pass，gatewayGroupId+accountIds准确发送与反馈 |
| 序列来源确认/进度补充 | 22:38:12 user-event JSON输入测试装置解析失败 | 22:39:56 修正为表单change事件后pass，核验default/step:2、确认启动、sent/skipped完整进度 |

红阶段初建模块无法导入是事实，未伪造行为断言失败；WS跳号与中文按钮为独立实际行为失败。最终保留的测试检验用户/公共协议行为，不依赖内部函数。

## 当前验证与边界

- pnpm typecheck：通过；含Playwright用例类型。
- pnpm build：通过；Vite报告约1.15MB未压缩初始JS（AntD），无构建错误。
- pnpm test：22:39:56 最终 9 个文件、12 项全部通过；完整当前UI/REST/WS测试，非后端集成测试。
- pnpm test:e2e：2个真实栈用例已尝试，后端health未就绪导致C3失败；viewer登录未能完成。仍pending，不能报告完整E2E通过。
- 实际headless Edge渲染五页截图，HTTP仅展示fixture；截图只用于视觉验收。

真实E2E没有page.route API mock。C3按真实API准备fake账号/建群job、启用Agent，浏览器登录/打开群，用fake-only POST /admin/events {type:"message",data:{groupId:gatewayGroupId,msgId,senderPlatformUserId,text,sentAt}} 注入外部消息，然后打开run步骤；第二用例UI viewer隐藏与后端403。Playwright配置支持E2E_STACK_COMMAND先从root启动真正栈，再启Vite。集成CI/PG/三个服务的root启动脚本由Rowan负责；Iris先释放并发槽。入口与环境见frontend/README.md。

Vite dev进程如仍运行：本轮session 65903，127.0.0.1:5173，仅本机。测试和截图没有真实Telegram操作、LLM/API key或发布。后续只需在栈就绪后运行真实E2E与定向缺口修复。


## F2 与主控组合验收补充（2026-10-14 23:15）

- Iris 已完整使用 interface-design 与 design-taste-frontend 的适用规则完成 F2；设计审计见 ui-design.md。原始响应默认收起的可见性测试实际红→绿。受影响 7 个测试文件、10 项通过，typecheck/build 通过；F1 未受影响证据沿用。
- 1440/1280 五页以及 empty/loading/error/预检状态已实际浏览器渲染；截图仅视觉 fixture，存于忽略目录 .local/frontend-visual-v2。用户已明确接受 F2 方向（沿用这版方向，继续完成验收），视觉方向验收门已通过。
- 原有 8080 被其他 Docker 应用占用，保留该应用。Vite 新增可选 VITE_BACKEND_URL，Playwright health 使用 E2E_API_URL；默认端口不变。隔离 fake 栈运行 18080/18081/18082，使用独立 kapibala_e2e PostgreSQL 数据库。
- 首次真实栈运行 viewer 浏览器只读/直接写 API 403 通过；C3 暴露后台 invite POST 空 body 与网关 JSON 契约不一致。Orion 修为 {} 并通过真实 gateway HTTP 组合回归。
- E2E 现在按本次 job 返回 groupId 进入群，避免重跑时误选旧群。类型检查通过。
- 主控重建修复后的栈后，仅重跑受影响 C3：pnpm exec playwright test --grep C3，1 passed，8.1 s。实际 PostgreSQL、backend、fake gateway、scripted Agent 和 Edge；没有 page.route API mock，也没有真实 Telegram 发送。
- 两条真实栈用例现各自通过。当前 Vite session 42943、fake 栈 session 18220 由主控管理；此前 F1 session 65903 已停止。这些进程编号是本次交接信息，不是可重用启动参数。

## F3 会话隔离修复（2026-10-14）

Iris F3 revision1，仅 frontend/src/api/client.ts、client.test.ts 与本记录。按 TDD 和已批准 REST/auth seam 执行；CodeGraph 先查询当前 ApiClient、调用关系及测试。未修改 UI 布局、后端或其他员工文件，未运行全部浏览器/真实 Telegram。

- 会话代次独立于 access token 轮换：登录开始、退出开始和显式替换 session 使旧代次失效；成功续期只更新当前会话 token。
- 401 重试之前以及等待续期之后验证代次，旧 A 请求不会使用新 B 身份重放，也不会在迟到重试错误中清除 B。
- 续期成功或失败均先验证所属代次。旧续期 completion 不更新/清除新会话；finally 仅清理它自己仍持有的单飞 Promise，不能拆开 B 的正在进行的续期。
- 退出请求仍可在自身代次内续期以完成服务端撤销，完成后仅清空它所属的当前会话；其间若登录 B，不清除 B。

| 实际失败行为 | red | green |
|---|---|---|
| A 写操作在 logout/login B 后迟到 401 | 23:45:59 返回成功而非 SESSION_CHANGED，旧写被 B 身份重放 | 23:46:19 原始 HTTP 写身份仅 A；B 保留；同会话单飞仍通过 |
| pending refresh 在退出或登录 B 后恢复 A | 23:47:56 两个交错均错误返回旧续期结果 | 23:48:22 旧结果拒绝，退出保持 null、B 保持新身份 |
| 初次修复使 expired-access logout 未向服务端续期撤销 | 23:50:54 只有 expired-A 请求，缺少 renewed-A 撤销请求 | 23:51:02 保留过期 token 自动续期后退出，最终 session null |

补充定向回归：旧重试迟到 401、旧续期迟到失败、A 续期收尾与 B 单飞交错、A 退出完成与 B 登录交错。HTTP barrier 控制真实异步顺序，只模拟浏览器外 HTTP，无私有 collaborator mock 或检查 generation 字段。

验证：23:52:45 auth/WS/login 三个文件共 11 项通过；23:53:27 修正测试的 unknown 错误断言后 9 项 auth 再次通过。最终 pnpm build（含 tsc --noEmit）通过，3254 modules；原有大 chunk 警告不属于本次 auth 范围。范围内 git diff --check 通过。稳定代码待主控提交及 Sentry 定向独立复查。

用户已明确接受 F2 视觉方向：“沿用这版方向，继续完成验收”。视觉方向验收门已通过；这不代替剩余功能、恢复和真实资源验收。

## F4 HttpOnly Cookie 请求顺序修复（2026-10-15）

Iris F4 revision1，范围仅 ApiClient、受影响 auth 测试、新增 e2e/auth-cookies.spec.ts 与本记录。复用原请求入口、generation 与 refresh 单飞；没有改 UI、后端、布局或跨标签页机制。CodeGraph 查询了 auth 调用链后实施。

F3 的 generation 只能拒绝迟到 JSON，不能撤销浏览器已应用的 Set-Cookie。新增一个同 ApiClient 的 HTTP 队列，仅串行 login/refresh/logout 的实际 Axios 请求；队列发出请求前检查所属 generation，失败也释放队列。401 后的 refresh 和 logout 重试发生在 HTTP 锁之外，避免递归锁死。队列不因登录/退出而重置，确保旧在途 Cookie 先落地，再由新登录或退出覆盖；陈旧排队请求直接 SESSION_CHANGED，不发到服务器。

真实浏览器 TDD：自有临时端口 Node HTTP 服务加载实际 client.ts 的 TypeScript 编译结果和实际 Axios 浏览器包，用 headless Edge 原生 Cookie jar 验证 HttpOnly。不是 page.route/MSW Cookie 模拟，不使用业务数据库、真实账号或 API key。

| 行为 | red（修复前） | green（修复后） |
|---|---|---|
| 挂起 A refresh 后发起 B login，最后再次 refresh 仍 B | Cookie 期望 B1，实际 A2；JS generation 虽拒绝旧响应，Cookie 已跨会话 | 最终 memory B1/viewer、Cookie B1；再次 refresh 发 Cookie B1，结果 B2/viewer |
| 挂起 A refresh 后正常 logout | 退出后 Cookie 仍 A2 | Cookie 清空、session null；后续 refresh 401，登录 B 后续期仍 B |
| 挂起 A refresh 后 expired-access logout | 退出后 Cookie 仍 A2 | 自动续期后重试撤销，两次 logout，最终退出；后续 B 仍 B，无死锁 |
| 旧 logout 排队期间又发起 B login | 补充边界覆盖 | 旧 logout 返回 SESSION_CHANGED，服务器未收到 logout；队列继续完成 B 登录/续期 |

首次真实浏览器三条用例均为上述 Cookie 断言失败，未将未启动业务栈计作 red。修复后独立 runner 四条全部通过（3.8s）。局部 runner 位于 frontend/.local/playwright-auth.config.ts，仅移除 webServer 并选 auth-cookies；正式 Playwright testDir 自动包含新用例，CI 原配置继续跑业务栈与该独立夹具。测试在 finally 释放阻塞响应、关闭 HTTP 服务，Playwright 管理浏览器生命周期。

原 MSW 交错用例调整为先发起 B/退出 Promise，再释放挂起 A 响应，保留原身份、错误、单飞次数和新 session 断言，符合新的 HTTP 顺序。定向 Vitest auth/WS/login 共 11 项通过（00:16，9.49s）；真实 Cookie 浏览器 4 项通过。pnpm typecheck 与 pnpm build 均通过，3254 modules、构建 7.69s；原有 AntD 大 chunk 警告未扩展本轮范围。范围内 git diff --check 通过。未重跑全部业务 Go/浏览器或真实 Telegram。稳定快照仍需主控提交、独立复查和最终组合验收。


## F15 attempt1 revision1：U1–U3（2026-10-15）

Iris · 前端工程师｜序列与重连；主控最终验收。范围为 frontend 与所属 TDD/reuse 文档；保留已批准 F2 视觉方向。已读取 interface-design、design-taste-frontend、TDD 及 tests/mocking 指引；沿用已批准 browser UI/HTTP/WS seams，没有内部模块 mock。先查询 CodeGraph；后续工具报告 auto-sync disabled（索引锁竞争），编辑以当前磁盘文件为准，没有重建索引。

| 条目 | red 及最小修复 | green / 证据 |
|---|---|---|
| U1 延迟预检与确认快照 | `pnpm test -- src/pages/Sequences.test.tsx`，exit 1：vars、stepVars、sequence、group 四种变化均使旧响应重新出现“确认启动”。该命令的 `--` 导致运行全体 24 项，4 failed / 20 passed；后续改用精确 `pnpm exec vitest run`。请求携带 groupId/input/revision，同一快照用于预检及确认，编辑使旧响应失效；aria-busy 表示预检在途，使测试等待真实请求完成而非固定延迟。 | `pnpm exec vitest run src/pages/Sequences.test.tsx` exit 0，6 passed。原确认启动测试断言预检与启动发送相同 vars/stepVars/sequenceId。日志 `.local/repairs-20261015/iris/u1-red.log`、`u1-green.log`。 |
| U2 服务器当前运行与结果保留 | `pnpm exec vitest run src/pages/Sequences.test.tsx -t R1` exit 1：R1 完成后实际 WS seam 发出 R2 通知，页面仍找不到 r2。原位改为服务器 activeSequenceRunId 优先，最后运行结果绑定所属群；R2 完成后保留 R2。 | 最终 `pnpm exec vitest run src/pages/Sequences.test.tsx src/api/realtime.test.ts` exit 0，8 passed（7 UI + 1 WS）。日志 `u2-red.log`、`u2-green.log`、`final-unit.log`。 |
| U3 实际栈断线三秒证据 | 本条是证据缺口；新增真实栈测试首跑通过，未伪造生产故障 red，未改动 RealtimeClient。复用现有 Playwright/C3 HTTP 准备流程；routeWebSocket 只转发真实后端帧并强制关闭/阻断 WS，HTTP、PG、fake gateway 均是真实栈。断线窗口注入三条 fake 消息，通过公开 HTTP 确认持久化且页面尚未显示，再恢复实际 WS。 | 首跑 `pnpm exec playwright test e2e/platform.spec.ts --grep B4` exit 0，1 passed；恢复网络到全部可见 198.21 ms、认证后 133.41 ms。定向组合 `pnpm exec playwright test e2e/platform.spec.ts` exit 0，3 passed（C3、viewer、B4）；B4 恢复网络到全部可见 103.42 ms、认证后 55.97 ms，唯一 seq 19/20/21，三条各一行；第二次真实重连 sinceSeq=21，仍各一行且没有额外消息帧。严格 3000 ms，无虚拟计时或放宽阈值。日志 `u3-first.log`、`platform-green.log`；结构化证据 `u3-first-b4.json`、`platform-green-b4.json`。 |

最终 `pnpm typecheck` exit 0；`pnpm build` exit 0（含 tsc，3254 modules，7.11 s）；原有 AntD >500 kB chunk 提示仍在，不扩展本轮范围。日志 `typecheck.log`、`final-build.log`。最后仅增加 aria-busy 后重跑受影响 8 项与 build；不重复未受影响业务浏览器/后端整套测试。主控负责最终 CI 与组合验收。

隔离环境为主控创建的 `kapibala_e2e_today` / PostgreSQL 127.0.0.1:55433；backend/gateway/agent 18080/18081/18082，Vite 15173，Edge headless。Playwright webServer 现在使用 E2E_BASE_URL 的端口，避免 baseURL 指向隔离端口却静默复用 5173。实际启动 `E2E_DATABASE_URL=指定隔离DSN BACKEND_PORT=18080 GATEWAY_PORT=18081 AGENT_PORT=18082 node scripts/fake-stack.mjs`；浏览器环境 `E2E_API_URL=http://127.0.0.1:18080 E2E_GATEWAY_URL=http://127.0.0.1:18081 E2E_BASE_URL=http://127.0.0.1:15173 VITE_BACKEND_URL=http://127.0.0.1:18080 E2E_BROWSER_CHANNEL=msedge`。仅测试 fake 账号/消息，无真实 Telegram/LLM。用户 Docker demo 5173 未使用或改动。

自有 stack node PID 48780，服务 PID 35704/9564/45648；测试完成核对路径为 `.local/fake-stack/18080-18081-18082/bin/` 后全部清理，Playwright 自有 Vite 已退出。进程记录 `processes.json`、`cleanup.txt`；15173/18080/18081/18082 无剩余监听。首次 sandbox `pnpm` EPERM 属于环境限制，必要 escalated 后运行，不计行为 red。证据均在忽略目录 `.local/repairs-20261015/iris/`。本次无分支切换、commit/push、根脚本修改或发布；SUBMITTED 待主控验收。

## F15 attempt2 revision2：CI 序列状态测试超时（2026-10-15）

PR #3 CI `38673104348` frontend 首轮 24 passed / 1 failed；R1→WS R2 用例耗时 5810 ms，超过默认 5000 ms。失败是测试总时限，不是生产状态断言；原日志 `.local/repairs-20261015/ci-frontend-first.log`。冻结期间仅在 `.local/repairs-20261015/iris/a2/` 用副本分段调查：原配置聚焦原测试 2999 ms；带观测的副本 3065 ms，其中建立 R1 的 AntD role 查询和两次 userEvent 交互累计至 2566 ms，后续 WS 状态段约 0.5 s；未观察到等待一秒生产轮询。只替换 click 未改善（3280 ms），精确标签查询并保留 userEvent 为 2287 ms，标签查询配合直接 click 的候选为 1865 ms。分段、候选与限制见该目录 `findings.md` 及相应日志；本地计时不能冒充远端 CI 已通过。

主控在固定 `3c5192b` 独立复核完成后解除指定测试/TDD 冻结。仅该状态测试建立 R1 的两次按钮操作改为已有 aria-label 查询、明确等待预检按钮 enabled，并用 fireEvent.click；确认按钮仍断言 enabled。真实 UI→HTTP 启动与本地 runId、全部 WS 帧、R1/R2 完成和保留断言均保留。复用现有 fireEvent/waitFor，无新增模块或依赖；替换旧两次完整 pointer/focus 设置流程。独立预检确认测试继续使用 userEvent。未提高测试 timeout、未修改生产逻辑或 B4 三秒阈值，也未制造生产 red。

原配置验证：`pnpm exec vitest run src/pages/Sequences.test.tsx -t R1 --reporter verbose` exit 0，1 passed，1941 ms（`a2/r2-focused.log`）；`pnpm exec vitest run src/pages/Sequences.test.tsx src/api/realtime.test.ts` exit 0，8 passed，该状态用例 1700 ms（`a2/r2-affected.log`）；`pnpm typecheck` exit 0（`a2/r2-typecheck.log`）。日志均位于 `.local/repairs-20261015/iris/`。仅测试和文档改变，按授权不重复生产 build 或 B4/完整套件。远端 CI 与最终验收由主控完成；本次提交为 SUBMITTED。

## F16 attempt1 revision1：账号管理（2026-10-15）

Iris · 前端工程师。按 account-inspection-20261015.md revision3 冻结契约，沿用已批准 AntD 方向；只写 frontend 与所属文档。先 CodeGraph 定位 Accounts/AccountTableColumns/MainLayout/api 调用链；主控报告 auto-sync 锁竞争停止后，对编辑文件直接核对磁盘。已读 interface-design、TDD、tests/mocking、实现合同及规范 A1/UI；公共 seam 沿用批准的浏览器 UI、HTTP、WS。未操作真实账号、切分支、commit/push 或部署。

| 切片 | 实际 red | 实现及 green |
|---|---|---|
| 完整身份与搜索 | `pnpm exec vitest run src/pages/Accounts.test.tsx -t '账号管理完整'`：旧界面找不到“账号管理”，缺完整手机号与搜索，search-red.log | 账号表格原位扩列、手机号复制、用户名/手机号/ID 搜索、实际计数、分组/状态筛选、分页；搜索行为在 search-green.log 已绿（当次文件的旧合法按钮测试因 role 查询耗时失败，未把整次记绿）。 |
| 详情只读 | `... -t '详情打开'`：旧详情按钮不打开 Modal，details-red.log | 1080px Modal 三标签；仅读 account/cached groups/logs；viewer无写入口、离线提示连接、unknown独立角色。后续 management-unit.log 该项绿。 |
| 多文件配对 | `... -t '多账号上传'`：旧上传入口没有 Modal，import-red.log | 原生多选文件+AntD预览表；缺失/重复/不支持文件阻止提交，多组完整才启用；account-unit-final.log 配对项绿。真实 multipart 与逐项结果留真实浏览器，不以 jsdom/MSW 替代。 |
| 分组 CRUD | `... -t '管理员创建'`：旧界面无管理入口，groups-red.log | 创建/原位重命名/确认删除，保留账号提示与失败输入。首次测试删除后等待了已存在文字，定向修正为等待目标行移除；后来组合因无关六账号表格反复重绘触发15s超时，缩小该测试为无账号列表，保留全部CRUD和HTTP断言、未提高timeout，groups-isolated-green.log 4820ms。 |
| 补充关键边界 | 新增 HTTP/UI seam 覆盖，不伪造额外生产 red | 显式连接check=true、在线check=false、模拟来源未核验、同步失败保留缓存/creator角色、批移403保留选择/目标与重试；check-sync-green.log 定向项绿。 |

AntD NODE_ENV=test 的 useId 固定为 test-id，使多个 Select 与 Modal aria-labelledby 撞 ID；测试按 dialog 查询，再在其中断言内容，未以生产 hack 绕过。Axios/jsdom/MSW 对 multipart 文件体解析未完成，最初结果断言挂起；改为 unit 只验证配对，浏览器实际 HTTP 上传验证真实 FormData。ApiClient 无手写 Content-Type，当前 Axios 在标准浏览器对 FormData 清除 Content-Type，由浏览器生成 boundary；E2E 将断言实际请求头，不读凭据内容。

布局浏览器：`E2E_BASE_URL=http://127.0.0.1:15173 E2E_BROWSER_CHANNEL=msedge pnpm exec playwright test e2e/accounts-layout.spec.ts`，layout-green.log 1 passed / 5.4s。61条合成 HTTP 展示数据、1366/1920/2560×900，验证 account content 右边界、无页面横溢、内部滚动、分页留在视口内、切页和搜索回页1；截图 accounts-{1366,1920,2560}.png，已看图1366与2560。首次展示路由通配符误拦 Vite 的 src/api/client.ts 模块，范围改为 URL.pathname 以 /api/ 开头后绿；属于测试夹具，不是产品红。后续加分组/状态组合筛选断言，待组合浏览器一起验证。

`pnpm typecheck`（含新增 Node22 SQLite E2E）通过，typecheck-with-e2e.log；最终生产 `pnpm build` 通过，3258 modules，7.64s，build-stable.log；旧 AntD >500kB chunk 提示保留，不扩展优化范围。实际 Telegram/LLM、远端CI与主控最终验收不由这些本地结果证明。
