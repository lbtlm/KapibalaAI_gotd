# Candidate delivery and manual operations

本流程提供本机 Compose 演示和已验收候选的手动推广/回滚。没有生产目标、环境凭据或线上部署授权，GitHub 工作流不会部署。示例账号仅用于受控演示，不适合将控制台暴露到公网。

Compose 的数据库健康依赖与迁移完成依赖按 [Docker 启动顺序规则](https://docs.docker.com/compose/how-tos/startup-order/) 配置；镜像的 gzip 包使用官方支持的 [docker image load](https://docs.docker.com/reference/cli/docker/image/load/) 导入。

## CI and artifacts

`.github/workflows/ci.yml` 在面向 dev/main 的 PR 和显式手动启动时运行，始终保留 `policy` 和固定 required check `CI Gate`。普通 dev/main push 不重复触发 CI。`policy` 验证工作分支目标、PR 标题、非 merge commit 格式，并运行交付脚本测试；main 只接受本仓库 dev 的发布 PR。

| Profile | 选择条件 | 执行内容 |
| --- | --- | --- |
| docs | 合法 PR → dev，且全部变更命中明确文档路径 | policy/脚本测试、Gate |
| light | 合法工作分支 PR → dev 的其他变更 | policy、`backend/internal/domain` 纯 Go unit、前端 unit/typecheck、Gate |
| full | 同仓 dev → main 发布 PR（包括纯文档），或手动选择 `profile=full` | 原有 PostgreSQL/Go/race/vet、前端 unit/type/build、真实服务空账号 E2E、镜像构建/冒烟/原制品打包、Gate |

light 不启动 PostgreSQL、gateway 服务或 Docker，不运行 race、E2E、`pnpm build` 或独立构建命令，也不生成候选；domain 测试不代表 gateway、PG 或恢复链路已通过。前端 unit/typecheck 作业被 light/full 复用，仅 full 执行 build。手动 full 为可选重验，发布 PR 已产出的原候选可直接用于后述准备流程，无需再完整跑一次。

文档路径仅包括根 `README.md`、`CONTRIBUTING.md`、`AGENTS.md`、`CHANGELOG.md` 和 `docs/` 下 `.md`、`.rst`、`.txt`。PR 使用 base/head 的 merge-base 差异并关闭 rename detection，移动代码到文档仍包含原代码删除路径。空差异或 Git diff 暂不可用时至少执行 light；缺少来源/目标/仓库/SHA 元数据或非法分支则 policy 失败，不能落入 docs。普通文档提交不使用 `[skip ci]`；分支保护保持生效。

full 模式的 Go 作业分别向后端和网关提供独立 PostgreSQL 数据库，执行纯逻辑、真实数据库集成、race 和 vet。前端执行 unit/type/build。E2E 通过 `scripts/local-stack.mjs` 启动真实 backend、空账号 Telegram gateway、既有 provider 和 Vite，检查空账号导入入口、布局及 viewer 权限；不注入 Telegram 响应，不使用 Telegram 凭据。原账号/网关替身及故障注入覆盖已退役，当前 CI 不证明真实 Telegram 发送、群同步或外部失败恢复已验收。

固定 `CI Gate` 使用 `always()` 执行，要求 policy 成功且输出明确档位；docs 要求五个测试/制品作业全部 skipped，light 要求 go_light/frontend success 且 go/e2e/artifact skipped，full 要求 go/frontend/e2e/artifact success 且 go_light skipped。每个预期结果必须精确匹配；失败、取消、缺失、未知档位或其他组合继续阻塞。workflow 整体取消不能生成成功 gate，应保持 required check 未通过。档位输出及作业条件按 GitHub 的 [job outputs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/pass-job-outputs) 和 [job conditions](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-jobs-with-conditions) 传递；Gate 仍检查 [needs results](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts)。

full 前置检查全部成功才构建 Docker 镜像。产物作业通过 `compose.ci.yaml` 在专用 `kapibala-ci-<run_id>-<run_attempt>` 项目启动同批镜像，验证 backend/gateway/provider 健康、前端 root/JS 资源、HttpOnly refresh cookie 登录，以及同源 WebSocket 101/有效 token 认证、无效 token 拒绝与跨源 403，然后回收该项目。Nginx `/ws` 转发原始 Host（含端口），保留后端同源和鉴权检查。仅这个 disposable CI 项目执行 `down --volumes --remove-orphans`；该规则不适用于演示或真实数据库。任何启动/冒烟/清理失败均阻止打包。交付包 `candidate-<github.sha>-<run_attempt>` 包含：

- `images.tar.gz`：backend（含 migrate/provider）、gateway、frontend 三个 SHA 标签镜像。
- `compose.yaml`：启动/迁移规则。
- `COMMIT_SHA`：测试和构建的不可变提交 SHA；PR 是 GitHub 测试的 merge SHA。
- `SHA256SUMS`：以上三个文件的校验值。

[upload-artifact v4](https://github.com/actions/upload-artifact) 的 artifact 本体不可原位修改，同一重跑使用不同 run_attempt 名称。候选保存 14 天，过期需对同 SHA 重新完整 CI，不能宣称旧包仍可下载。不要上传源码目录、数据库、Session、`.local/` 或私有配置；CI 的 browser failure artifacts 仅来自无 Telegram 账号的隔离环境。真实模型 opt-in 测试单独运行、单独验收，普通 CI 不传入 API key。

## Manual promotion

维护者应先在相同 candidate SHA 上完成必需功能验收，然后在默认分支的 `Prepare accepted candidate` workflow 提供：成功的同仓 dev → main **发布 PR CI run ID**、原 artifact 中 `COMMIT_SHA` 指定的完整 merge-test SHA（不是 dev HEAD）、deploy 或 rollback purpose。流程核验 run 来源/状态/仓库、该 run 记录的 PR main/dev 分支与仓库 ID、merge commit 两个父提交对应的 base/head，以及 merge tree 等于被测 dev source tree；任一元数据缺失或 tree 不一致时拒绝，需维护者先核清差异。随后按接受的 merge SHA 和原 run_attempt 下载原 artifact，验证 checksum/COMMIT_SHA，记录 tree，再交付 `accepted-<sha>-<purpose>-<run_id>`，不会为了准备包再完整跑一次 CI。

历史成功 dev/main push 候选仍可用于回滚；可选手动 full 在 dev/main 生成的候选也兼容，且接受 SHA 必须等于该 run.head_sha。普通工作 PR → dev 的 light/docs run 没有候选，不能用于准备制品。该 workflow 不会 build、tag、release、部署或改可见性；首次启用 workflow_dispatch 需文件存在于 GitHub 默认分支。PR merge-test SHA 与后续正式合并 SHA应分别记录，不能把 tree 对应关系说成提交 SHA 相同。

操作者只从可信仓库成功 run 下载已验收包，解压到单独目录；SHA256 验证完整性，不替代来源可信性或功能验收。部署脚本不会自动下载候选，不读取账户私有文件。使用专用 Compose project 名，避免接触其他项目；首次启动前在运行机确认端口/项目范围。

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:POSTGRES_PASSWORD='<该项目既有的URI安全密码>'
$env:CONSOLE_PORT='15173'
# Docker未在PATH时可显式设置完整可执行路径
# $env:DOCKER_COMMAND='D:/Applications/Docker/resources/bin/docker.exe'
node scripts/deploy.mjs .local/candidates/<accepted-sha> kapibala-candidate deploy
```

脚本先验证每个文件 checksum 和 40 字符 SHA，加载原镜像，并确认 backend/gateway/frontend 三个预期 SHA 镜像均存在后，才停止该专用项目的旧 backend/provider/gateway。`up` 保留 `--no-build`；显式迁移使用 [`docker compose run --rm --pull never migrate`](https://docs.docker.com/reference/cli/docker/compose/run/)，该子命令不支持 `--no-build`。脚本复用已加载镜像，不执行 build 或 pull，再启动服务并检查通过控制台代理的 `/api/health`。此手动流程有停机窗口；迁移前需完成下述备份。仅健康后将 SHA/mode/time 写入忽略的 `.local/deploy/<project>.json`。健康检查证明进程/代理可用，登录、业务、真实服务仍需对应功能验收。失败保留数据库和现场，操作者查看该 project 日志；没有暗中 reset 或删卷。

## Rollback and stop

每次应用迁移前由操作者备份该项目自己的 PostgreSQL，并记录可恢复的 schema 版本和先前已验收 SHA。备份只存受保护的本地位置，不进 Git/CI。历史候选需提前在本地保留，artifact retention 不是备份策略。

```powershell
# 同一项目、同一数据库，切回已验收的旧镜像；不反向执行迁移。
node scripts/deploy.mjs .local/candidates/<previous-accepted-sha> kapibala-candidate rollback
```

rollback 使用原 SHA 镜像，不 rebuild、不降级/清空 schema、不删除数据。旧 server 会拒绝不兼容的 schema，不能绕过检查；这时先评估迁移兼容性，再按已验证备份恢复到**独立**数据库并人工验证，不直接覆盖当前数据库。当前初版还没有两个生产版本的升级/回滚实测证据。

停止专用容器栈，保留数据：

```powershell
docker compose -f .local/candidates/<accepted-sha>/compose.yaml -p kapibala-candidate down
```

不要添加 `--volumes`，也不要运行无 project 范围的清理命令。原生 `local-stack` 使用 Ctrl+C；Playwright 启动的栈由 Playwright teardown 回收。端口冲突必须由拥有该服务的人处理，脚本不会停止既有监听进程。

## Secrets and real modes

根 `.env` 与 `.local/` 被 Git 忽略。Docker 网关固定为 Telegram，首次启动没有账号；从页面上传真实 Session+JSON，再明确连接/检测。`gateway-db` 一次性服务只创建缺失的 `kapibala_telegram_gateway`，后端继续使用 `kapibala`。手动部署识别候选的 `gateway-db` 服务并先完成初始化，旧候选和回滚不强制加入该步骤。不要把个人身份资料或私有 Session 放入 backend/gateway/frontend build contexts。所有私有配置保存在根 `.local/` 或外部受控路径，构建产物和日志不包含这些文件。

后端 access/refresh 权限、gateway writer lease、运行恢复的业务验收由组件 TDD 与独立审查提供；运维完整性检查不替代它们。

旧 manifest 环境升级前必须停止网关并备份后端与网关两个数据库，按 [网关说明](../gateway/README.md) 的离线 adopt 命令登记已有凭据。该命令不登录 Telegram，不覆盖最新 Session，不重置事件游标。正常启动只读取持久化登记；缺失或不完整凭据不能当作有效账号连接。旧模拟账号清理须核对凭据、真实验证来源和业务引用，禁止按账号名称批量删除。模型 provider 的既有模式独立配置，本次账号整改未扩展到 provider 产品行为。

本次启动器与部署脚本回归：缺失独立网关 DSN、首次部署未初始化网关库均先复现失败，再修复通过。部署脚本保留原镜像校验、迁移失败保护和回滚不迁移行为；这些脚本检查不替代实际容器启动验收。
