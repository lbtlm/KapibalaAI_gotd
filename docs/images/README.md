# 页面图片

这些 PNG 直接截自本机 `http://127.0.0.1:5173`，不是设计稿或合成页面。用户明确要求保留测试手机号、群链接和真实测试消息，用于 README 与验收指南。未包含 Session、API key、数据库内容或登录凭据。

- 采集时间：2026-10-16（Asia/Shanghai）。
- 视口：1760 × 1040；列表截图仅截取有内容的上半部分。使用项目现有 Playwright 与 Edge，只读浏览。
- 页面基线：前端 / Gateway `708633c`，Backend `e36d699`；本轮 Backend 改动不涉及界面。
- 群在截图时已暂停营销；没有启动剧本、发送消息、调用模型、改变开关或成员关系。除网页登录、刷新和退出外，写请求被阻止；采集未出现被阻止写请求或页面脚本错误。
- 图片保留当时的失败 / 已取消批次和历史状态，不作为所有功能、异常或时限都通过的证据。

| 文件 | 说明 |
| --- | --- |
| [accounts.png](accounts.png) | 账号列表、检测状态和操作 |
| [groups.png](groups.png) | 群列表、群主 / 管理员和快捷入口 |
| [group-chat.png](group-chat.png) | 实时聊天、测试消息与账号选择 |
| [script-editor.png](script-editor.png) | 剧本折叠编辑、角色 / 延迟与对话预览 |
| [sequence-history.png](sequence-history.png) | 发送批次、进度与逐步投递 |
| [agent-history.png](agent-history.png) | Agent 批次、模型回复与执行步骤 |

新截图应继续来自实际页面，先确认数据展示范围；不得把图片中的历史状态作为新候选测试结果。
