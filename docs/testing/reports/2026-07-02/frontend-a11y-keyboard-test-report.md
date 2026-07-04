# 前端无障碍与键盘导航轻量回归测试报告

## 0. 基本信息

| 项目 | 记录 |
| --- | --- |
| 报告日期 | `2026-07-02` |
| 测试任务 / Issue | `T-010` / [#454](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/454) |
| 测试负责人 | `@AKTNL` |
| 协助人员 | 无 |
| 测试范围 | 登录页、AppShell 主导航、QA 检索测试表单、报告模板结构弹窗、现有前端 mock-backed E2E smoke |
| 被测分支 | `Test/test/frontend-a11y-keyboard-smoke` |
| 被测 commit | PR #460 当前 head；最终 head SHA 以 GitHub Actions / PR Checks 为准。代码变更的本地 clean worktree 复核基线见第 5 节 `158f7c54d0af490c6d32bd6813583bd0c14ee465` 记录 |
| Base branch | `upstream/develop @ 05fbeffc` |
| 测试环境 | Windows / PowerShell / Bun `1.3.14` / Playwright Chromium，本地未启动真实 Gateway |
| 结论 | 测试通过；干净临时 worktree 的 `bun run --cwd apps/web check` 已通过。原 Windows 工作树因既有 CRLF 行尾文件导致 `format:check` 失败，变更范围 Prettier check、build、unit、mock-backed E2E 均通过 |

## 1. 测试目标

- 验证登录页字段、错误提示和提交按钮具备基础语义，并可通过 Tab/Enter 完成登录 smoke。
- 验证 AppShell 顶部主导航链接有可访问名称，可通过 Tab 聚焦并通过 Enter 触发导航。
- 验证 QA 检索测试表单的 Query、Top K、rerank checkbox 等控件有可访问名称，且可用键盘填写、切换和提交。
- 验证报告模板结构弹窗可通过键盘打开，具备 dialog 标题语义，并可用 Escape 关闭。
- 明确本轮只验证前端 mock-backed / RTL 层可操作性，不验证真实后端业务链路。

## 2. 测试依据

| 类型 | 文件或链接 | 使用方式 |
| --- | --- | --- |
| 测试任务 | [#454](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/454) | 范围、交付物、验收标准 |
| 测试策略 | `docs/testing/README.md`、`docs/testing/strategy.md`、`docs/testing/test-matrix.md` | 命令选择、报告归档、mock/local Gateway/人工结果区分 |
| 前端规范 | `docs/collaboration/frontend-workflow.md`、`.trellis/spec/frontend/quality-guidelines.md` | Bun 命令、检查范围、RTL/Playwright 边界 |
| 报告模板 | `docs/testing/templates/test-report-template.md` | 报告结构 |

## 3. 测试范围与不测范围

### 测试范围

- React Testing Library:
  - `LoginPage` 键盘登录、label 绑定和错误 `role="alert"`。
  - `AppLayout` 主导航 Tab 顺序、链接可访问名称和 Enter 激活。
  - `QARetrievalTestPage` 表单字段 label、checkbox 键盘切换和提交 payload。
  - `ReportTemplatesPage` 结构弹窗 Enter 打开、dialog 标题和 Escape 关闭。
- Playwright:
  - 现有 `app-smoke.spec.ts` 和 `critical-flows.spec.ts` mock-backed E2E smoke。

### 不测范围

- 不启动真实 Gateway/Auth/QA/Knowledge/Document/File 服务。
- 不验证真实数据库、Redis、对象存储、legacy vector index、Parser 或 AI provider。
- 不做完整 WCAG 审计、视觉像素验收或真实后端端到端业务链路。

### 环境与前置条件

- 依赖服务：无真实后端依赖；E2E 使用 Playwright route mock。
- 数据库 / Redis / MinIO / legacy vector index：未启动。
- 环境变量：使用前端测试默认 `VITE_API_BASE_URL=http://127.0.0.1/api/v1`。
- 测试账号或 seed：E2E 内 mock 用户 `operator`；无真实账号。
- 外部 provider 或模型：未使用。

## 4. 测试用例矩阵

| ID | 分类 | 场景 | 预期结果 | 实际结果 | 结论 |
| --- | --- | --- | --- | --- | --- |
| A11Y-001 | RTL / 登录页 | Tab 到用户名、密码、提交按钮，Enter 提交 | 字段可聚焦，提交调用 Gateway session mock，错误提示具备 alert 语义 | focused 和完整 unit 均通过 | pass |
| A11Y-002 | RTL / AppShell | Tab 遍历主导航，Enter 激活报告入口 | 导航链接有可访问名称，Enter 触发 `/reports` 导航 | focused 和完整 unit 均通过 | pass |
| A11Y-003 | RTL / 表单 | QA 检索表单键盘填写 Query/KB，空格切换 rerank，Enter 提交 | payload 包含 question、knowledgeBaseIds、retrieval 参数 | focused 和完整 unit 均通过 | pass |
| A11Y-004 | RTL / Dialog | 报告模板结构按钮 Enter 打开，Escape 关闭 | 弹窗具备 dialog 标题语义，并可关闭 | focused 和完整 unit 均通过 | pass |
| E2E-001 | Playwright / mock-backed | 现有登录、匿名跳转、文档上传、QA SSE、报告生成 smoke | 6 个 Chromium 用例通过 | 6 passed | pass |
| ENV-001 | local Gateway-backed | 真实 Gateway 链路 | 本任务不依赖真实后端，应记录未运行原因 | 未运行，见未运行项 | skipped |
| ENV-002 | 纯人工检查 | 手工逐页 smoke | 本轮用自动化覆盖轻量键盘/a11y smoke | 未运行，见未运行项 | skipped |

## 5. 执行命令与结果

| 时间 | ID | 命令或操作 | 结果 | 证据 / 备注 |
| --- | --- | --- | --- | --- |
| `2026-07-02 11:42 +0800` | A11Y-001..004 | `bun run --cwd apps/web test:unit -- src/pages/login/page.test.tsx src/layouts/app-layout.a11y.test.tsx src/pages/admin/qa-retrieval-test.a11y.test.tsx src/pages/reports/templates/page.test.tsx` | pass | 4 files passed, 8 tests passed |
| `2026-07-02 11:55 +0800` | setup | `bun install --frozen-lockfile` | pass | 559 installs checked, no changes |
| `2026-07-02 11:55 +0800` | frontend-check | `bun run --cwd apps/web check` | fail | `typecheck`、`typecheck:test`、`lint` passed；`format:check` failed on 38 pre-existing working-tree CRLF files such as `src/main.tsx` |
| `2026-07-02 11:58 +0800` | frontend-check-clean | clean temp worktree: `bun install --frozen-lockfile` + `bun run --cwd apps/web check` | pass | Fresh checkout of this PR branch at `158f7c54d0af490c6d32bd6813583bd0c14ee465`; all matched files use Prettier code style |
| `2026-07-02 12:05 +0800` | ci-frontend | GitHub Actions `Frontend / test` on PR #460 | pass | Workflow run `28564506413`, job `84688921213`; commit `158f7c54d0af490c6d32bd6813583bd0c14ee465` |
| `2026-07-02 11:46 +0800` | frontend-check-scope | `prettier --check <changed frontend files>` | pass | All changed files use Prettier code style |
| `2026-07-02 11:55 +0800` | build | `bun run --cwd apps/web build` | pass | Vite build passed；仅有 chunk size warning |
| `2026-07-02 11:55 +0800` | unit | `bun run --cwd apps/web test:unit` | pass | 22 files passed, 75 tests passed |
| `2026-07-02 11:56 +0800` | E2E-001 | `bun run --cwd apps/web test:e2e` | pass | 6 Chromium tests passed；mock-backed only |
| `2026-07-02 11:56 +0800` | whitespace | `git diff --check` | pass | 无空白错误 |

未运行项：

| 测试项 | 未运行原因 | 缺失环境 | 残余风险 | 后续归属 |
| --- | --- | --- | --- | --- |
| local Gateway-backed E2E | 本任务范围声明不接入真实后端，且未启动本地 Gateway/Auth/QA/Knowledge 等服务 | Gateway、Auth、Redis、QA、Knowledge、Document/File 等本地联调栈 | 不能证明真实后端链路、鉴权上下文、真实数据和 request id 行为 | #401 / #125 / 后续真实 E2E 任务 |
| 纯人工 smoke | 本轮已补 RTL 键盘 smoke 并运行 mock-backed Playwright，不做重复人工验收 | 人工演示环境、截图/录屏 | 不覆盖人工读屏器体验或视觉顺序细节 | #403 最终人工验收 |
| 原工作树 full `check` | Windows 工作树已有 CRLF 行尾文件导致 `format:check` 失败；本任务不批量改 38 个未触碰文件 | 干净 LF checkout 或单独格式基线修复 | 原工作树本地 full check 会继续在既有文件上失败；干净临时 worktree 已验证 `check` 通过 | 前端格式基线/协作环境后续整理 |

## 6. 缺陷与处理记录

| 问题 | 等级 | 处理结论 | 关联 issue / PR | 说明 |
| --- | --- | --- | --- | --- |
| 登录页错误提示没有显式 alert 语义 | 小问题 | 已修复 | #454 | 给错误显示容器补 `role="alert"`，并用 RTL 覆盖空表单错误语义 |
| AppShell 顶部导航区域没有明确导航名称 | 小问题 | 已修复 | #454 | 给顶栏 `nav` 补 `aria-label="主导航"`，并用 RTL 覆盖键盘聚焦和 Enter 激活 |
| Windows 工作树既有 CRLF 行尾触发原工作树 `format:check` 失败 | 小问题 / 环境基线 | 暂不在本任务批量修改 | #454 | 失败文件为未触碰的既有前端文件；变更范围 Prettier check 通过，干净临时 worktree full `check` 通过，避免引入大面积无关行尾 diff |

## 7. 证据清单

| 证据类型 | 位置 / 链接 | 说明 |
| --- | --- | --- |
| 自动化测试代码 | `apps/web/src/pages/login/page.test.tsx`、`apps/web/src/layouts/app-layout.a11y.test.tsx`、`apps/web/src/pages/admin/qa-retrieval-test.a11y.test.tsx`、`apps/web/src/pages/reports/templates/page.test.tsx` | RTL 键盘与语义 smoke |
| 实现修复 | `apps/web/src/pages/login/page.tsx`、`apps/web/src/layouts/app-layout.tsx` | alert 语义和主导航名称 |
| E2E 证据 | `bun run --cwd apps/web test:e2e` 输出 | 6 个 mock-backed Playwright 用例通过；无失败截图或 trace |
| 报告 | `docs/testing/reports/2026-07-02/frontend-a11y-keyboard-test-report.md` | 本报告 |

## 8. 风险与剩余缺口

- mock-backed E2E 不等价于真实 Gateway-backed 验收；真实链路仍需 #401/#125 等任务覆盖。
- 本轮未执行人工读屏器检查，不能替代完整 WCAG 或人工验收。
- Windows 原工作树的既有 CRLF 行尾会让 full `format:check` 本地失败；干净临时 worktree full `check` 已通过，本任务未批量重写未触碰文件。

## 9. 最终结论

测试通过：登录页、AppShell 主导航、QA 检索表单、报告模板弹窗的轻量键盘与基础语义 smoke 已有自动化覆盖；完整 unit、build、mock-backed E2E 通过。未运行的 local Gateway-backed 和纯人工检查已记录边界与残余风险。

## 10. 复核清单

- [x] 已实际运行测试，而不是只补测试代码或测试清单。
- [x] 已记录执行命令、环境、结果和失败证据。
- [x] 已区分小问题并在本任务内修复。
- [x] 已记录未运行项的环境缺口和残余风险。
- [x] 已在测试 issue / PR 可链接本报告。
