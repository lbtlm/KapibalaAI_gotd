# Development workflow

保持维护者的开发习惯：`main` 是稳定主分支，`dev` 是开发集成分支。业务、修复、文档和 CI 变更均从 `dev` 拉工作分支，通过 PR 合入 `dev`。

```text
dev → feat/* | fix/* | hotfix/* | docs/* | test/* | refactor/* | chore/* | ci/*
                       ↓ PR + checks + review
                      dev
                       ↓ release PR after acceptance
                      main
```

## Start a change

在没有其他员工共享写入的独立 checkout 中执行：

```powershell
git fetch origin
git switch dev
git pull --ff-only origin dev
git switch -c feat/your-topic
```

多个员工共享工作区时，由主控统一切分支、选择文件提交与推送；员工只修改自己负责的目录。

## Commits and pull requests

- 提交格式：`type(scope): observable change`。常用类型：`feat`、`fix`、`hotfix`、`docs`、`test`、`refactor`、`chore`、`ci`。
- 一次提交围绕一个可解释的行为变化，避免混入其他员工未完成的文件。
- 工作分支的 PR 目标必须是 `dev`，`hotfix/*` 也遵守此规则。
- PR 说明问题、变化后的行为、复用来源、红绿测试证据和仍未验证的外部依赖；不得包含密钥、Session 或真实聊天内容。
- 测试、构建和 PR 目标检查通过后完成审查与合入。
- `dev` → `main` 通过单独发布 PR 完成。可见性变更、标签、公开 Release 和生产部署遵循对应授权。

## Test-driven development

按公共契约逐片实现：先运行会失败的行为测试，再复用或实现代码使其通过，最后整理重复。集成测试使用真实 PostgreSQL；E2E 使用真实前端、后端、模拟网关与脚本 Agent。真实 Telegram 与 LLM 验收单独记录，缺少凭据不能记作通过。

空仓库仅用一个不含业务代码的空提交建立 `main` 和 `dev`，此后遵守以上流程。

## Remote enforcement

2026-10-14 已核实 GitHub 默认分支为 `main`，`main` 和 `dev` 均启用 PR 必须、`CI Gate` 必须、管理员同样受约束、禁止强推/删除及要求解决审查讨论。PR 目标分支和提交格式由 CI 检查；保护规则不会替代应用功能验收。
