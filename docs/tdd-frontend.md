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


