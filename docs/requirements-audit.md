# 面试要求当前差距与验证清单（R33）

范围：主控独立完成，沿用 issue #4 / PR #5。基线 `708633c`，叠加本次 R33 限流恢复修复与测试。规范为 [候选人版](全栈开发工程师笔试题-候选人版.md)，用户覆盖项以 [实现契约](implementation-contract.md) 为准。本文是当前结论；旧验收矩阵内的历史模拟网关记录不能替代当前证据。

## 结论与优先级

核心功能均有实现入口，但不能宣称 A/B/C 全部验收完成。本轮确认并修复一个实际缺陷：**没有待发消息时，账号限流到期不自动恢复**。其余新增用例补齐证据，没有为了制造红绿过程而修改正确实现。

| 优先级 | 尚未完成或未验证项 | 当前判断与下一步验收标准 |
|---|---|---|
| P0 | A2 / S5：504 已落地、未落地、重试再次未知；by-client-id 连续503后恢复 | 生产代码有持久意图、查询和一次重试；原模拟网关测试已删除。R33仅验证真实端口不可用时保持unknown、迟到回执合并、Agent同key不重新审计/入队。**尚不能证明网关恰好一条、504起5秒收敛、查询恢复后2秒收敛**。真实Telegram不提供题目假设的两秒结果收敛保证，当前采取保留unknown避免重发，严格时序契约因此尚未完全满足，不能仅靠补测试宣称完成。 |
| P0 | A5.8：发消息或kick已经产生外部效果、落库前进程被杀 | 有持久effect、epoch、网关key及对账实现；本轮强杀覆盖审计中断，不覆盖已发送/已踢人的不可逆窗口。需证明同runId恢复、不重复效果、不误记失败；kick后目标重新入群也不能再踢。 |
| P1 | A2：SSE接入层断连、整库不可用后的重放 | R33覆盖**已持久化inbox**的乱序、重复、消息事务失败、inconsistency和重启续跑；没有验证socket断流、inbox首次写入失败或整个PostgreSQL停机。需要真实网关历史/游标与最终逐条消息对账。 |
| P1 | A3 / B2：邀请未就绪/过期、join无事件、promote上限、退群失败 | 状态机分支在 `groups.go`，但当前保留测试未完整覆盖这些外部结果。需要验证JOIN_TIMEOUT、promote总数≤2、非群主失败后其他成员继续/群主保留、后台与网关成员一致。用户建群截图属于正常流程人工证据，不等于异常验收。 |
| P1 | B1：发送回执与推进同时发生、运行中重启后的重排 | 当前实现仍是回执驱动；R29现场有数步真实间隔证据。完整多步重启窗口、限流顺延、只重排最早过期一步仍缺当前端到端故障证据。S7/S8并发与预检本轮已加强验证。 |
| P1 | B4：浏览器断线后3秒内补齐且不重复 | 保留WS后端及前端游标单元证据；旧模拟栈浏览器证据不再算当前完整验收。R32 C3验证已有run详情，不等于断线补齐的时限测试。 |
| P1 | A5.5：无可用执行账号的错误码 | 固定管理员身份已按用户要求实现，不应回退到其他账号；当前返回 `ADMIN_UNAVAILABLE`，题目为 `NO_AVAILABLE_ACCOUNT`。这是**尚未对齐的错误码差异**，不是已经通过规范码表。须保留管理员固定约束并统一错误码/提示后验证。 |
| P2 | A5工具边界与协议组合覆盖 | 已有坏JSON、未知工具、重复tool ID、12步、60秒、取消等测试；仍缺当前完整矩阵：turn非2xx/单轮迟到响应、协议计数重置的所有组合、kick策略/权限及审计文本、50条/500字/8KiB组合上限。代码存在不代表这些每一项均已执行。 |
| P2 | C1所有格式现场验证 | R32已验TGS动画与MP4、保留清理/鉴权/失败重试的本地测试。真实照片、音频、普通文件、WebP/WebM贴纸、文件过期、跨DC/文件引用刷新仍需各自实测；不重复跑已通过的本地媒体用例。 |
| 发布门禁 | 当前候选完整发布CI、双版本升级/回滚 | 开发PR继续轻量CI；本轮不启动完整CI或双版本演练。旧完整CI结果不能给当前候选背书；本地镜像不是CI原制品。 |

## A0–C3 条款逐项映射

状态用语：**本轮验证**=本次执行；**复用证据**=保留的相关测试/历史验证，相关职责未因本轮改动失效；**部分验证**=仅列明的边界成立；**待验证**=代码存在但缺当前适用结果。未把静态阅读当作运行通过。

