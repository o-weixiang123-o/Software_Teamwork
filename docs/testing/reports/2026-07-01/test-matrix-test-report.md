# 全局测试矩阵与证据追踪测试报告

## 0. 基本信息

| 项目 | 记录 |
| --- | --- |
| 报告日期 | `2026-07-01` |
| 测试任务 / Issue | `T-001` / `#396` |
| 测试负责人 | `@AKTNL` |
| 协助人员 | 无 |
| 测试范围 | 全局测试矩阵、测试证据记录模板、最终验收汇总模板、测试资料目录入口 |
| 被测分支 | `Test/docs/test-matrix` |
| 被测 commit | 完整验证快照：`0c9fd1d32fd2803044bf833e728009c4af9e68b8`；当前 PR head 仅包含本报告重生成 / 溯源说明修正，详见第 5 节增量复核记录。 |
| Base branch | `upstream/develop @ 8d226de43b9a0bde285bff6b017c3de545e5d037` |
| 测试环境 | 本地 PowerShell / GitHub CLI |
| 结论 | 测试通过 |

## 1. 测试目标

- 验证 #396 要求的全局测试矩阵已覆盖既有测试相关 issue 和 Batch 5 新增测试 issue。
- 验证矩阵区分本地自动化、CI 自动化、env-gated smoke、真实 provider smoke 和人工验收。
- 验证每条测试任务都有主责人、协作对象、状态、依赖、必跑命令、证据位置和残余风险口径。
- 验证新增模板可支持 issue/PR 证据块和最终验收汇总。

本轮不验证各专项 issue 的业务逻辑、前端页面、后端服务、Docker 构建或真实跨服务环境；这些由矩阵中对应专项任务负责。

## 2. 测试依据

| 类型 | 链接或文件 | 使用方式 |
| --- | --- | --- |
| 测试策略 | `docs/testing/strategy.md` | 确认文档变更只需文档一致性和 `git diff --check`，并确认 T-* 报告归档要求。 |
| 测试资料入口 | `docs/testing/README.md` | 确认矩阵、证据块模板和汇总模板入口已登记。 |
| Issue | `#396` | 任务来源、覆盖范围和验收标准。 |
| 依赖 issue | `#377` | 自动化测试分层口径来源。 |
| PR metadata | `#427` | 完整验证快照 base 为 `8d226de43b9a0bde285bff6b017c3de545e5d037`，head 为 `0c9fd1d32fd2803044bf833e728009c4af9e68b8`；后续 report-only amend 的当前 head 以对应 GitHub Actions Head / PR metadata 为准。 |
| GitHub issue 列表 | `#372 #375 #388 #389 #385 #386 #379 #304 #125 #305 #306 #378 #397 #398 #399 #400 #401 #402 #403` | 矩阵覆盖对象和负责人、状态、依赖来源。 |

## 3. 测试范围与不测范围

### 测试范围

- `docs/testing/test-matrix.md`
- `docs/testing/templates/test-evidence-record-template.md`
- `docs/testing/templates/final-acceptance-summary-template.md`
- `docs/testing/README.md`
- `docs/testing/reports/2026-07-01/test-matrix-test-report.md`

### 不测范围

- 前端 `apps/web` 构建、单元测试或 E2E。
- 后端服务 `go test ./...`、migration apply 或真实数据库测试。
- Docker/Compose config、Docker build 或 registry mirror 排障。
- env-gated smoke、真实 provider smoke 或人工演示链路。

### 环境与前置条件

- 依赖服务：无。
- 数据库 / Redis / MinIO / legacy vector index：未启动，不需要。
- 环境变量：无。
- 测试账号或 seed：无。
- 外部 provider 或模型：无。

## 4. 测试用例矩阵

| ID | 分类 | 用例 / 场景 | 预期结果 | 实际结果 | 结论 |
| --- | --- | --- | --- | --- | --- |
| TEST-001 | 文档覆盖 | 矩阵覆盖 #396 要求的既有 issue 和 Batch 5 新增测试 issue。 | 每个 issue 都有行记录，包含负责人、状态、命令、证据和风险。 | 已覆盖 #372、#375、#388、#389、#385、#386、#379、#304、#125、#305、#306、#378、#396 到 #403。 | pass |
| TEST-002 | 分层口径 | 区分 mock/fake、轻量容器、env-gated、真实 provider 和人工验收。 | 口径章节和矩阵行均能区分测试层级。 | `docs/testing/test-matrix.md` 已定义分类并在各行标注。 | pass |
| TEST-003 | 证据模板 | 提供 issue/PR 快速证据块和最终汇总模板。 | 模板包含已运行、未运行、缺陷、证据、结论和风险字段。 | 已新增两个模板，并在 README 与矩阵中引用。 | pass |
| TEST-004 | 空白检查 | 已跟踪改动和新增文件无 trailing whitespace 或 conflict marker。 | `git diff --check` 和新增文件 no-index check 无错误输出。 | 已通过。 | pass |

## 5. 执行命令与结果

