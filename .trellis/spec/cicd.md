# CI/CD Guidelines

> GitHub Actions and local infrastructure Compose rules for this monorepo.

---

## Overview

This repository uses GitHub Actions for pull request checks and deployment
automation. Existing collaboration workflows protect contribution rules. Product
CI/CD should be added around the confirmed monorepo layout:

```text
apps/web/
services/gateway/
services/auth/
services/file/
services/qa/
services/knowledge/
services/knowledge-runtime/
services/document/
services/ai-gateway/
deploy/docker-compose.yml
```

Current Docker target: local infrastructure Compose only. Business services and
the RAGFlow Knowledge runtime API/worker run on the host. Local Elasticsearch is
the only optional Compose profile service, used as Knowledge runtime
infrastructure when explicitly enabled from local env.

---

## Existing Guard Workflows

These workflows already exist and must remain separate from product build jobs:

| Workflow | File | Purpose |
|----------|------|---------|
| Auto Label | `.github/workflows/auto-label.yml` | Applies team/path labels and syncs PR `blocked` label from linked issues |
| PR Guard | `.github/workflows/pr-guard.yml` | Enforces fork + PR collaboration rules and allowed base branches |
| Commitlint | `.github/workflows/commitlint.yml` | Enforces Conventional Commits on PR commits |
| Task Issue Sync | `.github/workflows/task-issue-sync.yml` | Syncs managed task issues into the GitHub Project |
| Task Claim | `.github/workflows/task-claim.yml` | Handles task claim comments and actual-hours comments |

Do not weaken collaboration checks when adding product CI.

## Task Issue Project Sync Contract

### 1. Scope / Trigger

Update this contract when changing `.github/ISSUE_TEMPLATE/issue.md`,
`.github/ISSUE_TEMPLATE/test_issue.md`, `.github/workflows/task-issue-sync.yml`,
`.github/workflows/task-claim.yml`, task issue workflow docs, or GitHub Project
custom fields.

### 2. Signatures

- Managed task title: `[A-001] ...`, `[B-001] ...`, `[C-001] ...`,
  `[F-001] ...`, `[S-001] ...`, or `[T-001] ...`.
- Task templates: `.github/ISSUE_TEMPLATE/issue.md` for ordinary managed tasks;
  `.github/ISSUE_TEMPLATE/test_issue.md` for `T-*` testing tasks.
- Project marker: `GitHub Project：Software Teamwork`.
- Issue body fields read by automation include `状态`, `主责小组`, `优先级`,
  `批次`, `模块`, `预期工时（小时数）`, `实际工时（小时数）`, `Risk`,
  `依赖任务`, `阻塞任务`, and `Project sync`.
- Project fields written include `Status`, `Group`, `Priority`, `Batch`,
  `Module`, `Risk`, `Dependency`, `ExpectedHours`, `ActualHours`, and
  `OwnerNote`.
- Claim command: `认领：@<github-login>`.
- Actual-hours command: `实际工时：<hour-number>`.

### 3. Contracts

- Task Issue Sync must skip non-managed issues and issues without the
  `Software Teamwork` Project marker.
- `Group` derives from the task title prefix, not from mutable issue body text.
- `T-*` maps to Project `Group` option `Test`; use it only for testing
  documentation, test code, test reports, and test support work. Bugs or
  optimization requests found during testing but owned by development still use
  the matching development group prefix.
- `T-*` issues should be created from `test_issue.md` or otherwise include the
  testing execution and defect-handling rules. Testing task owners must run the
  tests and record commands, environment, results, and failure evidence. Small
  issues may be fixed inside the testing PR when scoped and verified; larger
  owner-service, contract, data-model, migration, security, product, architecture,
  or cross-module issues must become separate owner-group issues and be linked
  from the testing task.
- Every `T-*` issue must require reviewable test evidence. Pure unit,
  component, and static-check automation may use lightweight issue/PR execution
  records; integration, E2E, permission/security boundary, file/parser boundary,
  migration, environment acceptance, manual acceptance, regression, and defect
  reproduction tasks must require a completed report based on
  `docs/testing/templates/test-report-template.md`, archived under
  `docs/testing/reports/YYYY-MM-DD/`, and linked from the testing issue or PR.
- Missing or zero `预期工时（小时数）` is allowed only while the managed issue
  status is `Draft`; non-Draft task issues must provide a positive estimate.
- Missing `实际工时（小时数）` defaults to numeric `0`.
- Hour fields must be non-negative hour numbers without units. Floating-point
  values are allowed, e.g. `0`, `0.5`, `1.25`.
- GitHub Project hour fields should be Number fields for statistics. Workflow
  code may write a text fallback only while the remote Project still has legacy
  Text fields.
