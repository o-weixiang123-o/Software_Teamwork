# 本地启动脚本诊断与 Go 镜像预检验收测试报告

## 0. 基本信息

| 项目 | 记录 |
| --- | --- |
| 报告日期 | `2026-07-03` |
| 测试任务 / Issue | `T-016` / `#550` |
| 测试负责人 | `@EIR9264` |
| 协助人员 | 无 |
| 测试范围 | `deploy/docker-compose.yml`、`scripts/local/dev-up.sh`、`scripts/local/run-backend.sh`、`scripts/local/stop-backend.sh`、local startup docs/checker |
| 被测分支 | `Test/test/local-startup-diagnostics` |
| 被测 commit | `796f7f218ae8` |
| Base branch | `upstream/develop @ 796f7f218ae8` |
| 测试环境 | 本地 Debian 13 / Docker 29.6.1 / Docker Compose v5.2.0 / Go 1.25.4 / Dockerized PostgreSQL client wrapper |
| 结论 | 测试通过 |

说明：本报告记录 `Test/test/local-startup-diagnostics` / `796f7f218ae8`
上的历史执行证据，不是当前 Docker 策略来源。当前本地 Docker 策略以
`AGENTS.md`、`deploy/README.md`、`docs/runbooks/docker-image-pull-environment.md`
和 `docs/testing/strategy.md` 为准；根级 Compose 默认基础设施清单已扩展为
`postgres`、`redis`、`legacy-vector-index`、`minio`、`minio-init`、`elasticsearch`。

## 1. 测试目标

- 验证旧 `deploy/.env` 缺少 `GOPROXY` / `GOSUMDB` 时，脚本只在当前进程使用仓库默认值并提示用户补齐，不改写本地文件。
- 验证 `run-backend.sh` 在 Go module 预检失败时输出失败服务、有效 `GOPROXY` / `GOSUMDB` 和下一步排查入口，并以非零状态退出。
- 验证服务早退时，`run-backend.sh` 汇总失败服务和 `.local/logs/<service>.log` 尾部，不误报静默成功。
- 验证 `dev-up.sh`、`run-backend.sh`、`stop-backend.sh` 均有开始、成功和失败摘要。
- 验证文档/checker 能区分 Go module proxy、Docker registry rewrite、`UV_DEFAULT_INDEX`。

不验证完整业务 E2E、不验证真实模型 provider、不恢复或验证 retired standalone Parser。

## 2. 测试依据

| 类型 | 链接或文件 | 使用方式 |
| --- | --- | --- |
| Issue | `#550` | 任务来源、验收标准和交付报告要求。 |
| 依赖 Issue | `#542` | 本地启动脚本诊断能力来源；测试前确认已关闭。 |
| 测试策略 | `docs/testing/strategy.md` | 本地启动脚本 / 文档命令矩阵。 |
| 运行手册 | `README.md`、`deploy/README.md`、`docs/runbooks/local-integration.md` | 启动命令、Go/Docker/uv 边界和排障入口。 |
| Trellis 规范 | `.trellis/spec/backend/quality-guidelines.md`、`.trellis/spec/cicd.md` | Compose infra-only、host-run backend、Go proxy 和 local startup 契约。 |

## 3. 测试范围与不测范围

### 测试范围

- Compose orphan cleanup 和当时分支的 infra-only 观察值。
- `dev-up.sh` 真实本地路径：infra health、MinIO init、legacy vector index collection、migration、seed。
- `run-backend.sh` 真实本地成功路径和隔离失败路径。
- `stop-backend.sh` 真实停止清理路径。
- checker、unit test、shell syntax、Docker policy 和 Compose config。

### 不测范围

- 前端启动和浏览器 E2E。
- 真实模型 provider readiness 或模型调用。
- 业务 API 深度验收。

### 环境与前置条件

- Docker 清理前存在旧业务服务 orphan containers；已执行：
  `docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --remove-orphans`。
