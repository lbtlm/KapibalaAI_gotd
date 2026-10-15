# 前端复用记录

源仓库只读：D:/web_projects/card_code_workspace/go_tg_assistant，源提交 1d7ca2240ec8c41650c27b4c7965baf84cf36a35。本轮先读源 root/frontend AGENTS，实际用 Copy-Item 拷贝下表文件，再原位精简适配。未导入源 .git、Session、生产配置、数据库备份或租户/支付/营销/WebA 模块。

| 原文件（frontend 下） | 当前文件（frontend 下） | 必要适配 |
|---|---|---|
| src/main.tsx | src/main.tsx | 保留 React18 StrictMode / ReactDOM createRoot / RouterProvider / AntD App / QueryClientProvider，删租户/主题/聊天室桥和devtools，中文locale |
| src/routes/index.tsx | src/routes/index.tsx | 保留 createBrowserRouter / Navigate，缩至登录、账号、群列表详情、Agent详情、序列；加入cookie恢复会话guard |
| src/layout/MainLayout.tsx | src/layout/MainLayout.tsx | 保留 AntD Header/Sider/Content/Menu、Outlet/navigation骨架；删动态租户菜单/订阅/主题，改三入口与实时连接状态 |
| src/layout/MainLayout.module.scss | src/layout/MainLayout.module.scss | 保留 scoped layout/sidebar/header/content类，按消息操作台调整密度、颜色tokens、时间线与审计阻塞 |
| src/views/resources/account/components/AccountTableColumns.tsx | src/components/AccountTableColumns.tsx | 保留 ColumnsType建表、Typography/Space/Button模式，替换字符串账号ID与题目状态/合法按钮；删源订阅/删除/聊天/手机号逻辑 |
| src/components/resource/StatusTag.tsx | src/components/StatusTag.tsx | 保留状态映射+AntD Tag模式，移除资源/代理旧映射，替换题目生命周期与发送/运行状态 |
| src/saas/utils/api-client.ts | src/api/client.ts | 保留 axios实例、refreshPromise单飞、401只重试一次思路；删除localStorage token、refresh body、多路径配置和重复入口，改内存access/HttpOnly cookie；opaque token role来自服务端 |
| src/vite-env.d.ts | src/vite-env.d.ts | 保留 Vite client类型入口；清除源业务环境变量契约（当前无前端密钥） |

Vite、TS与package配置按源React18/Vite工具链重建最小依赖集，不复制源锁中的不相关模块。pnpm-lock.yaml为本项目真实解析依赖锁，保留版本来源。一个请求入口为 api.request；所有业务请求与登录续期均使用它。

新增页面、Job反馈、GroupActions、ImportGroup、SequenceProgress、RealtimeClient和类型是题目没有现成等价模块的必要适配。认证、成员/消息/Agent/序列跨服务公共协议保持原题命名；GET groups/sequences裸数组，preview为POST群sequence-runs/preview。外部群主允许creatorAccountId为null。未加入i18n/主题切换/响应式功能。

视觉方向：运营员核验消息、账号和审计结果；领域词为服务账号、成员角色、受理/发出、序列排期、审计阻塞、游标补拉。颜色来自纸白时间线、深蓝墨水、灰色元数据、绿色在线信号、黄色未知结果和红色阻塞。签名为每条消息左侧状态轨道与明确发送状态，并与Agent步骤/序列来源贯通。复用原操作台导航与组件，不增统计仪表盘。
组件检查点共同遵循：焦点是当前任务状态/内容；白与冷灰的轻层次、细边界；Segoe UI与Microsoft YaHei区分30px标题、18px区域、14px正文、12px时间；4px节奏，面板24px、页面36px、sidebar220px，单一绿色主操作。native/AntD控制保留键盘与表单标签。

截图为实际UI在headless Edge 1440px渲染，HTTP fixture明确为展示数据（不证明真实业务链）。位置 .local/frontend-visual/login.png、accounts.png、groups.png、agent.png、sequences.png。已逐图检查无空白/重叠/截断，阻塞运行强调padding已修正。截图数据不是Telegram真实资源。



F15 attempt1 revision1（2026-10-15）：在既有 Sequences 的 inspect/start mutation 原位绑定 groupId/input/revision，请求和确认复用同一快照；替代旧成功回调读取可变 group 的路径。服务器 active run 优先，原 runId 改为含所属群的最后运行结果，替代长期覆盖当前运行的选择逻辑。复用原 SequenceProgress、React Query 失效及 MainLayout WS 入口，没有第二套进度/重连模块。新增 PreflightSnapshot 类型用于跨预检/确认保存一次用户输入，revision 用于拒绝已被输入变更淘汰的响应；没有新增业务 API。

B4 复用现有实际栈 Playwright、HTTP 准备和 fake 网关注入；将 C3 建群代码提为同文件 createTestGroup 供 C3/B4 共用，原路径已替换，没有保留重复实现。仅新增覆盖缺失验收窗口的浏览器用例，WS transport fault 转发真实服务消息，不 mock HTTP。Playwright 的 Vite 启动端口原位随 E2E_BASE_URL 选择，默认仍 5173。未复制新仓库模块、未增加依赖、框架或视觉方向。红绿及 3 秒唯一性证据见 tdd-frontend.md 的 F15。

## F16 attempt1 revision1：账号管理（2026-10-15）

复用既有 AccountTableColumns、StatusTag、Feedback、MainLayout 与 React Query 查询失效；账号列表原位扩列与筛选，旧“重连”入口替换为显式“连接并检测”，继续调用既有 A1 transition 处理离线/释放。没有第二套账号连接管理或认证请求入口。仅账号路由去除 content 的 1600px 限宽，其他工作区保持既有布局。

只读交互参考：D:/web_projects/card_code_workspace/go_follow_live，提交 00eb35a56ad1f4bf382e6e3906d35bacd1740376，frontend/src/views/Accounts.vue 的上传/详情入口及 frontend/src/components/AccountDetailModal.vue 的标签分区和群列表。Vue 与当前 React、生命周期和 HTTP DTO 不同，未直接复制代码；未引入源批量运营、账号恢复、删除、自动连接或授权数据。

必要新增 AccountDetails（独立只读查询/群缓存/分页日志）、ImportAccounts（同名配对预览和逐项返回）、AccountGroupManager（单分组 CRUD）分别对应冻结契约，均组合现有 AntD Modal/Tabs/Form/Table/Select 原语。AccountFeedback 在上述多个入口复用账号错误的中文主说明，安全 code 仍可查证；不另造通用表单、上传系统或连接池。配对只读文件名，不在前端解析/打印授权内容。ApiClient 未改动，沿用 Axios 的浏览器 FormData 自动 boundary 行为。

实际栈测试的 Telethon SQLite 表结构参考本批 Niko 的 gateway/account_management_test.go（同一开发快照），由 Node 22 内置 SQLite 在临时目录生成完全合成文件，授权字节不是实际账号，结束删除；不将 Session 文件加入 Git 或证据包。仅新增覆盖缺失公共行为的 UI/HTTP/浏览器测试，无额外依赖。旧账号操作/表头实现已原位替换，没有保留重复入口。