- GitHub Project field names are exact: `ExpectedHours` and `ActualHours`.
- Claim comments must keep existing claim validation, set `Status` to
  `In Progress`, and refresh both hour fields in the Project. Because claim
  changes the effective status to non-`Draft`, it must reject missing, `0`,
  `待估`, or `未填写` expected hours before assigning or syncing Project fields.
- Closing a managed issue must calculate `ActualHours` automatically from the
  later of issue `created_at` and the latest closed dependency in `依赖任务`, to
  the current issue `closed_at`, then update the issue body and Project
  `ActualHours`; closed GitHub issues must sync Project `Status` to `Done`
  regardless of stale body `状态` text.
- Maintainers may rerun Task Issue Sync with `workflow_dispatch` and
  `issue_number` to backfill a closed managed issue without reopening it.
- Actual-hours comments must update the issue body `实际工时（小时数）` field
  and sync Project `ActualHours`; they must not require the task to be claimable,
  must remain able to override an automatically generated value, and must still
  enforce positive expected hours for non-`Draft` tasks.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Issue title is not a managed task title | Skip without mutating the issue or Project. |
| `预期工时（小时数）` is missing, zero, `待估`, or `未填写` on a non-`Draft` issue | Set `Project sync` to `blocked` and fail the workflow run. |
| Claim would move an issue to `In Progress` while `预期工时（小时数）` is missing, zero, `待估`, or `未填写` | Reject with an issue comment and do not assign or mutate Project fields. |
| Actual-hours comment targets a non-`Draft` issue while `预期工时（小时数）` is missing, zero, `待估`, or `未填写` | Reject with an issue comment and do not mutate fields. |
| `预期工时（小时数）` is missing on a `Draft` issue | Sync `ExpectedHours` as `0`. |
| `实际工时（小时数）` is missing | Sync `ActualHours` as `0`. |
| Managed issue is closed | Calculate and write `ActualHours` from the later of issue creation and latest closed dependency to issue close time. |
| Comment is `实际工时：` with an empty or non-numeric value | Reject with an issue comment and do not mutate fields. |
| Commenter is not trusted or assigned | Reject actual-hours update with an issue comment. |
| Project lacks `ExpectedHours` or `ActualHours` | Set `Project sync` to `blocked` and fail the workflow run. |
| Claim target differs from commenter | Reject the claim with an issue comment. |

### 5. Good/Base/Bad Cases

- Good: a managed task body has `预期工时（小时数）：1.5`; a maintainer comments
  `实际工时：0.5`; the body and Project `ActualHours` both become numeric `0.5`.
- Base: a new managed task omits hour fields; Project receives `ExpectedHours`
  as `0` and `ActualHours` as `0`.
- Bad: the workflow writes `ActualHours` only during issue create/edit and
  ignores `实际工时：2` comments.

### 6. Tests Required

- Run `actionlint` for changed workflow files.
- Parse changed workflow YAML.
- Extract embedded `actions/github-script` bodies and run `node --check` inside
  an async wrapper.
- Query GitHub Project fields and confirm `ExpectedHours` and `ActualHours` are
  present as Number fields before relying on statistics.
- For changed issue templates, run `git diff --check` and manually verify that
  the required task fields, Project marker, hour fields, and claim rules remain
  present.
