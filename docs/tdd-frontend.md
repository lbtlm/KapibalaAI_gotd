# 前端测试与交接证据

当前验收策略以文末 I19 和账号契约 Revision 5 为准。下列 F1–I18 记录是历史证据；其中账号/群网关替身、合成 Session、模拟栈、展示 route 和 C3/B4 注入测试均已退役，不能作为当前真实平台行为的通过证据或运行指南。

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

### 最终验证与审查后收敛

最终受影响组合 `pnpm exec vitest run src/pages/Accounts.test.tsx src/api/realtime.test.ts --reporter verbose`，2 files / 10 passed，35.97s；分组CRUD 7274ms。证据 affected-stable.log。主控 Sentry 首轮未发现前端产品阻塞；随后唯一新增生产改动是按主控指示为 upgrade_disconnect 日志动作加“升级后断开连接”标签/筛选。`pnpm build`（含 tsc）再次通过，3258 modules、6.22s，build-review-mapping.log；未为静态映射增加镜像单测，未重复已通过unit。

真实浏览器新账号用例最终 `pnpm exec playwright test e2e/accounts.spec.ts --grep '配对导入' --reporter=list --output=../.local/inspection-20261015/iris/account-capture-results`，1 passed / 16.0s（用例11.2s），account-capture.log。真实 PG、backend、持久 fake gateway、scripted Agent；Node22内置SQLite临时生成两个独立合成授权与一份无效Session，测试finally删除文件。实际浏览器multipart含自动boundary，2成功/1 INVALID_SESSION逐结果；完整手机号/分组→明确连接并检测→模拟来源且未核验手机号→只读详情和导入/连接/检测日志→完整creator群快照→重复导入明确错误→批移未分组→删组仍保留账号，全部通过；读取详情/日志未产生连接或同步POST，群同步前后托管群清单及AI开关一致。

新layout 1 passed / 1.7s，新增viewer 1 passed / 1.5s，复用 accounts-focused.log 对这两项的通过证据。layout包含三桌面宽度、内滚动、分页、搜索回页1、分组30条与叠加在线10条，统计仍61。最初集中测试停于隐藏AntD虚拟option，改点可见option-content后绿；账号流程后续定位假设分别是zhCN关闭按钮并非英文Close、has内层错误包含modal根。均经trace/页面快照确认后只修测试定位，未改变产品/timeout；已知等待主动停止。最终四截图使用 animations:'disabled'，避免过渡帧误作视觉证据。

既有真实栈 `pnpm exec playwright test e2e/platform.spec.ts --reporter=list --output=../.local/inspection-20261015/iris/platform-results`，3 passed / 18.4s（C3/viewer/B4），platform-stable.log。B4从恢复184.971ms、认证后100.555ms，三条各一行，seq16/17/18，第二重连cursor18保持唯一；没有重跑已通过platform。

本轮服务端口18080/18081/18082，Vite15173，DSN由忽略的test-environment.json提供，未使用5173 demo。截图已逐图看过：.local/inspection-20261015/iris/actual-upload.png、actual-details.png、actual-groups.png、actual-logs.png；都是隔离真实栈加合成数据，不是Telegram真实账号/真实LLM证据。旧fake-stack session72771保留供主控审查修复后的定向健康/限流/升级联调，主控确认后清理；新二进制修复的联调尚不由上述旧服务结果证明。源码和文档完成后冻结，SUBMITTED待主控最终验收。

## I18：日志结果 Tag 与所属群空态（2026-10-15）

Iris · 前端工程师，沿用 Sol High，Fast未暴露。范围仅 AccountDetails、StatusTag、受影响UI测试及本轮TDD/reuse记录；共享仓库不回退他人修改，无分支/提交/push、递归委派或5173账号写请求。先经CodeGraph定位现有调用链，按当前磁盘和账号契约revision4实施。API、缓存更新和连接权限均不变。

