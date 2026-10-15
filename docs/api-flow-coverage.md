# 面试 API 与当前操作流程覆盖（2026-10-15）

> API存在性与页面入口见本文；逐条行为、未完成/未验证项及R33关键异常证据见 [当前要求审计](requirements-audit.md)。

本次依据 `全栈开发工程师笔试题-候选人版.md` 的 2.1、2.2、2.3、A5、C1、C3，对照当前路由和页面调用。沿用 issue #4 / PR #5；主控独立实现和验证。

**结论：2.3 的 19 个方法/路径组合均已暴露，B3 的 refresh/logout 也已暴露。当前页面有对应业务入口，但不能把用户走过一次正常流程当作所有接口的异常契约验收通过。**

| 面试接口 | 当前操作入口 / 调用链 | 覆盖边界 |
|---|---|---|
| POST /api/auth/login | 登录页 → ApiClient.login | C3 本次实际验证 viewer 登录；admin/RBAC 另由鉴权测试覆盖 |
| GET /api/health | 服务就绪检查 | 无单独用户按钮；部署后核验 ok/schemaVersion |
| GET /api/accounts | 账号管理、群账号选择 | 页面直接调用 |
| POST /api/accounts/:id/connect | 保留规范入口；页面“连接并检测”调用扩展 /check，再复用连接生命周期 | 正常点击不直接命中 /connect；不能把页面检测等同该路由全部输入契约测试 |
| POST /api/accounts/:id/transition | 标记离线 / 释放账号 | 页面携带 expectedFrom；非法转移和 CAS 冲突需专门验证 |
| POST /api/groups | 创建群组 | 返回 jobId，JobFeedback 轮询任务；本次未建新群 |
| GET /api/groups | 群组列表 | C3 实际读取 |
| GET /api/groups/:id | 进入管理 | C3 实际读取 |
| PATCH /api/groups/:id | Agent/自动移除开关、管理员绑定 | 页面直接调用；本次未改开关 |
| POST /api/groups/:id/send | 实时聊天、选择账号快捷回复 | 共用可靠发送队列；C3 不发送消息 |
| POST /api/groups/:id/leave-all | 结束群组营销 | 保留 B2 非群主先退、群主最后退出的工作链；本次未执行退群 |
| GET /api/jobs/:jobId | 建群、成员操作、结束营销后的 JobFeedback | 操作日志同时使用扩展 GET /api/groups/:id/jobs；不能只看日志列表便认为每种任务失败已覆盖 |
| GET /api/groups/:id/messages | 实时聊天、“加载更早” | 保留原字段与游标；C1 附加媒体字段，分页顺序冻结但文件可用状态从实时记录读取 |
| GET /api/agent-runs/:id | Agent 回复记录、独立运行追踪 | C3 核对实际已保存步骤数量及 JSON 详情 |
| GET /api/groups/:id/agent-runs | Agent 回复记录批次选择 | C3 实际读取并选择已有批次 |
| POST /api/sequences | 剧本库新增 | 可视化/JSON 编辑，PUT 为额外编辑入口 |
| POST /api/groups/:id/sequence-runs | 剧本与账号 → 检查预览 → 确认启动 | 预览调用单独的无发送预检接口；只有确认才调用规范启动入口 |
| GET /api/sequence-runs/:id | 剧本发送记录 → 选择批次 | 读取已冻结步骤、变量来源和投递状态 |
| WS /ws | 登录后的 MainLayout / RealtimeClient | auth 首帧、seq、重连、缓存刷新；C3 经过实际连接，重放/过期需专项测试 |
| POST /api/auth/refresh、POST /api/auth/logout（B3） | ApiClient 自动刷新、退出登录 | 已有鉴权回归；一次 C3 登录不会覆盖15分钟过期和重放撤销 |

证据入口：`backend/internal/platform/app.go`；`frontend/src/pages/{Accounts,Groups,GroupDetail,Sequences,AgentRun}.tsx`；`frontend/src/components/{JobFeedback,GroupActions,GroupSequence,SequenceProgress,GroupAgentHistory,EndMarketingButton}.tsx`；`frontend/src/api/{client,realtime}.ts`。

## 2.1 / 2.2 / A5 与页面的关系

