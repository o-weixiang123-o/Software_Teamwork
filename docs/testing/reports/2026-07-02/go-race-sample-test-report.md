# Go 后端 Race Detector 抽样检查报告

## 0. 基本信息

| 项目 | 记录 |
| --- | --- |
| 报告日期 | `2026-07-02` |
| 测试任务 / Issue | `T-011` / [#455](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/455) |
| 测试负责人 | `@AKTNL` |
| 协助人员 | 无 |
| 测试范围 | Gateway、QA、Knowledge、Document 的并发敏感 Go focused packages |
| 被测分支 | `Test/test/go-race-sample` |
| 被测源码快照 | `upstream/develop @ f05776cef4b5b6caa54bec5d177f66937f25962d` 的 `services/` tree `885fe90bb18c15778c39483794e0a20697b520eb` |
| Base branch | `upstream/develop @ f05776cef4b5b6caa54bec5d177f66937f25962d` |
| 测试环境 | Windows/amd64 PowerShell；Docker Desktop Linux engine；`golang:1.25.6-bookworm` |
| 结论 | 测试通过 |

说明：本 PR 只新增/更新 `docs/testing/**` 测试证据，不修改 `services/**`。PR review 后已将分支 rebase 到最新 `upstream/develop @ f05776cef4b5b6caa54bec5d177f66937f25962d`，并在该 base 对应的 `services/` tree 上重新执行非缓存 focused 普通测试和 race 抽样。当前 PR HEAD 与被测源码快照的关系可按“PR HEAD 与被测源码快照对照”小节复核。

## 1. 测试目标

- 对耗时可控的 Go 后端并发敏感包执行 `go test -race` 抽样。
- 覆盖 Gateway middleware/http、QA service/platform、Knowledge worker/queue、Document worker/job service 路径。
- 区分普通 focused `go test` 基线、race detector 抽样、未运行项和环境阻塞项。
- 不把 `-race` 升级为默认 CI required check。

本轮不验证完整跨服务 E2E、真实数据库/Redis/legacy vector index/MinIO/provider、全量 `go test ./...` 或服务构建命令。

## 2. 测试依据

| 类型 | 链接或文件 | 使用方式 |
| --- | --- | --- |
| 测试任务 | [#455](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/455) | 任务范围、建议命令、验收标准 |
| 测试资料入口 | `docs/testing/README.md` | 报告归档与证据要求 |
| 测试策略 | `docs/testing/strategy.md` | 本地自动化、CI 与 env-gated 检查分层 |
| 测试矩阵 | `docs/testing/test-matrix.md` | T-* 任务证据追踪入口 |
| Go Services CI | `.github/workflows/go-services.yml` | 当前 CI 仅运行普通 `go test` / build，不默认启用 `-race` |
| 后端质量规范 | `.trellis/spec/backend/quality-guidelines.md` | Go 服务本地检查、CI 基线和 forbidden patterns |

## 3. 测试范围与不测范围

### 测试范围

| 服务 | Focused packages | 并发敏感路径 |
| --- | --- | --- |
| Gateway | `./internal/middleware ./internal/http` | HTTP middleware、request handling、proxy-facing handler tests |
| QA | `./internal/service/... ./internal/platform/...` | SSE/agent service、tool/model/knowledge/MCP platform clients |
| Knowledge | `./internal/service ./internal/worker ./internal/platform/queue` | ingestion service、worker、queue/asynq adapter |
| Document | `./internal/service ./internal/worker` | report service、worker/job orchestration |

### 不测范围

- 不运行全量 `go test ./...` 与 `go build ./cmd/server`；本任务只做 focused race sampling。
- 不启动 PostgreSQL、Redis、legacy vector index、MinIO、Parser、AI Gateway 或真实 provider。
- 不执行 env-gated integration smoke。
- 不修改 `.github/workflows/go-services.yml`，不新增默认 required check。

### 环境与前置条件

- Windows native Go: `go version go1.25.6 windows/amd64`。
- Docker Go: `go version go1.25.6 linux/amd64`。
- Docker image: `golang:1.25.6-bookworm`，digest `sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e`。
- Docker 源码挂载为只读：`D:\college\software\Software_Teamwork:/workspace:ro`。
- Docker Go module/build cache 使用命名卷：`stw-go-mod-cache-1.25.6`、`stw-go-build-cache-1.25.6`。

### Docker 复跑命令模板

以下命令为 PowerShell 形式。实际执行使用 `golang:1.25.6-bookworm`；执行前 `docker pull golang:1.25.6-bookworm` 返回 digest `sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e`。如需强制 digest pin，可将 `$Image` 替换为 `$ImagePinned`。

```powershell
$Repo = "D:\college\software\Software_Teamwork"
$Image = "golang:1.25.6-bookworm"
$ImagePinned = "golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e"
$DockerArgs = @(
  "--rm",
  "-v", "${Repo}:/workspace:ro",
  "-v", "stw-go-mod-cache-1.25.6:/go/pkg/mod",
  "-v", "stw-go-build-cache-1.25.6:/root/.cache/go-build"
)
```

| 命令 ID | 可复跑命令 |
| --- | --- |
| DOCKER-GATEWAY-BASELINE | `docker run @DockerArgs -w /workspace/services/gateway $Image go test -count=1 ./internal/middleware ./internal/http` |
| DOCKER-GATEWAY-RACE | `docker run @DockerArgs -w /workspace/services/gateway $Image go test -race -count=1 ./internal/middleware ./internal/http` |
| DOCKER-QA-BASELINE | `docker run @DockerArgs -w /workspace/services/qa $Image go test -count=1 ./internal/service/... ./internal/platform/...` |
| DOCKER-QA-RACE | `docker run @DockerArgs -w /workspace/services/qa $Image go test -race -count=1 ./internal/service/... ./internal/platform/...` |
| DOCKER-KNOWLEDGE-BASELINE | `docker run @DockerArgs -w /workspace/services/knowledge $Image go test -count=1 ./internal/service ./internal/worker ./internal/platform/queue` |
| DOCKER-KNOWLEDGE-RACE | `docker run @DockerArgs -w /workspace/services/knowledge $Image go test -race -count=1 ./internal/service ./internal/worker ./internal/platform/queue` |
| DOCKER-DOCUMENT-BASELINE | `docker run @DockerArgs -w /workspace/services/document $Image go test -count=1 ./internal/service ./internal/worker` |
| DOCKER-DOCUMENT-RACE | `docker run @DockerArgs -w /workspace/services/document $Image go test -race -count=1 ./internal/service ./internal/worker` |

### PR HEAD 与被测源码快照对照

由于本 PR 只调整测试证据文档，PR HEAD 会随 evidence-only amend 继续变化；race detector 实测源码固定为 base `f05776cef4b5b6caa54bec5d177f66937f25962d` 的 `services/` tree `885fe90bb18c15778c39483794e0a20697b520eb`。复核当前 PR HEAD 是否仍对应同一份 Go 服务源码，可在检出 PR head 后执行：

```powershell
git rev-parse HEAD:services
git diff --quiet f05776cef4b5b6caa54bec5d177f66937f25962d..HEAD -- services
git diff --name-only f05776cef4b5b6caa54bec5d177f66937f25962d..HEAD
```

本次证据修复前执行结果：

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `git rev-parse HEAD:services` | `885fe90bb18c15778c39483794e0a20697b520eb` | 当前 PR HEAD 的 `services/` tree 与被测源码快照一致 |
| `git diff --quiet f05776cef4b5b6caa54bec5d177f66937f25962d..HEAD -- services` | exit 0 | `services/**` 无差异，说明当前 PR HEAD 没有改动 Go 服务源码 |
| `git diff --name-only f05776cef4b5b6caa54bec5d177f66937f25962d..HEAD` | `docs/testing/reports/2026-07-02/go-race-sample-test-report.md`；`docs/testing/test-matrix.md` | 本 PR 差异仅为测试证据文档 |

## 4. 测试用例矩阵

| ID | 分类 | 用例 / 场景 | 预期结果 | 实际结果 | 结论 |
| --- | --- | --- | --- | --- | --- |
| TEST-001 | 环境预检 | Windows native `go test -race` | 若本机具备 cgo/GCC，则可执行；否则记录环境阻塞 | `CGO_ENABLED=0` 提示 `-race requires cgo`；`CGO_ENABLED=1` 后缺少 `gcc` | blocked |
| TEST-002 | 普通测试 | Gateway focused `go test` | 通过 | 通过 | pass |
| TEST-003 | Race 抽样 | Gateway focused `go test -race` | 通过且无 race 报告 | 通过，无 race 报告 | pass |
| TEST-004 | 普通测试 | QA focused `go test` | 通过 | 通过 | pass |
| TEST-005 | Race 抽样 | QA focused `go test -race` | 通过且无 race 报告 | 通过，无 race 报告 | pass |
| TEST-006 | 普通测试 | Knowledge focused `go test` | 通过 | 通过 | pass |
| TEST-007 | Race 抽样 | Knowledge focused `go test -race` | 通过且无 race 报告 | 通过，无 race 报告 | pass |
| TEST-008 | 普通测试 | Document focused `go test` | 通过 | 通过 | pass |
| TEST-009 | Race 抽样 | Document focused `go test -race` | 通过且无 race 报告 | 通过，无 race 报告 | pass |

## 5. 执行命令与结果

| 时间 | ID | 命令或操作 | 结果 | 证据 / 备注 |
| --- | --- | --- | --- | --- |
| `2026-07-02 12:40 +0800` | TEST-001 | `cd services/gateway && go test -race ./internal/middleware ./internal/http` | blocked | Windows native：exit 2，0.1s；`go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` |
| `2026-07-02 12:40 +0800` | TEST-001 | `cd services/gateway && CGO_ENABLED=1 go test -race ./internal/middleware ./internal/http` | blocked | Windows native：exit 1，11.9s；`cgo: C compiler "gcc" not found` |
| `2026-07-02 13:20 +0800` | TEST-002 | DOCKER-GATEWAY-BASELINE | pass | exit 0，1.4s；`internal/middleware`、`internal/http` 通过 |
| `2026-07-02 13:20 +0800` | TEST-003 | DOCKER-GATEWAY-RACE | pass | exit 0，2.5s；无 race 报告 |
| `2026-07-02 13:20 +0800` | TEST-004 | DOCKER-QA-BASELINE | pass | exit 0，1.9s；无失败包 |
| `2026-07-02 13:20 +0800` | TEST-005 | DOCKER-QA-RACE | pass | exit 0，2.7s；无 race 报告 |
| `2026-07-02 13:20 +0800` | TEST-006 | DOCKER-KNOWLEDGE-BASELINE | pass | exit 0，0.9s；无失败包 |
| `2026-07-02 13:20 +0800` | TEST-007 | DOCKER-KNOWLEDGE-RACE | pass | exit 0，2.0s；无 race 报告 |
| `2026-07-02 13:20 +0800` | TEST-008 | DOCKER-DOCUMENT-BASELINE | pass | exit 0，0.9s；无失败包 |
| `2026-07-02 13:20 +0800` | TEST-009 | DOCKER-DOCUMENT-RACE | pass | exit 0，2.0s；无 race 报告 |

未运行项：

| 测试项 | 未运行原因 | 缺失环境 | 残余风险 | 后续归属 |
| --- | --- | --- | --- | --- |
| 全量 `go test ./...` | #455 范围是 focused race sampling，本轮未改 Go 业务代码 | 无 | focused 包通过不代表所有 Go 包通过 | 常规 Go Services CI / 后续服务 PR |
| `go build ./cmd/server`、QA `go build ./cmd/agent` | 本轮不改服务代码或构建配置 | 无 | 不覆盖二进制构建回归 | 常规 Go Services CI / 后续服务 PR |
| env-gated integration smoke | 本轮不启动真实数据库、Redis、legacy vector index、MinIO、Parser 或 provider | 真实本地联调依赖未启动 | 不证明真实跨服务链路无竞态或集成问题 | #125 / #304 / 相关 owner issue |
| 默认 CI `-race` required check | #455 明确不要求升级默认 CI | 无 | race detector 仍是本地/PR 前建议层级 | 如需升级需另行资源评估 issue |

## 6. 缺陷与处理记录

| 问题 | 等级 | 处理结论 | 关联 issue / PR | 说明 |
| --- | --- | --- | --- | --- |
| Windows native race detector 缺少 cgo/GCC | 环境问题 | 记录环境阻塞，改用 Docker Linux Go 1.25.6 执行实际抽样 | #455 | 不是代码 race；Docker 环境完成全部 focused race 抽样 |
| Race detector 抽样未发现竞态 | 无缺陷 | 不新建 owner issue | 无 | 四个服务 focused race commands 均 exit 0，未输出 data race |

## 7. 证据清单

| 证据类型 | 位置 / 链接 | 说明 |
| --- | --- | --- |
| 测试报告 | `docs/testing/reports/2026-07-02/go-race-sample-test-report.md` | 本报告 |
| 测试矩阵 | `docs/testing/test-matrix.md` | #455 / T-011 证据路径与风险口径 |
| Issue | [#455](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/455) | 任务来源和验收标准 |

## 8. 风险与剩余缺口

- 本轮是 focused package sampling，不替代所有服务的全量普通测试、构建检查或跨服务 E2E。
- Docker Linux race 抽样通过不代表 Windows native race detector 可直接运行；Windows 本机仍需安装兼容的 GCC/cgo 工具链才能原生执行。
- 未启动真实基础设施或 provider，因此不覆盖 repository integration、real dependency smoke 或长时间运行压力下的竞态。
- 未修改默认 CI；`-race` 仍保持本地/PR 前建议层级。

## 9. 最终结论

测试通过：Gateway、QA、Knowledge、Document 四个 Go 服务的 focused race detector 抽样均已在 Docker Linux Go 1.25.6 环境中以 `-count=1` 非缓存方式实际执行并通过，未发现 race；普通 focused `go test -count=1` 基线也已通过。Windows native 缺少 GCC/cgo 工具链的问题已作为环境阻塞记录，不影响 Docker 环境中的抽样结论。

## 10. 复核清单

- [x] 已实际运行测试，而不是只补测试代码或测试清单。
- [x] 已记录执行命令、环境、结果和失败证据。
- [x] 已区分普通 focused 测试、race 抽样和未运行项。
- [x] 已记录 Windows native race detector 环境阻塞和残余风险。
- [x] 未把 `-race` 默认升级为 CI required check。