根因1：日志单独使用 AntD6 默认 filled Tag 的 success/error 状态色，现有深色 colorSuccess/colorError 经主题派生的背景偏灰；没有全局Tag覆盖或opacity问题。原生产构建成功文字/背景 rgb(45,97,80)/rgb(151,161,156)，对比度2.687；失败4.455，opacity均1。修复复用现有 StatusTag Positive/Danger 的明确浅底/深字，仅增加 success→成功映射；不改主题或CSS、不保留旧结果渲染路径。新构建成功对比度6.400、失败6.266，两者分别绿/红且文字完整。

根因2：所在群空表无条件使用“尚无成功同步的群组快照”，忽略 lastSyncedAt。现按该字段区分“尚未同步所在群”和“最近一次同步未发现已加入的群组”；首次读取失败单独提示重试，加载时不提前显示空态。同步失败继续保留既有数据/时间，并明确“本次同步失败，仍显示上次同步结果”。未加入虚构身份或群资料。

| 验证 | red | green |
|---|---|---|
| 成功空结果/失败保留 | `pnpm exec vitest run src/components/AccountDetails.test.tsx`，empty-red.log：成功空结果仍旧文案，断言失败 | 最小空态修复后empty-green.log 1 passed；补未同步/读取失败覆盖，最终与Accounts调用链合跑12 passed。 |
| 结果Tag可读性 | 原构建+Edge公开UI颜色测量，colors-red.log：成功对比度2.687，未达4.5；log-colors-red.json、log-results-red.png保留实际原值和图 | 复用状态组件后colors-green.log 1 passed/4.1s；log-colors.json实测成功6.400/失败6.266，log-results.png。 |

最终受影响命令 `pnpm exec vitest run src/components/AccountDetails.test.tsx src/pages/Accounts.test.tsx`：2 files / 12 passed，39.89s（affected-unit.log）。原缓存非空/失败保留、权限、显式连接意图等Accounts覆盖全部保留。`pnpm build`通过，3258 modules，6.13s（build.log，含tsc）；原大chunk警告未扩展优化。新增三种视觉用例后仅补`pnpm typecheck`通过（typecheck-final.log），不重复生产build。

基于该新production dist，以临时15173 vite preview运行Edge，所有/api/仅为公共HTTP展示夹具，身份字段null、群items始终[]，不是实际账号/Telegram证据。`pnpm exec playwright test --config ../.local/inspection-20261015/iris-i18/playwright.config.mjs --grep '所在群'`：3 passed/8.9s（empty-visual.log），分别检查未同步/成功空结果/失败保留上次成功空结果，并比较失败前后同步时间不变。截图groups-not-synced.png、groups-synced-empty.png、groups-failed-retained.png及日志结果图已逐图检查，无重叠/过渡帧。临时runner首次ESM扩展名配置错误修正为.mjs后才获得上述真实行为red，不把工具启动失败计作产品red。

证据全部在忽略目录 .local/inspection-20261015/iris-i18/；没有真实姓名、手机号或群名加入Git，未触5173真实账号检查/连接/同步。现有实际栈accounts.spec只更新被替代的空态文案断言，未重复整套上传/C3/B4。主控部署后再做5173只读终验，当前本地结果不冒充已部署或真实链路验收。

I18最终artifact收敛：主控指出tracked浏览器测试不应依赖预先存在的ignored输出目录。已全部改用testInfo.outputPath，颜色JSON通过testInfo.attach进入标准Playwright artifact；tracked测试不依赖本地runner/人工目录。本地仅把结果复制回上述证据目录供查看。仅定向复跑4项反馈用例，feedback-final.log 4 passed / 6.9s；typecheck-artifacts.log通过。没有重复12项unit或生产build；生产代码仍与已通过的新dist一致。全部完成后停止写入，SUBMITTED等待主控部署后只读终验。

