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
- 1440/1280 五页以及 empty/loading/error/预检状态已实际浏览器渲染；截图仅视觉 fixture，存于忽略目录 .local/frontend-visual-v2。最终用户视觉验收仍待完成。
- 原有 8080 被其他 Docker 应用占用，保留该应用。Vite 新增可选 VITE_BACKEND_URL，Playwright health 使用 E2E_API_URL；默认端口不变。隔离 fake 栈运行 18080/18081/18082，使用独立 kapibala_e2e PostgreSQL 数据库。
- 首次真实栈运行 viewer 浏览器只读/直接写 API 403 通过；C3 暴露后台 invite POST 空 body 与网关 JSON 契约不一致。Orion 修为 {} 并通过真实 gateway HTTP 组合回归。
- E2E 现在按本次 job 返回 groupId 进入群，避免重跑时误选旧群。类型检查通过。
- 主控重建修复后的栈后，仅重跑受影响 C3：pnpm exec playwright test --grep C3，1 passed，8.1 s。实际 PostgreSQL、backend、fake gateway、scripted Agent 和 Edge；没有 page.route API mock，也没有真实 Telegram 发送。
- 两条真实栈用例现各自通过。当前 Vite session 42943、fake 栈 session 18220 由主控管理；此前 F1 session 65903 已停止。这些进程编号是本次交接信息，不是可重用启动参数。