| 条款 | 实现 / 当前有效证据 | 结论与剩余边界 |
|---|---|---|
| A0 重复迁移、旧schema拒启动 | `migration_contract_test.go`，R32在schema12实际通过 | 复用证据；本次没有迁移变化 |
| A0 错误包、viewer写入403 | `app.go`、`auth_test.go`、各业务权限测试 | 已覆盖部分写入口；“所有扩展写API”的穷举权限矩阵尚未本轮执行 |
| A1 状态图、并发CAS、终态无出边 | `accounts.go`、`accounts_test.go`、`account_terminal_test.go` | 本轮并发断言加强为精确CAS_CONFLICT；终态现有证据复用 |
| A1 终态状态/成员/排队后果原子性 | `business_persistence_test.go`注入真实PG事务失败 | 本轮复测通过；网关同步永久错误的完整映射未重新做平台故障注入 |
| A1 限流到期、离线/终态不被覆盖、提交后事件 | `rate_limit_expiry_test.go`，真实PG双实例 | **本轮红→绿修复**；无需消息队列也恢复，未来截止时间不提前恢复，只有一次WS状态事件 |
| A2 出站先记录，accepted/sent分离 | `repo_message.go`、R29真实剧本回执 | 部分验证；202响应丢失/进程崩溃窗口仍待验证 |
| A2 504与503对账、一次重试 | `dispatch` / `reconcileMessage` | R33验证unknown在不可用端口超过5秒及重建后保留，并接收迟到回执；不代表504/404/503全部HTTP路径 |
| A2 / S2 / S3 去重、乱序、自己消息不触发 | `events.go`；R33 `TestInboxWriteFailureRecoversAfterRestartWithoutDuplicateOrOwnTrigger` | 本轮验证持久inbox边界；离线账号旧消息仍isOwn，两个不同eventId同msgId只一行/一次run |
| A2 数据库写失败不能丢事件、通知操作员 | 同上，PG触发器故障+WS inconsistency+App重建 | 本轮验证消息事务失败；整个DB离线和最初接入inbox失败仍待验证 |
| A2 RATE_LIMITED等待期不重发、FIFO顺延 | `applyAccountRateLimit` / `sendOne`，现有调度测试 | 恢复缺陷已修；未把到期测试称作429平台限流完整验收 |
| A2 群不可写与账号错误分流 | `dispatch` / `unreachable`，终态和取消测试 | 状态处理有实现；各同步/异步网关错误组合需专项验证 |
| A3 建群、创建者入表、成员事件、提升 | `groups.go`、群工作区测试 | 正常业务入口存在；外部异常见P1 |
| A4 稳定分页、WS已提交单调seq | `business_persistence_test.go` / `ws_test.go` | 保留证据复用；本轮新用例也实际读取WS状态/inconsistency |
| A5.1 每群单run、多实例、待处理消息升序 | `agent_concurrency_contract_test.go` | 本轮两个App实例同库，HTTP turn峰值1；后续外部消息进入新run，自有消息排除 |
| A5.2 12步、60秒、重启停机时间、turn超时 | `agent_boundaries_test.go` / `budget_test.go` / `agent.go` | 既有12步/60秒证据复用；本轮审计真实30秒累计与强杀用例通过；单轮turn迟到仍有缺口 |
| A5.3 协议错误形状/历史/连续计数 | `agent_test.go`、`agent_boundaries_test.go`、`domain/agent.go` | 有坏JSON→未知工具→正常结束、重复ID及原始响应证据；全部输入组合未穷举 |
| A5.4 审计fail-closed、最多3次 | `agent_failure_contract_test.go` | 本轮500、坏JSON、围栏、尾随文本、缺失/非法/null verdict、超时8分支；只一工具步、零出站、blocked/audit_blocked |
| A5.4 审计拒绝不消耗key | `TestAgentAuditRejectionDoesNotConsumeSendKey` | 本轮相同key、新tool ID重新审计，模型收到AUDIT_REJECTED；没有出站记录 |
| A5.5 固定在线管理员、执行途中终态 | `group_workspace_test.go` / `agentSend` | 不回退群主已有测试；错误码差异见P1；执行中终态的全路径待补 |
| A5.6 kick策略开关 | `executeAgent`先判策略再审计 | 当前专项回归待补；不做真实踢人测试 |
| A5.7 同key不重复审计/入队 | `TestAgentSendKeyReuseAfterUnknownDeliverySkipsAuditAndEnqueue` | 本轮真实端口不可用→SEND_TIMEOUT→同key返回原unknown记录，审计仅1次、出站仅1行；真实落地唯一性仍属P0 |
| A5.8 恢复同runId与原检查点 | `TestAgentAuditAttemptBudgetSurvivesProcessKill` | 本轮实际强杀子进程，仍同runId、turn1次、审计累计3次；外部效果窗口未覆盖 |
| A5.9 结果≤8KiB、摘要≤200字 | `recentMessages` / `executeAgent` | 有裁剪实现；极长Unicode、多条消息、limit上限的组合边界待补 |
| A5.10 当前步结束后取消 | `agent_cancel_test.go`覆盖end_turn/finish、禁用/不可达/暂停 | 复用已存证据；当前步产生外部效果时的取消窗口待补 |
| A5.11 相同读取不无限运行 | 12次读取到budget_exhausted的既有用例 | 复用证据 |
| A5.12 每步可见 | Agent详情API/独立页/群内tab、R32 C3 | 复用真实已有run的浏览器证据；不代表新模型轮次成功 |
| A6 / 页面1–3 | 登录、账号、群管理、viewer隐藏写入口 | 复用相关前端测试与现场检查；本轮没有UI改动，不重跑前端本地测试 |
| B1 变量继承、空值、来源、预检 | `domain/sequence_test.go` / `sequences_test.go` | 本轮精确验证422/code/stepIndex/key，预检失败无运行和消息 |
| B1 / S7 同群并发启动 | `sequences_test.go` | 本轮恰好201与409，冲突码精确SEQUENCE_ALREADY_RUNNING |
| B1 角色、限流、回执排期、跳过、重启 | `sequences.go` / R23 / R29 | 用户指定角色分配有测试；完整定时/重启异常链见P1 |
| B2 邀请及群主最后退、失败一致性 | `groups.go` / `group_member_jobs.go` | 实现存在；当前外部异常和真实退群专项未验证 |
| B3 轮换/重放撤销/logout/singleflight | `auth_test.go`、前端client/auth-failure及cookie E2E | 既有相关证据复用；本次没有认证改动 |
| B4 断线补齐、步骤详情 | WS客户端和服务端、R32 C3 | 详情已验；当前浏览器3秒断线场景仍待补 |
| C1 媒体下载/清理/运行保护 | R32 media测试及TGS/MP4现场 | 已实现且有分层证据；其他现场格式见P2 |
| C2 独立真实LLM服务 | `provider`、真实DeepSeek工具/审计与R29现场回复 | 用户替换Claude/Gemini；真实Responses端点未验 |
| C3 登录→群→Agent步骤 | `frontend/e2e/group-agent.spec.ts`，R32实际1 passed | 复用；空系统CI显式skip，不能算自动获得该业务证据 |
| 交付 README / Git历史 | README、现有分支PR | 已有；最终提交CI见PR；不发布、不合并、不改仓库可见性 |