I18真实响应语义补充：主控确认Telegram读取成功但username确实为空。列表/详情原位增加verificationSource==='telegram'条件，分别显示“未设置用户名”/“未设置”；尚未读取或模拟来源仍“未获取”，不改API或填造username。先新增一个公共UI用例验证列表→详情，username-red.log缺少“未设置用户名”而失败；最小两处文案修改后username-green.log 1 passed（其余9项未重复，718ms），username-typecheck.log及username-build.log通过（3258 modules、6.43s）。已有12项/4项反馈用例不重复。此前0ee9290部署版5173只读验收的脱敏结果在iris-i18/final-5173.json：身份可见性布尔通过、群数量/角色符合主控已验事实、成功Tag对比度6.400、业务POST与console/pageerror均0；本次自建Web会话退出后refresh401确认撤销，没有真实PII截图。用户名文案的新部署版仍待主控提交/重建后终验，源码冻结SUBMITTED。

## I19：移除账号与网关测试替身（2026-10-15）

Iris · 前端工程师，Sol High，Fast未暴露；按主控冻结的账号Revision 5执行。仅修改frontend与本人组件记录，不操作5173或真实账号，不Git/部署/递归委派。CodeGraph先定位后按磁盘确认；继续使用interface-design与TDD方法，不改现有视觉方向。

先新增公开Account核验来源类型断言：`pnpm --dir frontend typecheck`在原`telegram | simulated | null`类型上报TS2344，types-red.log为实际red；首次沙箱EPERM是工具环境失败，不计产品red。最小改为`telegram | null`后同命令通过，types-green.log。类型断言依赖tsc，Vitest运行结果本身不证明静态类型正确。

移除10份账号/群/消息替身测试文件：Accounts、AccountDetails、ImportGroup、Groups、GroupDetail、Sequences，以及accounts、accounts-layout、account-feedback、platform浏览器测试。Sequences现有全部用例都依赖群HTTP或WS运行，没有独立纯值/UI用例可以原样保留。删除包含C3/B4注入、synthetic SQLite Session、模拟导入检测、61行布局和同步失败缓存的旧覆盖，不计作通过；没有新造替身补回数量。

保留认证/Agent测试与依赖。ApiClient认证并发用例原位改成中性受保护资源与普通value，不再借用账号数组、发送路径或Telegram消息数据；通用RealtimeClient认证/游标用例改用notification普通值，保留鉴权前拒流、重连cursor、重复seq拒绝断言。这些属于通用协议测试，不能证明Telegram消息链路。测试环境matchMedia/ResizeObserver等jsdom shims不构成账号/网关响应，保留。

`pnpm --dir frontend test`：5 files / 13 passed，18.02s（unit-green.log）；仅保留的9项认证客户端、1项登录、1项Agent、1项通用WS和1项类型测试。`pnpm --dir frontend typecheck`通过；`pnpm --dir frontend build`通过，3258 modules / 9.33s（build.log），原大chunk提示未扩展处理。旧账号业务测试不在这13项内。

新增empty-system.spec.ts使用真实HTTP/页面，无route/mock、无Session、无账号/群构造。只在实际GET账号为空后检查登录、上传真实文件引导、1366/1920/2560全宽；viewer隐藏写入口，实际无文件导入请求403。截图使用testInfo.outputPath标准目录。Playwright默认15173/18080，Vite明确代理到E2E_API_URL；local-stack由主控管理，不触5173。真实空栈定向联测与视觉证据待主控服务就绪，未执行不计通过。

限制：本次没有验证真实上传、连接、资料获取、同步/故障保留、429、迟到响应、实际断线恢复、大列表滚动、C3、B4或真实LLM；历史证据与新空系统通过不能替代这些验收。指定账号只读页面验收待主控部署和授权，采用脱敏布尔/计数/样式并撤销自己的Web登录会话。所有本轮本地命令证据位于`.local/inspection-20261015/iris-i19/`。

I19真实空栈定向验收补充：主控提供全新两独立PG库，通过Playwright的E2E_STACK_COMMAND启动local-stack（18080/18081/18082，网关telegram、schema7；无账号/Session），Vite15173。只执行`pnpm --dir frontend exec playwright test e2e/empty-system.spec.ts --reporter=list --output=../.local/inspection-20261015/iris-i19/empty-system-final-results`，2 passed / 11.9s（empty-system-final.log）。实际GET accounts=[]、真实上传指引、1366/1920/2560全宽、viewer无写按钮/实际POST import403与退出均通过，管理员页pageerror断言为空。没有请求导入/连接/同步真实账号，也没有重跑其它测试。

