# Candidate delivery and manual operations

本流程提供本机 Compose 演示和已验收候选的手动推广/回滚。没有生产目标、环境凭据或线上部署授权，GitHub 工作流不会部署。示例账号仅用于受控演示，不适合将控制台暴露到公网。

Compose 的数据库健康依赖与迁移完成依赖按 [Docker 启动顺序规则](https://docs.docker.com/compose/how-tos/startup-order/) 配置；镜像的 gzip 包使用官方支持的 [docker image load](https://docs.docker.com/reference/cli/docker/image/load/) 导入。

## CI and artifacts

`.github/workflows/ci.yml` 在面向 dev/main 的 PR、dev/main push、手动启动时运行，始终保留 `policy` 和固定 required check `CI Gate`。`policy` 验证工作分支目标、PR 标题和非 merge commit 格式；main 只接受本仓库 dev 的推广 PR。它还运行交付脚本测试，并从真实 Git diff 判定 docs/full 模式：仅根 `README.md`、`CONTRIBUTING.md`、`AGENTS.md`、`CHANGELOG.md` 和 `docs/` 下的 `.md`、`.rst`、`.txt` 路径全部命中时，跳过 Go、frontend、E2E、images 四个昂贵作业。PR 使用 base/head 的 merge-base 差异，push 使用 before/after 差异；关闭 rename detection，移动代码到文档仍包含原代码的删除路径。空差异、未知或缺失基线、手动启动及任何代码/脚本/依赖/配置/工作流变更均执行 full 模式。普通文档提交不使用 `[skip ci]`；轻量模式不会取消分支保护，也不会让 Gate 永久 pending。

full 模式的 Go 作业明确提供真实 PostgreSQL DSN，并在另一数据库 `kapibala_gateway_integration` 启动生产 fake gateway，通过 `GATEWAY_INTEGRATION_URL` 运行实际群生命周期组合测试；gateway writer lease 测试使用 `kapibala_test`，互不争夺同库 writer。作业执行单元/集成、race、vet，并回收自有 gateway PID。前端作业执行 unit/type/build。E2E 使用独立 PostgreSQL、真实 backend、fake gateway、scripted provider 和 Vite。

固定 `CI Gate` 使用 `always()` 执行：full 模式要求 policy/go/frontend/e2e/artifact 全部 result=success；只有 policy 成功且输出明确为 docs 时，才接受其余四项恰好为 skipped。失败、取消、缺失和其他组合继续阻塞。workflow 整体取消不能生成成功 gate，应保持 required check 未通过。分类输出及作业条件按 GitHub 的 [job outputs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/pass-job-outputs) 和 [job conditions](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-jobs-with-conditions) 传递；Gate 仍检查 [needs results](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts)。

前置检查全部成功才构建 Docker 镜像。产物作业通过 `compose.ci.yaml` 在专用 `kapibala-ci-<run_id>-<run_attempt>` 项目启动同批镜像，验证 backend/gateway/provider 健康、前端 root/JS 资源、HttpOnly refresh cookie 登录，以及同源 WebSocket 101/有效 token 认证、无效 token 拒绝与跨源 403，然后回收该项目。Nginx `/ws` 转发原始 Host（含端口），保留后端同源和鉴权检查。仅这个 disposable CI 项目执行 `down --volumes --remove-orphans`；该规则不适用于演示或真实数据库。任何启动/冒烟/清理失败均阻止打包。交付包 `candidate-<github.sha>-<run_attempt>` 包含：

- `images.tar.gz`：backend（含 migrate/provider）、gateway、frontend 三个 SHA 标签镜像。
- `compose.yaml`：启动/迁移规则。
- `COMMIT_SHA`：测试和构建的不可变提交 SHA；PR 是 GitHub 测试的 merge SHA。
- `SHA256SUMS`：以上三个文件的校验值。

[upload-artifact v4](https://github.com/actions/upload-artifact) 的 artifact 本体不可原位修改，同一重跑使用不同 run_attempt 名称。候选保存 14 天，过期需对同 SHA 重新完整 CI，不能宣称旧包仍可下载。不要上传源码目录、数据库、Session、`.local/` 或私有配置；CI 的 browser failure artifacts 仅来自 fake 数据。真实模型 opt-in 测试单独运行、单独验收，普通 CI 不传入 API key。

## Manual promotion

维护者应先在相同 candidate SHA 上完成必需功能验收，然后在默认分支的 `Prepare accepted candidate` workflow 提供：成功的 CI **push** run ID（dev/main）、明确验收的完整 SHA、deploy 或 rollback purpose。该 workflow 检查 run 的来源、分支、状态和 SHA，下载同 run/attempt 的原候选，验证 SHA256，再交付 `accepted-<sha>-<purpose>-<run_id>`。它不会 build、tag、release、部署、改可见性。首次启用 workflow_dispatch 需该文件存在于 GitHub 默认分支。

操作者只从可信仓库成功 run 下载已验收包，解压到单独目录；SHA256 验证完整性，不替代来源可信性或功能验收。部署脚本不会自动下载候选，不读取账户私有文件。使用专用 Compose project 名，避免接触其他项目；首次启动前在运行机确认端口/项目范围。

```powershell
Set-Location D:\web_projects\kapibala_ai
$env:POSTGRES_PASSWORD='<该项目既有的URI安全密码>'
$env:CONSOLE_PORT='15173'
# Docker未在PATH时可显式设置完整可执行路径
# $env:DOCKER_COMMAND='D:/Applications/Docker/resources/bin/docker.exe'
node scripts/deploy.mjs .local/candidates/<accepted-sha> kapibala-candidate deploy
```

脚本先验证每个文件 checksum 和 40 字符 SHA，加载原镜像，停止该专用项目的旧 backend/provider/gateway，`--no-build` 启动 PostgreSQL、执行显式迁移并启动服务，随后检查通过控制台代理的 `/api/health`。此手动流程有停机窗口；迁移前需完成下述备份。仅健康后将 SHA/mode/time 写入忽略的 `.local/deploy/<project>.json`。健康检查证明进程/代理可用，登录、业务、真实服务仍需对应功能验收。失败保留数据库和现场，操作者查看该 project 日志；没有暗中 reset 或删卷。

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

不要添加 `--volumes`，也不要运行无 project 范围的清理命令。原生 fake 栈使用 Ctrl+C；Playwright 启动的栈由 Playwright teardown 回收。端口冲突必须由拥有该服务的人处理，脚本不会停止既有监听进程。

## Secrets and real modes

根 `.env` 与 `.local/` 被 Git 忽略。Docker 示例只用 fake/scripted，真实模式需人工在已授权环境配置进程/秘密挂载和独立数据库；当前 Compose 不自动消费 Telegram manifest 或模型 key。不要把个人身份资料或私有 Session 放入 backend/gateway/frontend build contexts。所有私有配置保存在根 `.local/` 或外部受控路径，构建产物和日志不包含这些文件。

后端 access/refresh 权限、gateway writer lease、运行恢复的业务验收由组件 TDD 与独立审查提供；运维完整性检查不替代它们。