- 网关接口由后台调用，不应让浏览器直连 Telegram。账号/群/发消息、按 clientMsgId 对账、SSE 在网关已有对应入口。本次替换了原来固定404的 GET /media/:id。
- POST /agent/turn 与 POST /agent/audit 属于 Agent 服务（`backend/internal/provider/provider.go`），由后台执行器使用；网页的“AI 回复预演”不是这两个接口的完整验收替代品。
- A5 的 get_recent_messages、send_message、kick_user、finish 是 turn 协议工具，不是四个额外网页 REST 路由。`domain/agent.go` 定义工具，`platform/agent.go` 执行并保存步骤。普通问答只会覆盖其中被模型实际调用的工具，不能因此声称真实踢人已验证。
- C3 证明登录、群入口、运行列表、运行步骤与完整结果可见；它读取已有记录，不产生新的模型轮次，不证明本次真实模型生成/审计/Telegram 投递。失败、blocked、原始协议响应和异步投递应结合日志与专项证据判断。
- 用户变更已经保留：真实 Session 上传替代预置服务账号；Go/gotd 替代模拟网关；DeepSeek 替代 Claude/Gemini；管理员固定为群 Agent 身份；同群串行 run、上下文跨 run 保存；新增暂停与彻底结束。新增 ending/cancelled 等状态属于用户扩展，不能直接称为原试题枚举零差异。

## C1 媒体链路与范围

1. Telegram photo/document 更新保留 mediaUrl、kind、MIME、名称；下载时用现有在线成员客户端读取原消息刷新 file_reference，按文件所属 DC 下载，不另起账号登录。
2. 后台独立、最多2个媒体 worker 下载到 MEDIA_DIR；不占用消息/剧本发送名额，不阻塞 SSE 入库。64MiB 单文件上限、100秒请求上限；失败退避，最多5次自动尝试，页面可手动重新下载。
3. GET /api/media/:id 沿用 Bearer 鉴权和 viewer 只读权限；前端认证请求取得 Blob 后显示，不把 token 放进URL。仅信任配置网关的 /media 路径，不跟随重定向。文件不会成为公开静态目录。
4. 展示 JPEG/PNG/WebP/GIF、音视频原生播放器、文件下载、WebP静态贴纸、WebM视频贴纸、TGS动画。TGS 复用 lottie-web 的无表达式播放器，限制解压体积并拒绝外部图片资源；格式不受浏览器支持时保留下载入口。
5. 默认保留30天（MEDIA_RETENTION_DAYS）；Docker 独立 media-data 卷。运行中 Agent 所在群的媒体全部保留，保护范围有意比“已读取文件”更宽。先清除 localFilePath，再删文件；deleting 可恢复；旧分页不会引用已删文件。
6. schema12 为旧版只存空文本的 Telegram 消息建立补取任务；从原群/消息ID补取，不重发消息、不重新触发 Agent。原消息已删除、没有在线成员、文件超限等会给出明确状态；不把这些情况冒充下载成功。

## C3 可重复执行

使用已有本地服务和已有 Agent 历史。默认空系统 CI 不具备真实群与运行数据，C3 用例明确跳过；既有空系统 E2E 不变。选择以下只读用例时，会阻止除了网页鉴权以外的所有写请求，无已有有效记录则失败。

```powershell
Set-Location D:\web_projects\kapibala_ai\frontend
$env:E2E_EXISTING_GROUP='1'
$env:E2E_BASE_URL='http://127.0.0.1:5173'
$env:E2E_API_URL='http://127.0.0.1:5173'
$env:E2E_BROWSER_CHANNEL='msedge'
pnpm exec playwright test e2e/group-agent.spec.ts
```

默认 viewer/viewer；可用 E2E_USERNAME/E2E_PASSWORD 指定网页用户，E2E_GROUP_ID 限定一个已有群。测试关闭 trace、视频和截图，不把真实消息上传到 CI。该例本次实际运行通过（1 passed）；不是 skipped，也没有插入模拟 run。

## 仍需区分的验收证据

- 正常页面流程覆盖正常入口，不覆盖：429重置、503恢复、504结果未知后的对账、重复/乱序事件、并发CAS、审计服务异常、坏JSON、同key幂等、run预算、进程崩溃恢复及退群失败顺序。
- 现有纯逻辑/真实PostgreSQL测试和历史验收证据分别保留。按用户要求退休的模拟网关成功用例不算当前通过。本次不重新声称这些所有故障窗口都在真实 Telegram 验证。
- 本次 C1 定向测试覆盖更新转换、媒体入库、真实HTTP文件字节、鉴权、SSRF/重定向拒绝、超限、保留保护、清理恢复、游标新鲜状态、重试和浏览器展示；这些本地证据与真实 Telegram 媒体下载的现场检查分别记录。

现场补充：已有TGS动画贴纸和MP4视频已从真实Telegram补取，浏览器动画/视频解码通过；新到MP4同样下载成功。未主动发送、加退群或触发模型测试；这覆盖当前现场格式，不声称所有媒体格式均经过真实平台发送验收。