首轮已通过三个布局宽度，在退出按钮exact accessible-name定位等待；主动停止后只改两处退出selector允许既有图标名称，未改产品或timeout。最终截图位于empty-system-final-results/empty-system-真实空系统登录后引导上传真实账号，桌面账号工作区填满可用宽度/empty-accounts-{1366,1920,2560}.png；看图1366/2560无溢出、重叠或右侧限宽空区。截图是实际空系统，不含虚构身份或真实PII。Playwright结束后准确核对自有可执行路径和监听端口，remainingOwnedProcesses=0、remainingListeners=0，记录empty-final-summary.json，附实际本次二进制hash；未接触Docker5173。该视觉来自源码Vite，最终生产镜像视觉由主控另验。

S19结束后按主控授权只补两条既有日志映射：legacy_verification_reset“历史检测结果已重置”、legacy_cached_groups_unverified“历史群缓存待核验”；`pnpm --dir frontend typecheck`再次通过（types-final-mapping.log），没有重复unit/build/E2E。映射后的productionbuild与5173终验由主控负责。源码/测试/本人记录冻结，SUBMITTED。

## I20 v1：业务401误重放与同步后的账号状态（2026-10-15）

Iris · 前端工程师，沿用Sol High，Fast未暴露；唯一修改frontend和本TDD记录，无Git、部署、递归委派、真实账号/凭据读取或5173业务操作。先读diagnosing-bugs并CodeGraph定位，再按磁盘确认调用链；本轮不改样式或恢复已删除的账号/网关替身。

公开red-capable反馈：先把ApiClient三处相同401判断原样提到同文件isWebAuthFailure，保持旧行为；`pnpm --dir frontend exec vitest run src/api/auth-failure.test.ts --reporter=verbose`的纯status/code输入回归实际2failed/3passed，401 SESSION_EXPIRED被判断为Web登录失效（auth-red.log）。这不是终态账号或网关响应替身。pnpm在失败测试后另输出递归命令提示，真实red依据是Vitest已执行并给出的断言差异，不是该包装提示。

red后向主控报告三个可证伪假设：1业务401走refresh/replay清session分支；2sync失败只刷新日志使列表/详情留旧；3WS/focus/详情旧对象导致广泛不同步。主控真实只读日志显示SESSION_EXPIRED后约20ms又check ILLEGAL_TRANSITION；Orion确认Web auth401只用UNAUTHORIZED，终态SESSION_EXPIRED为401、ACCOUNT_SUSPENDED为403。该证据与原ApiClient分支吻合。第三假设没有额外证据：详情保存ID并由query取当前对象；check/transition已有onSettled刷新；WS账号事件刷新且认证重连全invalidate；query staleTime0/default可见恢复机制原已存在。没有新增轮询、额外状态层或为正常手动disconnect/connect补丁。

最小修复：isWebAuthFailure只接受401+UNAUTHORIZED，三处共用；业务401保持原status/code/details返回，不续期、不重放、不清Web登录。保留原session-generation/单飞续期/cookie队列，未知业务401也不误当Web认证。既有中性protected-resource认证transport新增公开请求回归：SESSION_EXPIRED请求次数1、refresh0、原码不变且原Web登录仍在；没有账号ID/DTO、Session、Telegram RPC或account/gateway HTTP替身。

第二个red：把sync既有只刷日志行为提为invalidateAccountState后，`pnpm --dir frontend exec vitest run src/api/account-cache.test.ts --reporter=verbose`用真实QueryClient和普通字符串缓存值复现accounts.isInvalidated=false（cache-red.log）。随后统一刷新accounts/account/account-logs，sync成功与失败均执行；check/transition、移动分组和既有WS账号事件原位复用，合并重复失效路径。群快照不失效、不清空，完整成功sync仍仅由既有onSuccess替换。这个小模块用于多个真实调用者和纯缓存公共契约，不新增账号状态存储。纯缓存回归覆盖列表、详情、各筛选日志及旧成功快照不变；它不宣称跑过真实账号同步故障。