| 时间 | ID | 命令或操作 | 结果 | 证据 / 备注 |
| --- | --- | --- | --- | --- |
| `2026-07-01 +0800` | TEST-001 | `gh issue view 396 --repo Sakayori-Iroha-168/Software_Teamwork --json number,title,state,body,labels,assignees,comments,url` | pass | 获取 #396 范围、验收标准和 assignee。 |
| `2026-07-01 +0800` | TEST-001 | `gh issue view <number> --repo Sakayori-Iroha-168/Software_Teamwork --json number,title,state,assignees,labels,url,body` | pass | 批量核对 #372、#375、#388、#389、#385、#386、#379、#304、#125、#305、#306、#378、#396 到 #403。 |
| `2026-07-01 +0800` | TEST-004 | `gh pr view 427 --repo Sakayori-Iroha-168/Software_Teamwork --json baseRefOid,headRefOid,baseRefName,headRefName` | pass | 完整验证快照记录 base `8d226de43b9a0bde285bff6b017c3de545e5d037` / head `0c9fd1d32fd2803044bf833e728009c4af9e68b8`。 |
| `2026-07-01 +0800` | TEST-004 | `git diff --check` | pass | 无输出。 |
| `2026-07-01 +0800` | TEST-004 | `git diff upstream/develop...HEAD --check` | pass | 针对当前 PR diff 复验，无输出。 |
| `2026-07-01 +0800` | TEST-004 | `gh pr checks 427 --repo Sakayori-Iroha-168/Software_Teamwork` | pass | 在 head `0c9fd1d32fd2803044bf833e728009c4af9e68b8` 上 Codex PR Review、commitlint、label 均通过。 |
| `2026-07-01 +0800` | TEST-004 | 覆盖重生成 `docs/testing/reports/2026-07-01/test-matrix-test-report.md` 后执行 `git diff --check` | pass | 本次增量仅修正报告溯源说明，复核无 whitespace 错误。 |
| `2026-07-01 +0800` | TEST-004 | `git diff --no-index --check -- NUL docs/testing/test-matrix.md` | pass | no-index 因新增文件差异返回 1，但无 whitespace 错误输出。 |
| `2026-07-01 +0800` | TEST-004 | `git diff --no-index --check -- NUL docs/testing/templates/test-evidence-record-template.md` | pass | no-index 因新增文件差异返回 1，但无 whitespace 错误输出。 |
| `2026-07-01 +0800` | TEST-004 | `git diff --no-index --check -- NUL docs/testing/templates/final-acceptance-summary-template.md` | pass | no-index 因新增文件差异返回 1，但无 whitespace 错误输出。 |

未运行项：

| 测试项 | 未运行原因 | 缺失环境 | 残余风险 | 后续归属 |
| --- | --- | --- | --- | --- |
| `bun run --cwd apps/web check` / `build` / `test:unit` / `test:e2e` | 本次未修改前端源码或前端依赖。 | 无。 | 不覆盖前端运行时行为；由 #401 和相关前端 PR 验证。 | #401 |
| 后端服务 `go test ./...` / `go build ./cmd/server` | 本次未修改后端服务代码、OpenAPI、migration 或服务配置。 | 无。 | 不覆盖服务运行时行为；由各服务专项 issue 验证。 | #375 #388 #389 #386 #379 #397 #398 #400 #402 |
| Docker/Compose config 或 Docker build | 本次未修改 Dockerfile、Compose、镜像源、deploy 配置或 Docker runbook。 | Docker daemon 未用于本轮验证。 | 不覆盖部署环境；由 #306 #399 #403 验证。 | #306 #399 #403 |
| env-gated smoke / 真实 provider smoke / 人工验收 | 本任务只交付测试治理文档，不启动跨服务环境或外部 provider。 | PostgreSQL、Redis、legacy vector index、MinIO、Parser、真实 provider key、人工演示环境。 | 不证明跨服务业务链路；矩阵已把这些验证分配给 #304 #125 #378 #397 到 #403。 | #304 #125 #378 #397 #398 #399 #400 #401 #402 #403 |

## 6. 缺陷与处理记录

| 问题 | 等级 | 处理结论 | 关联 issue / PR | 说明 |
| --- | --- | --- | --- | --- |
| 无 | - | - | - | 本轮未发现需转 owner issue 的缺陷。 |

## 7. 证据清单

| 证据类型 | 位置 / 链接 | 说明 |
| --- | --- | --- |
| 矩阵文档 | `docs/testing/test-matrix.md` | 全局测试矩阵和维护规则。 |
| 证据块模板 | `docs/testing/templates/test-evidence-record-template.md` | issue/PR 快速证据格式。 |
| 汇总模板 | `docs/testing/templates/final-acceptance-summary-template.md` | 周报或最终验收汇总格式。 |
| 资料入口 | `docs/testing/README.md` | 新文档入口和提交流程。 |
| GitHub issue | `#396` | 任务来源。 |

## 8. 风险与剩余缺口

- 矩阵中 Closed issue 的任务正文状态可能与 GitHub closed 状态不一致，本文明确以 GitHub state 为准。
- 证据路径中部分报告是后续专项测试任务的归档位置，不代表当前已经存在。
- 本任务不运行跨服务 smoke、真实 provider smoke 或人工验收，因此不证明业务链路通过。
- 本报告记录两类证据：矩阵与模板内容的完整验证快照记录具体 base/head；报告文件后续参与 PR amend 时，只记录 report-only 增量复核和对应 GitHub Actions Head / PR metadata，避免把无法自引用的最终 commit SHA 硬编码进报告。

## 9. 最终结论

测试通过：#396 要求的矩阵、证据记录模板、最终汇总模板和维护规则已落地；本轮文档空白检查通过，未运行项和残余风险已记录。

## 10. 复核清单

- [x] 已实际运行测试，而不是只补测试代码或测试清单。
- [x] 已记录执行命令、环境、结果和失败证据。
- [x] 已区分小问题和大问题，并按规则修复或转 issue。
- [x] 已记录所有未运行项的环境缺口和残余风险。
- [ ] 已在测试 issue 和 PR 中链接本报告。