- 在该 commit 上清理后只剩 `postgres`、`redis`、`legacy-vector-index`、`minio` 四个长运行容器，
  均为 `healthy`；这记录的是当时分支的实际观察值，不代表当前默认 Compose 策略。
- 当前宿主机 `go` 安装在 `/usr/local/go/bin/go`，但不在默认 PATH；真实运行命令用临时 PATH 加入 `/usr/local/go/bin`。
- 当前宿主机没有 native `psql` 且无免密 sudo；真实 `dev-up.sh` 使用临时 `/tmp/issue550-tools/psql` wrapper 调用 `docker.m.daocloud.io/library/postgres:16-alpine` 内的 `psql`，并将仓库只读挂载给容器读取 seed 文件。
- 当前 `deploy/.env` 是旧文件，缺少 30 个 `.env.example` 默认键；正常成功路径用 `set -a; . deploy/.env.example; . deploy/.env; set +a` 在当前进程补齐缺失默认值，不改写 `deploy/.env`。

## 4. 测试用例矩阵

| ID | 分类 | 用例 / 场景 | 预期结果 | 实际结果 | 结论 |
| --- | --- | --- | --- | --- | --- |
| TEST-001 | Docker | 清理 Compose orphan containers | 仅保留当时分支的 infra 容器 | 旧 `gateway/auth/.../parser` 容器被移除；`postgres/redis/legacy-vector-index/minio` healthy | pass |
| TEST-002 | 静态检查 | Compose config 和服务清单 | config 可解析，服务仅为当时分支的 `minio/minio-init/postgres/legacy-vector-index/redis` | 通过 | pass |
| TEST-003 | 静态检查 | Shell syntax、seed contract、unit test、Docker policy | 全部通过 | 全部通过 | pass |
| TEST-004 | 真实启动 | `dev-up.sh` | infra、migration、seed 完成并输出成功摘要 | 完成，输出 `local dev-up: completed successfully` | pass |
| TEST-005 | 真实启动 | `run-backend.sh` 成功路径 | Go module 预检通过、7 个 host-run 服务启动、输出成功摘要 | 完成，7 个 `/healthz` 均 ok | pass |
| TEST-006 | 停止清理 | `stop-backend.sh` | 停止进程组、删除 pid 文件、输出成功摘要 | 处理 7 个 pid 文件，停止后无残留 host-run 进程 | pass |
| TEST-007 | 失败路径 | 旧 env 缺少 `GOPROXY/GOSUMDB` 且 Go module 下载失败 | 使用进程内仓库默认值、不改写 env、非零退出并打印 Go 设置 | 退出码 1，env hash 前后一致，输出有效 `GOPROXY/GOSUMDB` 和补齐提示 | pass |
| TEST-008 | 失败路径 | 服务 fork 后快速早退 | 汇总失败服务和日志尾部，不只打印 `backend started` | 退出码 1，7 个服务均被汇总并打印对应 log tail | pass |
| TEST-009 | 环境兼容 | 旧 `deploy/.env` 缺少新 token | 失败输出能指向日志和非 Go mirror 分类 | Auth 早退时输出 `AUTH_GATEWAY_ADMIN_SERVICE_TOKEN` 日志尾部和排障提示 | pass |
| TEST-010 | 文档/checker | Go proxy / Docker registry / uv 边界 | checker 能防止概念混淆回退 | `python3 scripts/verify_local_seed_contract.py` 和 unit test 通过 | pass |

## 5. 执行命令与结果