错误展示无需新增文案体系：既有AccountFeedback已为SESSION_EXPIRED、ACCOUNT_SUSPENDED、ILLEGAL_TRANSITION提供中文并由Feedback保留code；新增三条直接ApiError值的UI断言确认，不构造账号响应。真正transition非法仍显示ILLEGAL_TRANSITION，不在客户端改写成其他错误。

最终定向`pnpm --dir frontend exec vitest run src/api/auth-failure.test.ts src/api/account-cache.test.ts src/api/client.test.ts src/components/AccountFeedback.test.tsx src/api/realtime.test.ts --reporter=verbose`：5files/20passed，7.04s（affected-green.log），包含9项既有认证并发、安全续期/会话隔离及通用WS游标回归。`pnpm --dir frontend typecheck`通过（typecheck.log）；`pnpm --dir frontend build`通过，3259modules/6.49s（build.log），原大chunk警告不扩展处理。

生产artifact：index-CwaG6Tsh.js SHA256 CE2948DF793915BFF72E2F58AB88C3ADECDCE7B7B21AB01F7A471B0CB3E545A7；CSS仍index-gBi7et6o.css，SHA256 D6799A9B78D5261061B96F12DB17485170BC815A8B2D31805B24D1802C660CB6。本轮证据全部在`.local/inspection-20261015/iris-i20/`，artifact-hashes.json保留机器可读值。

限制：本轮没有运行真实check/connect/sync或强制账号失效，也没有浏览器重连/窗口恢复真实故障验收；没有必要重复无法触及终态场景的空栈E2E。精确后台终态状态/code由Orion/Niko修复与验证，实际已上传账号业务按用户选择留待手动验收。上述纯条件、普通QueryClient值和中性auth transport通过不等同真实Telegram链路通过。源码与本记录稳定后冻结，SUBMITTED等待主控集成/独立review。

## I21：单账号删除（2026-10-15，Revision 8）

Iris · 前端工程师，复用已批准Sol High，Fast未暴露。基线5ed55b6，按共享契约Revision 8实施；无分支/Git提交、递归、真实账号/凭据读取或5173操作。先CodeGraph定位，沿用AntD组件、Accounts集中mutation、I20账号缓存与错误显示。早期202/pending/CAS建议已被用户简化要求替代，本实现不保留这些路径。

列表和详情复用AccountDeleteButton，admin单账号入口，idle/disconnected/session_expired/suspended可确认，online/rate_limited/connecting禁用并提示先离线；未知状态不放行。终态删除独立于A1连接动作。Accounts持有目标ID并渲染一份DeleteAccountDialog，使用当前列表状态，状态变化可禁止确认；依赖占用仍由后端权威判断。确认文字仅说明本系统账号/凭据，不删除Telegram账号或退出其他设备。确认使用同步ref锁和pending/loading，防同帧双击，未结束不能取消；失败留窗、记录和选择，手动重试，不自动重放DELETE。

按冻结契约发DELETE /api/accounts/:id，无body；204后关匹配详情/确认并prune选择，removeAccountState原位扩展现有缓存模块：过滤列表目标、清目标详情/所有筛选日志/群快照、刷新列表与分组计数，保留其他账号和历史业务缓存。MainLayout处理account_deleted {accountId}复用同一能力；列表实际集合变化时Accounts关闭匹配详情/确认并prune消失的选择，支持其他窗口删除。删除最后一行后将分页限制在有效页。没有额外状态层、事件轮询、批量删除或删除worker。

沿用AccountFeedback：error.reason直接从ApiError.details.reason读取，区别active_connection/managed_group/pending_message/active_job/active_effect和安全网关fallback；未知原因使用总说明，原code始终保留。ACCOUNT_RETIRED、ACCOUNT_DELETE_INCOMPLETE与NETWORK_TIMEOUT都有中文后续提示。日志枚举补delete“删除账号”。新增AccountDeletion组件仅因为列表/详情需共用有状态确认与权限/禁用说明，不复制两套删除流程；未引依赖/主题/CSS。