## 题目差异与用户覆盖项

- 用户已明确用Go/gotd、真实Session上传、DeepSeek独立provider替换题目的Node后端、预置服务账号与Claude/Gemini；移除了账号/网关模拟器，不为补绿测试重新引入。
- 群Agent固定管理员；群级上下文持久保存，但每个run仍遵守单次预算。同群普通成员按用户选择顺序轮流发言，而非题目默认字典序首个成员。
- 用户增加暂停/恢复、彻底结束、批量删除、账号分组、媒体预览、AI剧本生成等；ending/cancelled等扩展状态、保留完整rawResponse而非2KiB均需在交付时说明，不写成原文零差异。
- `ADMIN_UNAVAILABLE` 与 `NO_AVAILABLE_ACCOUNT` 的差异单独列为待对齐项；它不是采用Go或固定管理员身份必然要求的协议变化。
- [API覆盖表](api-flow-coverage.md)中的19个方法/路径存在性结论仍有效；接口存在、页面可操作、异常契约通过是三种不同证据。

## 本轮验证范围与复用规则

- 新增15个叶子场景（审计不明确8分支、审计拒绝、审计强杀、发送key复用、双实例、inbox恢复、unknown恢复、无队列限流到期）；加强已有CAS/S7/S8错误码与无副作用断言。
- 本地定向race覆盖19个叶子场景，包括上述新增场景（不重复耗时30秒的审计超时）和受发送调度修改影响的3个现有用例。审计超时已单独正常执行通过；不是跳过后算通过。
- 仅使用隔离PostgreSQL随机schema、临时子进程、Agent HTTP协议测试服务和不可用回环端口。未连接Telegram、未调用真实LLM、未修改用户群状态或发送消息。没有新增网关成功响应替身。
- 限流用例先失败（超过2秒仍未online），最小修复后通过。首个沙箱连接失败属于环境失败；审计强杀测试HTTP服务清理阻塞修正后重跑，均不算产品缺陷的红阶段。
- 本次生产差异只有账号到期恢复和调度入口；复用现有transition及已提交WS事件，无新增表、API、后台服务或依赖。新增进程测试把媒体目录传入既有子进程helper，避免写到源码目录。
- 网关、媒体、UI、认证、真实平台验收未受本次代码变更影响，不重复执行；开发PR规定的轻量CI仍执行。不将轻量CI描述为完整PG/race/发布验收。