| 时间 | ID | 命令或操作 | 结果 | 证据 / 备注 |
| --- | --- | --- | --- | --- |
| `2026-07-03 13:08 +0800` | TEST-001 | `docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --remove-orphans` | pass | 移除旧业务服务 orphan containers；后续 `ps` 只剩 infra。 |
| `2026-07-03 13:09 +0800` | TEST-002 | `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet` | pass | 无输出，退出码 0。 |
| `2026-07-03 13:09 +0800` | TEST-002 | `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --services` | pass | 输出 `minio`、`minio-init`、`postgres`、`legacy-vector-index`、`redis`。 |
| `2026-07-03 13:09 +0800` | TEST-003 | `bash -n scripts/local/dev-up.sh scripts/local/run-backend.sh scripts/local/stop-backend.sh` | pass | 无输出，退出码 0。 |
| `2026-07-03 13:09 +0800` | TEST-003 | `python3 scripts/verify_local_seed_contract.py` | pass | `Local seed contract verification passed.` |
| `2026-07-03 13:09 +0800` | TEST-003 | `python3 -m unittest scripts.tests.test_local_seed_contract` | pass | `Ran 7 tests ... OK`。 |
| `2026-07-03 13:09 +0800` | TEST-003 | `python3 scripts/check_docker_policy.py` | pass | `Docker policy checks passed.` |
| `2026-07-03 13:20 +0800` | TEST-003 | `python3 -m unittest scripts.tests.test_local_dev_up_script` | pass | `Ran 3 tests ... OK`。 |
| `2026-07-03 13:10 +0800` | TEST-004 | `./scripts/local/dev-up.sh` | fail/pass | 初次直接运行失败：默认 PATH 缺 `go` / `psql`。补临时 PATH 和 Dockerized `psql` wrapper 后通过。 |
| `2026-07-03 13:18 +0800` | TEST-004 | `PATH="/tmp/issue550-tools:/usr/local/go/bin:$PATH" ./scripts/local/dev-up.sh` | pass | 当前 `796f7f218ae8` 上完成 tool check、Compose config、image pull、infra health、MinIO init、legacy vector index collection、migrations、seed；QA migration 升至 version 16；最终 `local dev-up: completed successfully`。 |
| `2026-07-03 13:12 +0800` | TEST-009 | `PATH="/usr/local/go/bin:$PATH" LOCAL_BACKEND_STARTUP_CHECK_SECONDS=2 ./scripts/local/run-backend.sh` | pass | 用旧 `deploy/.env` 运行时 Auth 早退；脚本输出 `backend startup failed for: auth` 和 auth log tail：`AUTH_GATEWAY_ADMIN_SERVICE_TOKEN is required when AUTH_DATABASE_URL is set`。 |
| `2026-07-03 13:19 +0800` | TEST-005 | `set -a; . deploy/.env.example; . deploy/.env; set +a; PATH="/usr/local/go/bin:$PATH" LOCAL_BACKEND_STARTUP_CHECK_SECONDS=18 ./scripts/local/run-backend.sh` | pass | 当前 `796f7f218ae8` 上 Go module checks 全部通过，7 个服务启动，最终 `local backend startup: completed successfully`。 |
| `2026-07-03 13:19 +0800` | TEST-005 | `curl -fsS http://127.0.0.1:<port>/healthz` for ports `8001 8082 8083 8084 8085 8086 8080` | pass | Auth/File/Knowledge/QA/Document/AI Gateway/Gateway 均返回 JSON `status":"ok"`。 |
| `2026-07-03 13:19 +0800` | TEST-006 | `PATH="/usr/local/go/bin:$PATH" ./scripts/local/stop-backend.sh` | pass | 输出 `local backend stop: completed successfully; processed 7 pid file(s)`；`.local/run/*.pid` 清空。 |
| `2026-07-03 13:19 +0800` | TEST-006 | `ps -eo ... | rg 'go run ./cmd|/tmp/go-build|...'` | pass | 仅匹配检查命令自身，无 host-run 服务残留。 |
| `2026-07-03 13:20 +0800` | TEST-007 | `/tmp/issue550-worktree @ 796f7f218ae8` + fake `go mod download` failure | pass | 退出码 1；`deploy/.env did not set GOPROXY/GOSUMDB`；有效值为 `https://goproxy.cn,direct` / `sum.golang.google.cn`；env hash 前后一致。 |
| `2026-07-03 13:20 +0800` | TEST-008 | `/tmp/issue550-worktree @ 796f7f218ae8` + fake `go run` early exit | pass | 退出码 1；输出 `backend startup failed for: auth file knowledge ai-gateway qa document gateway`；逐个打印 `.local/logs/<service>.log` tail。 |
| `2026-07-03 13:20 +0800` | TEST-003 | `git diff --check` | pass | 无 whitespace error。 |