TDD证据均为纯props、直接错误值或真实QueryClient普通缓存键/字符串/ID，不包含account/gateway HTTP、Session或RPC替身。cache-feedback-red.log为5failed/4passed（目标缓存未清理+四个删除错误未中文化）；dialog-red.log复现三种不可删状态仍可点，以及双击onConfirm调用2次。该日志另有AntD prepare帧立即可见断言问题，不计产品red。最小安全实现后cache/错误已绿，slice-green.log剩4项仅在过渡prepare帧检查可见性，改为等待真实Tooltip/Modal可见后全绿，未修改产品动画或timeout。

最终定向命令：`pnpm --dir frontend exec vitest run src/api/auth-failure.test.ts src/api/account-cache.test.ts src/api/client.test.ts src/components/AccountFeedback.test.tsx src/api/realtime.test.ts src/components/AccountDeletion.test.tsx --reporter=verbose`，6files/36passed、14.06s（affected-green.log），包含原20项受影响安全回归。新控件覆盖四个可删状态、三种先离线状态、viewer隐藏、取消无提交、双击一次/提交中禁取消、失败留窗与可重试。QueryClient覆盖只移除目标、不损其他缓存与分组计数失效。初次typecheck发现新测试借用了Playwright的exact选项；Testing Library name字符串默认精确，删除该无效选项后typecheck-final.log通过，运行时查询语义不变，未重复36项。

`pnpm --dir frontend build`通过，3260modules/8.81s（build.log），原大chunk提示保留。产物index-BSdZKMfN.js SHA256 3216C82494B4A8972199D92E3D47BC314DE19A5AAF7C750808838858D115DF1D；CSS index-gBi7et6o.css / D6799A9B78D5261061B96F12DB17485170BC815A8B2D31805B24D1802C660CB6未变。artifact-hashes.json/source-hashes.json位于`.local/inspection-20261015/iris-i21/`。

主控批准仅扩既有两条empty-system E2E：实际accounts=[]后，admin删除不存在ID两次204；viewer无删除入口且DELETE403，不创建任何账号。该测试编辑后的typecheck-with-empty-e2e.log通过，生产源未变，未重复unit/build。实际schema9服务由主控先交Orion再统一交接，当前尚未启动/执行浏览器，不用旧schema重复验收。

边界：纯控件回调不伪造API成功，缓存值不冒充Telegram资料；本轮未验证真实已上传账号成功删除、占用拒绝、跨库不完整重试、真实凭据清理或跨窗口真实删除。后端/网关真实PG边界由各owner提供，用户现有五账号完整保留待手动验收。源码冻结SUBMITTED，等待主控独立review和schema9真实空栈联测。

I21主控确认对象修正：删除确认仅随机内部ID不易核对，现复用当前Account.phone及既有accountName；手机号/昵称有值才显示，内部ID始终显示且请求仍绑定deleting ID，不读凭据、不增加API或样式。先加纯props文本输入断言，identity-red.log 1failed/11passed（手机号未显示）；最小props/文本改后identity-green.log 12passed/9.54s。输入为非真实的说明性字符串，不构造账号/网关响应。identity-typecheck.log通过，identity-build.log通过3260modules/7.65s；原缓存/transport结果复用，未重复36项。最新产物index-D3K4atoX.js SHA256 6261872789FBBEDB329E6E0E4CDB69EA93563935CDDADB2899E535489B261A3D，CSS未变。artifact/source-hashes已更新；源码再冻结，实际schema9空栈仍待主控交接。

## R22 controller: per-account action state

Red: pure rendered account columns could not disable only the pending account. Green: per-account mutation keys/pending IDs and synchronous double-click admission; errors stay with their account. Group moves affect selected accounts only, deletion affects its target. Removed obsolete global action busy/error state. Account columns plus deletion/auth tests23pass; typecheck/build pass. Browser check/connect budget60s covers backend connect40s plus profile12s; unrelated calls remain15s. No simulated account/Gateway response. Real concurrent account checking remains manual acceptance.
