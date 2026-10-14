# UI F2 审计与设计

Iris，F2 attempt1 revision2。F1提交3403a4e是功能基线；用户否定F1视觉后本轮重新验收。完整分段读取 interface-design/SKILL.md（约300行）和 design-taste-frontend/SKILL.md（1206行，含附录）。后者明示不适用于dashboard：仅取审计、反套路、色彩/字体/密度/a11y规则；营销hero、图片、GSAP、主题/手机扩展、换组件库不适用。产品结构由interface-design主导，使用现有AntD原语。

Reading this as: 日常Telegram多账号运营员的消息工作台，以紧凑、可查证的产品语言重构已有AntD应用。DESIGN_VARIANCE=3，MOTION_INTENSITY=1，VISUAL_DENSITY=8。导航与路由/表单协议保持；只改操作层级与视觉。

审计：30px大标题和英文eyebrow重复；账号大数字游离于表格；24px嵌套白卡导致密度过低；群操作表单遮住消息；Agent全展开JSON迫使长滚动；AntD彩色Tag荧光浅色低对比。保留功能/a11y原语，移除这些默认布局。

领域：账号受限、群成员角色、受理/发出、未知结果核对、审计阻塞、逐步变量来源、重连补拉。颜色世界：桌面消息纸白、深灰内容、冷灰轨迹、深蓝选中/动作、赭色等待、暗红失败。签名：消息发送状态与Agent/序列执行结果共享同一读法；主要内容保持可见，诊断就近展开。
拒绝：固定三卡后台→直接表格；营销eyebrow/大数字→业务标题+inline实际总量；表单占第一屏→消息主区+底部composer；单步巨卡→细密执行轨迹。
深度：细边界与表面颜色分区，不采用装饰阴影。4px基础，页面24px、区域16px、行8-12px。标题22px/650，区域14px/600，正文14px/400，元数据12px；CJK Microsoft YaHei、拉丁Segoe UI，ID/时间Consolas/tabular。产品操作使用已有AntD控件，按钮最小40px hit area。

组件实施检查点（写代码前）：
| Component | Intent / Hierarchy | Palette / Depth / Surfaces | Typography / Spacing |
|---|---|---|---|
| main/Layout | 当前工作区内容领先，导航从属，连接状态在底部 | 冷灰导航/白内容，选中深蓝，单hairline | nav176，页24，header48，14/12 |
| StatusTag/Feedback | 状态直接可读，阻塞优先，可解释empty/error/loading | 状态文本深色+低饱和小背景，Skeleton与最终布局相近 | 12/500，4-8，文字不靠颜色 |
| Accounts/columns | 一眼找到受限状态与合法动作 | 单表面紧凑行，错误保留，hover轻灰 | ID mono，14正文，40按钮，8px表格行 |
| GroupDetail | 消息第一屏；成员/Agent从属 | 主内容白，服务消息微蓝填充，诊断冷灰 | 2fr+280侧栏，message16，最短元数据 |
| GroupActions/Composer | 输入紧邻消息，策略与退群右侧 | 现有AntD表单与开关，一个主发送动作 | 标签12，账号+文本+send紧凑栅格 |
| AgentRun | 执行结果形成轨迹；阻塞上下文持续可见 | plain rows，不巨卡；错误行暗红语义 | 14工具/结果，12ID，input/raw可展开 |
| Sequences/Progress | 参数与步骤并排；预检逐键比对值/来源 | 单工作面，不JSON巨块，不嵌套卡片 | vars280栏，step表，来源mono12 |
| Login | 可信工具入口，表单焦点 | 仅一块窄表单+产品名，无营销hero | 22title，14form，360wide，16gap |

公共行为调整仅Agent input/raw默认收起；先写失败visibility/disclosure测试，再变更。现有消息游标、viewer权限、按钮状态、预检确认及来源断言保留。纯样式不加镜像测试。
