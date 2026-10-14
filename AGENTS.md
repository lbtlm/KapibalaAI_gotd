# Repository collaboration rules

## Branches and pull requests

- `main` is the stable branch. `dev` is the development integration branch.
- Start development branches from current `dev`: `feat/<topic>`, `fix/<topic>`, `hotfix/<topic>`, `docs/<topic>`, `test/<topic>`, `refactor/<topic>`, `chore/<topic>` or `ci/<topic>`.
- Every development change, including hotfixes and documentation, must go through a pull request targeting `dev`. Do not directly push changes to `main` or `dev`.
- Promotion from `dev` to `main` is a separate release pull request after acceptance. Do not bypass `dev` with a feature or hotfix PR to `main`.
- The only bootstrap exception is the single empty initial commit needed to establish both branches in this previously empty repository. It contains no application code.
- Use focused Conventional Commits, for example `feat(gateway): persist replayable Telegram events`. Describe observable behavior and test evidence in the PR.
- Do not change repository visibility, create release tags, publish a release or deploy to production without the corresponding user authorization.

## Execution and ownership

- The controller owns integration, branch changes, commits/push, PRs and final acceptance. Workers in this shared checkout must not switch branches, commit or push independently.
- Read `docs/implementation-contract.md` and the normative examination document before implementing. Respect ownership boundaries and preserve other workers' changes.
- Follow TDD: run a meaningful failing behavior test, implement the smallest correct slice, then verify it passes. Record concise red/green evidence in the component TDD document.
- Reuse and adapt the user's existing modules before adding implementations. Record source repository, commit and path. Do not copy credentials, live databases, backups or unrelated features.
- Authentication, persistence, concurrency and crash recovery require independent review of a stable snapshot and relevant integration evidence.
- Keep `.local/`, API keys, Sessions, account manifests and live-message content out of Git, logs and CI artifacts. Real tests use only designated accounts and test groups.

## Code discovery and scope

If a repository has `.codegraph/`, use CodeGraph before text search or file reads to locate and understand code. Otherwise skip CodeGraph; indexing requires the user's decision.

State consequential assumptions, implement only requested behavior, change only affected paths, and verify observable contracts. A worker submission is not final acceptance. Distinguish local tests, remote CI, real Telegram tests and real LLM tests in reports.