- Run `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```js
await updateText('ActualHours', actualHours);
```

This is wrong if the Project field is a Number field, or if the workflow never
reads `实际工时（小时数）` from the issue body or never handles actual-hours comments.

#### Correct

```js
const expectedHours = parseHourNumber(readHourField('预期工时'), '预期工时（小时数）');
const actualHours = parseHourNumber(readHourField('实际工时'), '实际工时（小时数）');
await updateNumberOrText('ExpectedHours', expectedHours);
await updateNumberOrText('ActualHours', actualHours);
```

## Current CI Status

Use `docs/testing/strategy.md` as the authority for current CI coverage and
required-check candidates. As of the current docs baseline:

- Current CI covers collaboration guardrails, Go service tests/builds, goose
  migration apply, frontend check/build/unit/E2E smoke, Docker/Compose config
  checks, Gateway contract drift, and frontend Gateway API type drift.
- The best required-check candidates are frontend check/build, Go service tests,
  goose migration apply, Docker/Compose config, Gateway contract/API drift, and
  API type drift.
- Full DB integration test jobs and backend cross-service E2E smoke are gaps
  until stable workflows and dependencies land.
- Open PRs, draft issues, and unmerged capabilities must be documented as
  pending/follow-up, not as current `develop` behavior.

When editing workflow specs, keep the current-vs-target distinction explicit:
current CI coverage, PR-before local recommendations, and future gaps are not
interchangeable.

---

## Auto Label Service Path Contract

### 1. Scope / Trigger

Update this contract when changing `.github/labeler.json` service labels,
service directory layout, or service documentation layout.

### 2. Signatures

- Workflow file: `.github/workflows/auto-label.yml`
- Config file: `.github/labeler.json`
- Rule section: `pathLabels[]`
- Rule shape: `{ "paths": string[], "labels": string[] }`

### 3. Contracts

Each service label must cover its implementation path and, when a dedicated
service documentation path exists, that documentation path:

| Label | Required paths |
|-------|----------------|
| `service:gateway` | `services/gateway/**`, `docs/services/gateway/**` |
| `service:auth` | `services/auth/**`, `docs/services/auth/**` |
| `service:file` | `services/file/**`, `docs/services/file/**` |
| `service:qa` | `services/qa/**`, `docs/services/qa/**` |
| `service:knowledge` | `services/knowledge/**`, `docs/services/knowledge/**` |
| `service:knowledge-runner` | `services/knowledge-runtime/**` |
| `service:document` | `services/document/**`, `docs/services/document/**` |
| `service:ai-gateway` | `services/ai-gateway/**`, `docs/services/ai-gateway/**` |

All labels referenced by `.github/labeler.json` must exist in the GitHub
repository. The workflow skips missing labels rather than failing the PR, so
local changes must verify remote label existence when adding a new label name.

### 4. Validation & Error Matrix

| Condition | Required handling |
|-----------|-------------------|
| `.github/labeler.json` is invalid JSON | Fix before commit; Auto Label would fail at runtime. |
| Referenced label does not exist remotely | Create the label or remove the rule before PR. |
| Service implementation path changes | Update the matching docs path rule in the same PR. |
| Service documentation path changes | Update the matching implementation path rule in the same PR. |

### 5. Good/Base/Bad Cases

- Good: `docs/services/knowledge/README.md` matches `documentation` and
  `service:knowledge`.
- Base: `services/knowledge/internal/service/service.go` matches `backend` and
  `service:knowledge`.
- Base: `services/knowledge-runtime/api/apps/runtime_app.py` matches `backend`
  and `service:knowledge-runner`.
- Bad: `docs/services/knowledge/README.md` matches only `documentation`.

### 6. Tests Required

- Parse `.github/labeler.json` as JSON.
- Run a local matcher using the same glob conversion as `auto-label.yml` for at
  least one implementation path and one docs path per service label.
- Check all configured labels exist with `gh label list` before adding a new
  label reference.

### 7. Wrong vs Correct

#### Wrong

```json
{
  "paths": ["services/knowledge/**"],
  "labels": ["service:knowledge"]
}
```

#### Correct

```json
{
  "paths": ["services/knowledge/**", "docs/services/knowledge/**"],
  "labels": ["service:knowledge"]
}
```

---

## Auto Label Blocked PR Contract

### 1. Scope / Trigger

Update this contract when changing PR issue-link requirements, task issue
blocked semantics, or `.github/workflows/auto-label.yml` blocked-label logic.

### 2. Signatures

- Workflow file: `.github/workflows/auto-label.yml`
- PR events: `pull_request_target` opened, edited, synchronize, reopened,
  ready_for_review, labeled, unlabeled
- Issue events: `issues` edited, labeled, unlabeled, closed, reopened
- Primary PR link source: GitHub `closingIssuesReferences`
- Fallback PR link syntax: GitHub closing keywords in the `关联 Issue` section,
  for example `Closes #118`, `Fixes #119`, or `Resolves #120`
- Synced label: `blocked`

### 3. Contracts

- The workflow only treats GitHub closing issue references as linked issues.
- A PR receives `blocked` only when it has at least one linked issue and every
  linked issue is blocked.
- A managed task issue with body fields is blocked only when it is open and has
  task body field `状态：Blocked` or `Risk：Blocked`.
- A non-task linked issue without those body fields may use issue label
  `blocked` as the blocked signal.
- Closed issues, pull request pseudo-issues, unreadable issues, and issues
  without blocked state count as not blocked.
- On issue changes, the workflow finds open pull requests that reference that
  issue through timeline cross-references and PR search, then recomputes the PR
  `blocked` label.

### 4. Validation & Error Matrix

| Condition | Required handling |
|-----------|-------------------|
| PR has no linked issues | Remove `blocked` from the PR if present. |
| PR has mixed blocked and not-blocked linked issues | Remove `blocked` from the PR. |
| All linked issues are blocked | Add `blocked` to the PR when the label exists. |
| Linked issue changes from blocked to not blocked | Recompute open linked PRs and remove `blocked` where needed. |
| A linked issue cannot be read | Treat it as not blocked and log a warning. |
| `blocked` label does not exist remotely | Skip adding it and log a warning rather than failing unrelated PR labeling. |

### 5. Good/Base/Bad Cases

- Good: PR body contains `Closes #118` and `Fixes #119`; both issues are open
  with `Risk：Blocked`; PR gets `blocked`.
- Base: PR body contains `Closes #118`; issue #118 is open with
  `状态：In Progress`; PR does not get `blocked`.
- Bad: PR body says `关联 Issue: #118` without a closing keyword and expects
  blocked sync.

### 6. Tests Required

- Parse `.github/workflows/auto-label.yml` as YAML.
- Run `actionlint`.
- Extract the embedded `github-script` body and run `node --check` inside an
  async wrapper.
- Before relying on a new synced label name, verify it exists with
  `gh label list`.

### 7. Wrong vs Correct

#### Wrong

```markdown
## 关联 Issue

- #118
```

#### Correct

```markdown
## 关联 Issue

- Closes #118
```

---

## PR Guard Body Contract

### 1. Scope / Trigger

Update this contract when changing `.github/workflows/pr-guard.yml`,
`.github/pull_request_template.md`, `docs/collaboration/repository-settings.md`,
or any agent-facing PR creation workflow.

### 2. Signatures

- Workflow file: `.github/workflows/pr-guard.yml`
- PR template: `.github/pull_request_template.md`
- Repository rules: `docs/collaboration/repository-settings.md`
- PR command shape:

```bash
gh pr create \
  --repo Sakayori-Iroha-168/Software_Teamwork \
  --base develop \
  --head <owner>:<branch> \
  --title "<conventional-commit-title>" \
  --body-file <filled-chinese-template-file>
```

### 3. Contracts

- PR title must follow Conventional Commit style and must not contain Chinese
  characters.
- PR body must contain Chinese content.
- PR body must preserve the template intent with these filled sections:
  `修改内容`, `关联 Issue`, `验证`, `已知风险`, and `检查项`.
- PR body must not leave template placeholder text in `修改内容`, `关联 Issue`,
  `验证`, or `已知风险`.
- `关联 Issue` must contain a GitHub closing keyword such as `Closes #118`;
  if there is no issue, it must explain the reason instead of writing only
  `无`.
- Agents must read both `CONTRIBUTING.md` and
  `docs/collaboration/repository-settings.md` before opening or editing a PR;
  `CONTRIBUTING.md` covers branch/base/head/commit policy, while
  `repository-settings.md` contains the PR Guard title/body language contract.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| PR title contains Chinese characters | Rewrite the title to an English Conventional Commit title. |
| PR body is handwritten in English only | Replace it with the filled Chinese PR template before reporting completion. |
| PR body omits `Closes #<issue>` for an issue-backed task | Add the closing keyword in `关联 Issue`. |
| PR body keeps template placeholders | Replace placeholders with concrete Chinese content. |
| Agent read only `CONTRIBUTING.md` before creating PR | Stop and read `docs/collaboration/repository-settings.md` plus `.github/pull_request_template.md`; then re-check the PR body. |

### 5. Good/Base/Bad Cases

- Good: title is `test(frontend): add critical flow coverage`; body uses the
  Chinese template sections, lists concrete verification commands, includes
  `Closes #117`, and records known risks in Chinese.
- Base: title is English Conventional Commit style; body is mostly Chinese and
  includes all required template sections and issue linkage.
- Bad: body is manually written in English because the agent focused only on
  base/head/commitlint/`Closes #117` and skipped the PR Guard language rules.

### 6. Tests Required

Before reporting a PR as ready:

```bash
gh pr view <PR_NUMBER> --repo Sakayori-Iroha-168/Software_Teamwork \
  --json title,body,baseRefName,headRefName,headRepositoryOwner
gh pr checks <PR_NUMBER> --repo Sakayori-Iroha-168/Software_Teamwork
```

Review the returned JSON manually for:

- `baseRefName == "develop"`.
- `headRepositoryOwner.login` is the developer fork owner.
- Title has no Chinese characters.
- Body contains Chinese text and the required template sections.
- Body contains the correct closing keyword or a concrete no-issue reason.

### 7. Wrong vs Correct

#### Wrong

```markdown
## Summary

- Add tests.

## Verification

- bun run --cwd apps/web check

Closes #117
```

This is wrong because the body is English-only and does not use the required
Chinese PR template sections.

#### Correct

```markdown
## 修改内容

- 新增前端关键流程测试。

## 关联 Issue

- Closes #117

## 验证

- `bun run --cwd apps/web check`：通过。

## 已知风险

- 无。
```

---

## Target Product Workflows

Product workflow files:

| Workflow | Suggested File | Trigger |
|----------|----------------|---------|
| Frontend CI | `.github/workflows/frontend.yml` | `apps/web/**` |
| Go Services CI | `.github/workflows/go-services.yml` | `services/**` |
| Docker / Deploy Checks | `.github/workflows/docker-deploy-checks.yml` | infra Compose, Docker policy docs/scripts, `deploy/**` |

Use path filters so unrelated documentation or service changes do not run every
job. A workflow may still run a cheap detection job to decide which service jobs
are needed.

## Scenario: Path-Derived Matrix Inputs

### 1. Scope / Trigger

- Trigger: a GitHub Actions workflow derives a job matrix from changed file
  paths, pull request metadata, issue text, or other contributor-controlled
  input.
- Applies to `actions/github-script` detection jobs and downstream shell steps
  that consume matrix values.

### 2. Signatures

- Detection output: JSON arrays written with `core.setOutput`, for example
  `dockerfiles` or `compose-files`.
- Matrix consumption: `${{ fromJSON(needs.detect.outputs.<name>) }}`.
- Shell consumption: matrix values passed through step `env`, then read as
  shell variables.

### 3. Contracts

- Detection jobs must whitelist path-derived matrix entries against a known set
  or a repo-owned manifest before writing outputs.
- PR changed-file detection must consider both `filename` and
  `previous_filename` from `pulls.listFiles` so renamed files exercise checks
  for the old and new affected paths.
- Pattern checks alone are insufficient for contributor-controlled file names.
- Shell steps must not interpolate `${{ matrix.* }}` directly inside `run`
  scripts. Pass the value through `env` and quote the shell variable.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Changed path matches a broad glob but is not in the known set | Exclude it from the matrix output. |
| PR file entry has `previous_filename` | Evaluate both old and new paths through the same whitelist/routing rules. |
| Workflow file changes | Expand to the repo-owned known set, not arbitrary matching paths. |
| Matrix value is consumed by a shell step | Use `env:` and quote the shell variable in `run`. |
| A path contains quotes, command separators, spaces, or shell metacharacters | It must not reach shell execution unless it is an explicit known path. |

### 5. Good/Base/Bad Cases

- Good: `deploy/docker-compose.yml` is in the known Compose set and is validated
  through a quoted `COMPOSE_FILE` env variable.
- Good: renaming a file from `services/auth/**` to `services/qa/**` selects both
  affected services, because old and new paths are evaluated.
- Base: a root `.env.example` or Docker policy doc change maps to the known root
  Compose file.
- Bad: `deploy/docker-compose.yml";echo pwned #` matches a broad workflow trigger
  and is interpolated directly into a `run` script.

### 6. Tests Required

- Parse changed-file detection scripts with `node --check`.
- Add a local detection regression for valid known paths, workflow-file changes,
  and malicious path strings containing shell metacharacters.
- Run `actionlint` and `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```yaml
run: |
  compose_file="${{ matrix.compose-file }}"
  docker compose -f "$compose_file" config --quiet
```

#### Correct

```yaml
env:
  COMPOSE_FILE: ${{ matrix.compose-file }}
run: |
  compose_file="$COMPOSE_FILE"
  docker compose -f "$compose_file" --env-file deploy/.env.example config --quiet
```

## Scenario: Gateway Active API Contract Workflow

### 1. Scope / Trigger

- Trigger: changing the public gateway OpenAPI, gateway active owner map,
  frontend OpenAPI generation command, or the gateway contract verifier.
- Applies to `docs/services/gateway/api/public.openapi.yaml`,
  `docs/services/gateway/docs/active-api-owner-map.md`, `apps/web/package.json`,
  `package.json`, `scripts/verify_gateway_active_api.py`, `scripts/tests/**`,
  and `.github/workflows/gateway-contract.yml`.

### 2. Signatures

Local commands:

```bash
python scripts/verify_gateway_active_api.py
bun run check:gateway-contract
python -m unittest scripts.tests.test_verify_gateway_active_api
```

Workflow file:

```text
.github/workflows/gateway-contract.yml
```

### 3. Contracts

The verifier is the CI gate for these executable contracts:

- Active `/api/v1/**` operations must include `operationId`, non-empty `tags`,
  `x-owner-service`, effective `security`, at least one `2XX` response, and at
  least one `4XX` response.
- `/healthz` and `/readyz` are operational exceptions owned by `gateway` and may
  use `security: []`.
- Stable active public paths must not use action-style segments such as
  `login`, `logout`, `register`, `download`, `search`, `generate`, `export`,
  `retry`, or `revoke`.
- `x-missing-contracts.placeholderOperations` must not overlap active OpenAPI
  paths.
- `apps/web` API type generation must use
  `../../docs/services/gateway/api/public.openapi.yaml`.
- `docs/services/gateway/docs/active-api-owner-map.md` must match the active
  operations, owner summary, and missing contract placeholders derived from
  OpenAPI.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| OpenAPI metadata is missing on an active `/api/v1/**` operation | Verifier exits non-zero and names the method/path and missing field. |
| Owner map table or summary drifts from OpenAPI | Verifier exits non-zero and reports owner-map drift. |
| Missing-contract placeholder overlaps an active operation | Verifier exits non-zero and names the overlapping placeholder. |
| Frontend generation source changes away from gateway OpenAPI | Verifier exits non-zero and prints the expected source path. |
| PyYAML is unavailable in CI | Workflow installs `pyyaml` before running verifier commands. |

### 5. Good/Base/Bad Cases

- Good: update OpenAPI and owner map together, then run
  `bun run check:gateway-contract`.
- Base: update only verifier tests or workflow wiring; CI still runs the
  verifier unit tests and real-contract check.
- Bad: add `GET /api/v1/search` or an active operation without a `4XX` response
  and rely on manual review to catch it.

### 6. Tests Required

- Unit tests must cover missing required metadata, missing `4XX`, action-style
  path segments, missing-contract overlap, frontend generation source drift,
  and owner-map drift.
- Local verification before PR must run:

```bash
python -m unittest scripts.tests.test_verify_gateway_active_api
python scripts/verify_gateway_active_api.py
```

### 7. Wrong vs Correct

#### Wrong

```text
Change docs/services/gateway/api/public.openapi.yaml
Skip docs/services/gateway/docs/active-api-owner-map.md
Open PR without running the verifier
```

#### Correct

```text
Change docs/services/gateway/api/public.openapi.yaml
Update docs/services/gateway/docs/active-api-owner-map.md
Run bun run check:gateway-contract
Let .github/workflows/gateway-contract.yml enforce the same gate in PR
```

---

## Frontend CI Target

Frontend CI is a landed workflow. It runs only when frontend files, root
frontend dependency files, or the frontend workflow file change.

Target steps:

```bash
bun install --frozen-lockfile
bun run --cwd apps/web check
bun run --cwd apps/web build
bun run --cwd apps/web test:unit
bun run --cwd apps/web test:e2e
```

Rules:

- Keep CI commands behind package scripts.
- Do not encode a specific build tool in workflow logic unless the frontend tool is selected and documented.
- Cache package-manager dependencies using lockfile-based keys.
- Fail if the lockfile and package manifest are inconsistent.
- Vitest, React Testing Library, and Playwright scripts/dependencies already
  exist in `apps/web/package.json`. If replacing or adding frontend test tools,
  update `apps/web/package.json`, `bun.lock`, `docs/architecture/technology-decisions.md`,
  `docs/testing/strategy.md`, and this spec together.

---

## Go Services CI

Each Go service owns an independent `go.mod`. CI must test and build changed
services independently.

Service paths:

```text
services/gateway/
services/auth/
services/file/
services/qa/
services/knowledge/
services/document/
services/ai-gateway/
```

Required service-local checks:

```bash
go test ./...
go build ./cmd/server
```

Rules:

- Run checks from the changed service directory.
- Do not rely on a root `go.mod`.
- Cache Go modules per service or with keys that include service `go.sum`.
- If shared code is introduced later, update path filters so dependent services run.
- Use a matrix job when multiple services changed.

Example matrix dimensions:

```yaml
service:
  - gateway
  - auth
  - file
  - qa
  - knowledge
  - document
  - ai-gateway
```

---

## Scenario: GitHub Actions Minimum Permissions

### 1. Scope / Trigger

- Trigger: adding or modifying a GitHub Actions workflow, or fixing
  `actions/missing-workflow-permissions` / equivalent security alerts.
- Applies to every workflow under `.github/workflows/*.yml`.

### 2. Signatures

- Read-only repository workflows declare:

```yaml
permissions:
  contents: read
```

- Workflows that need additional scopes must list each scope explicitly at the
  workflow or job level and document the reason in the PR.

### 3. Contracts

- Do not rely on GitHub's default token permissions.
- Prefer the narrowest scope that lets the workflow run.
- Workflows that only check out repository content, run tests, parse files, or
  compare generated artifacts normally need only `contents: read`.
- Pull request labeling, issue comments, package pushes, deployments, OIDC, or
  status writes require explicit additional permissions and must stay tied to
  the job that needs them when practical.
- Do not add write scopes to silence a failing workflow without proving the
  command that needs the scope.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Workflow omits top-level and job-level `permissions` | Add the minimum explicit permissions. |
| Workflow uses `actions/checkout` only | Use `contents: read`. |
| Workflow writes labels or comments | Add only the required write scope, for example `issues: write` or `pull-requests: write`. |
| Workflow publishes packages or images | Add the package/deployment scope only on the publishing job. |
| Code scanning flags missing permissions | Fix the workflow YAML; do not suppress CodeQL or remove the workflow. |

### 5. Good/Base/Bad Cases

- Good: an API type drift workflow checks out code and declares
  `permissions: { contents: read }`.
- Base: a labeling workflow has `contents: read`, `issues: write`, and
  `pull-requests: write` because the script applies labels.
- Bad: a read-only test workflow uses broad write permissions or omits
  permissions entirely.

### 6. Tests Required

- Parse changed workflows as YAML.
- Run `actionlint` for changed workflows when available locally.
- For embedded JavaScript in `github-script`, run `node --check` inside an async
  wrapper when the script body changed.
- Record the exact workflow permission rationale in the PR body for
  security-alert work.

### 7. Wrong vs Correct

#### Wrong

```yaml
name: API Types Check
on: pull_request
jobs:
  check:
    runs-on: ubuntu-latest
```

#### Correct

```yaml
name: API Types Check
on: pull_request

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
```

---

## Docker Infra Compose

Repository Docker usage is infrastructure-only. The root Compose default path
may pull and start only:

```text
postgres
redis
qdrant
minio
minio-init
```

Rules:

- Do not add business services, migration jobs, seed jobs, frontend, Parser, or
  service runtime containers to the root Compose baseline.
- The only allowed root Compose profile service is `elasticsearch` under profile
  `knowledge-runtime`, used for local Knowledge runtime doc-engine
  infrastructure when `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true`.
- Do not add `build:` entries to `deploy/docker-compose.yml`. The optional
  `elasticsearch` profile must use a pinned image variable, not a local build.
- Compose infrastructure images must keep pinned defaults and may expose
  full-image override variables for local or enterprise registries. Do not use
  `latest` as a default or documented normal path.
- Source selection is official-by-default. The previous mainland-first default
  mirror contract is retired. Keep official Docker Hub, PyPI, `proxy.golang.org`, and
  `sum.golang.org` as the committed defaults.
- Mainland China users must still have a first-class explicit mirror mode.
  Prefer `registry rewrite > daemon mirror > proxy`: registry rewrite is
  selected by `dev-up.sh --china` or local untracked `deploy/.env` overrides,
  daemon mirrors are local machine state, and proxies are last-resort
  environment state. Keep these paths documented and diagnosable.
- Docker/Compose PR checks must run `python3 scripts/check_docker_policy.py`
  before Compose config validation. Keep this checker aligned with Docker policy
  changes so CI blocks obvious regressions without depending on a working Docker
  daemon mirror.
- Docker environment diagnostics belong in `scripts/check_docker_environment.py`.
  CI may run it with `--skip-network`; local investigations may run manifest
  probes with `--profile all --clean-env`.
- Docker policy docs/spec changes, including `deploy/README.md`, should trigger
  the lightweight policy checker even when Compose itself did not change.
- Local startup scripts, local seed SQL, and local seed contract files must
  trigger Docker/deploy checks. The policy job must run shell syntax checks for
  `scripts/local/*.sh` and `python3 scripts/verify_local_seed_contract.py`.
- Business-service Docker artifacts must not be introduced into the current
  repository baseline. The Docker/deploy detect job and policy checker must
  reject business-service Dockerfiles, service-level Compose files, and
  non-root deploy Compose files.

---

## Local Integration Runtime

Local integration uses `deploy/docker-compose.yml` only for shared
infrastructure. Business services run on the host. Optional local Elasticsearch
is Compose-managed infrastructure, not a business service container, and must be
disabled by default.

Required local sequence:

1. Copy local defaults with `cp deploy/.env.example deploy/.env`.
2. Run `./scripts/local/dev-up.sh` to pull/start infra, wait for long-running
   service health, run the one-shot `minio-init`, apply any configured
   legacy/test-only Qdrant collection initialization, host migrations, and
   local seed. Current Knowledge indexing is prepared by the host-run Knowledge
   runtime/doc engine, not by restoring Go-side Qdrant bootstrap as a required
   default. If local `deploy/.env` sets
   `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true`, this step also enables the
   Compose `knowledge-runtime` profile and starts the optional `elasticsearch`
   infrastructure service.
3. Start or keep available the host-run Knowledge runtime API/worker when
   running Knowledge ingestion/retrieval scenarios.
4. Run `./scripts/local/run-backend.sh` to start Auth, File, Knowledge,
   AI Gateway, QA, Document, and Gateway as host processes.
5. Run `cd apps/web && bun install && bun run dev` for the frontend.

Runtime rules:

- Store real runtime secrets outside the repository.
- Use `deploy/.env.example` as the single default local configuration source.
  Startup scripts may load `deploy/.env`, but must not duplicate service env
  defaults or generate env files for the user.
- Treat missing active DaoCloud/TUNA/goproxy.cn entries in `deploy/.env.example`
  as intentional under the current source policy, not as a mainland registry
  regression. `scripts/check_docker_policy.py` should reject active committed
  `*_IMAGE` mirror defaults while allowing commented examples and local
  untracked overrides.
- `run-backend.sh` must not prepare or start the retired standalone Parser.
  Knowledge parsing runs through the RAGFlow runtime API/worker path.
- Keep `UV_DEFAULT_INDEX` in `deploy/.env.example` as the default host-run uv
  package index, using official PyPI by default. Mainland China mirror usage
  must be explicit, preferably through `dev-up.sh --china` which prepares
  Knowledge runtime dependencies/artifacts, or local untracked env overrides.
  `ragflow_deps/download_deps.py --china` remains the manual fallback when that
  preparation was intentionally skipped. It affects Python dependency downloads
  only; Docker registry rewrite remains the Compose image path.
- Treat `services/knowledge-runtime/**` and its host-run API/worker scripts as
  the local runtime contract for Knowledge parsing and retrieval changes.
- `run-knowledge-parse-stack.sh` must not run direct `docker build` or
  `docker run` for Elasticsearch. It verifies the configured
  `KNOWLEDGE_RUNTIME_ES_URL` and starts host-run runtime processes; local
  Elasticsearch lifecycle belongs to the root Compose profile in `dev-up.sh`.
- `HF_ENDPOINT=https://hf-mirror.com` must not be active in committed defaults
  or forced by runtime scripts in official-default mode. Mainland China runtime
  model download mirrors are explicit through
  `run-knowledge-parse-stack.sh --china` or local untracked env overrides.
- Keep `GOPROXY` and `GOSUMDB` in `deploy/.env.example` as the default host-run
  Go module proxy/checksum settings, using official upstream values by default.
  Mainland China mirror usage must be explicit through `dev-up.sh --china`,
  `run-backend.sh --china`, or local untracked env overrides. They affect
  `dev-up.sh` goose migrations and `run-backend.sh` Go service startup, not
  Docker image pulls or Knowledge runtime uv downloads.
- `dev-up.sh` must check effective Go module settings before host-run goose
  migrations, and `run-backend.sh` must preflight each host-run Go service with
  `go mod download` before forking service processes. If `--china` is passed,
  local scripts may use mainland China mirrors for the current process without
  rewriting `deploy/.env`. If an old `deploy/.env` still contains mirror values
  while `--china` was not passed, scripts should warn but respect that local
  config. If module download fails, it should fail visibly in the terminal with
  the current effective values and remediation guidance.
- Host-run process management is part of the local startup contract:
  `run-backend.sh` should start service commands in managed process groups and
  `stop-backend.sh` should stop those process groups, not just wrapper PIDs.
- Local entrypoint scripts under `scripts/local/` must print command-line status
  for start, success, warning, failure, and diagnostic hints. Use
  human-scannable colored status labels when stdout/stderr supports color, with
  `NO_COLOR=1` disabling color. Failure output should include the current stage
  and next diagnostic location so contributors are not misled by missing or
  log-only errors.
- After forking services, `run-backend.sh` should observe a short configurable
  startup window and report early process exits with the relevant
  `.local/logs/<service>.log` tail instead of unconditionally printing
  `backend started`.
- Seeded local AI Gateway profiles should use `http://localhost:11434/v1` for
  the host-run default path; container-only hostnames such as
  `host.docker.internal` must fail the local seed/startup contract.
- Use named volumes for PostgreSQL, Qdrant, and MinIO persistence.
- Keep frontend and browser traffic routed through Gateway.
- Health checks for infra stay in Compose; service health checks are host-run
  `/healthz` and `/readyz` calls.

---

## Secrets

Never commit:

- database passwords,
- session, service-token, or signing secrets,
- MinIO access keys or secret keys,
- API keys,
- SSH private keys,
- cloud credentials.

---

## Checks Before Merge

Current required checks are defined by repository branch protection and
`docs/testing/strategy.md`; do not infer additional required checks from target
workflow sections above. For PRs:

- PR Guard passes.
- Commitlint passes.
- Current product CI passes for touched areas when the workflow exists.
- Frontend changes are covered by Frontend CI; local `bun run --cwd apps/web check`,
  `bun run --cwd apps/web build`, and targeted tests remain useful PR-before
  evidence.
- Docker/Compose config checks are covered for the infra-only root Compose,
  Docker policy docs/scripts, and image-source overlays; full DB integration
  jobs and cross-service smoke remain future gates until stable workflows land.
- Documentation changes update README/specs when architecture, commands,
  contracts, or implementation status change.

---

## Common Mistakes

- Running all service builds for every small frontend change.
- Assuming a root Go module exists.
- Committing production `.env` files.
- Exposing internal services directly to the public network.