未运行项：

| 测试项 | 未运行原因 | 缺失环境 | 残余风险 | 后续归属 |
| --- | --- | --- | --- | --- |
| 无 | 无 | 无 | 无 | 无 |

## 6. 缺陷与处理记录

| 问题 | 等级 | 处理结论 | 关联 issue / PR | 说明 |
| --- | --- | --- | --- | --- |
| 当前机器默认 PATH 缺 `go`，且无 native `psql` | 环境问题 | 已用临时 PATH 和 Dockerized `psql` wrapper 完成验收；未改系统配置 | 无 | 标准文档仍要求同一宿主机安装 Docker、Go、psql；脚本失败摘要能指出缺失工具。 |
| 当前旧 `deploy/.env` 缺少 `AUTH_GATEWAY_ADMIN_SERVICE_TOKEN` / `GATEWAY_AUTH_ADMIN_SERVICE_TOKEN` 等 `.env.example` 默认键 | 环境问题 | 正常路径用当前进程先加载 `.env.example` 再加载 `.env` 补齐；未改写用户文件 | 无 | Auth 早退时脚本正确打印日志尾部和非 Go mirror 排障提示。 |

未发现需要在本测试任务中修复的代码缺陷；未发现需要转 owner issue 的大问题。

## 7. 证据清单

| 证据类型 | 位置 / 链接 | 说明 |
| --- | --- | --- |
| Docker 状态 | `docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps` | 该 commit 上仅 `postgres`、`redis`、`legacy-vector-index`、`minio` running/healthy；当前默认 Compose 策略以本文开头说明为准。 |
| Go module failure output | `/tmp/issue550-go-failure.out` | 隔离 worktree 输出，包含默认 Go proxy、失败服务、有效设置和 remediation。 |
| Early-exit output | `/tmp/issue550-early-exit.out` | 隔离 worktree 输出，包含失败服务汇总和 7 个日志 tail。 |
| Host logs | `.local/logs/*.log` | 真实成功路径服务启动日志；历史 `parser.log` 为旧日志文件，不代表当前脚本启动 Parser。 |
| Health checks | 本报告第 5 节 | 7 个 host-run 服务 `/healthz` 返回 ok。 |

## 8. 风险与剩余缺口

- 当前 `deploy/.env` 是旧文件，缺少 `.env.example` 新增默认项。脚本按约定不改写用户 env；实际开发者应重新复制 `.env.example` 或手动补齐缺失键。
- 本机没有 native `psql`，本次通过 Dockerized `psql` wrapper 完成 seed 路径验证。标准使用者仍应安装宿主机 `psql`。
- 本轮不覆盖完整业务 E2E 或真实模型 provider；AI Gateway `/healthz` ok 不代表真实 provider readiness。

## 9. 最终结论

测试通过：#550 要求的正常启动、Go module 失败、服务早退和停止清理四类路径均有可复核证据；脚本没有改写用户 env；失败输出能指向下一步排查入口；文档/checker 已覆盖 Go proxy、Docker registry rewrite、`UV_DEFAULT_INDEX` 边界。

## 10. 复核清单

- [x] 已实际运行测试，而不是只补测试代码或测试清单。
- [x] 已记录执行命令、环境、结果和失败证据。
- [x] 已区分小问题和大问题，并按规则修复或转 issue。
- [x] 已记录所有未运行项的环境缺口和残余风险。
- [x] 已在测试 issue 和 PR 中链接本报告。
