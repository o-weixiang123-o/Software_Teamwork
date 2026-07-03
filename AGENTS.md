<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## 项目前端说明

- 前端源码放在 `apps/web/src/`。
- 分支、PR、提交和合并策略以 `CONTRIBUTING.md` 为准；当前前端工作默认通过个人 fork 向主仓库 `develop` 发起 PR。
- 面向团队成员的协作流程见 `docs/collaboration/frontend-workflow.md`。
- 面向 agent 的前端规范见 `.trellis/spec/frontend/index.md`。
- 涉及前端开发、分支、PR、Lint 或 CI 时，优先加载项目级 `frontend-workflow` skill。

## Docker 与本地启动说明

- 本地联调入口见 `README.md`、`deploy/README.md` 和 `docs/runbooks/local-integration.md`。
- 本次更新后的标准启动命令是：
  `cp .env.example .env.local`，
  `./scripts/local/dev-up.sh`，
  `./scripts/local/run-backend.sh`，
  `cd apps/web && bun install && bun run dev`。
- 根级 Docker Compose 只允许拉取并启动基础设施：`postgres`、`redis`、`minio`、`minio-init`、`elasticsearch`。Auth、File、Knowledge、QA、Document、AI Gateway、Gateway、Parser 和前端都必须按文档在宿主机启动。
- 仓库默认路径不再维护业务服务容器、服务级 Compose、migration 容器或 seed 容器。业务服务必须走宿主机启动。
- `config/` 是唯一默认配置来源；根 `.env.example` 是本地 secret 模板，用户复制成未跟踪的 `.env.local`。启动脚本通过 `config/ctl` 渲染 `.local/config/<profile>.env` 和 `.env.sh`，不得重新维护 `deploy/.env` 默认变量。
- 当前源策略是默认官方源、国内网络显式 `--china`。旧的大陆优先默认镜像契约已废弃；不要把缺少 active DaoCloud/TUNA/goproxy 默认值标记为回归。
- `UV_DEFAULT_INDEX` 属于宿主机 uv/Python 包索引配置，默认放在 `config/base.yaml` 且默认指向官方 PyPI；不要把 uv 慢误判成 Docker registry 问题。中国大陆网络用显式 `--china` 或本机私有覆盖，不要把默认配置改成第三方镜像。
- Docker 镜像源、registry rewrite、daemon mirror、proxy、pull 卡顿和 WSL 内存排障见 `docs/runbooks/docker-image-pull-environment.md`。默认路径使用官方 pinned images；面向中国大陆网络的推荐路径是 `./scripts/local/dev-up.sh --china`，只在本次进程启用显式 registry rewrite；优先级为 `registry rewrite > daemon mirror > proxy`。
- GitHub release/raw 下载慢时，不要改写 committed `pyproject.toml`、`uv.lock` 或 OpenAPI 契约。Knowledge runtime 依赖和 artifact 下载优先由 `./scripts/local/dev-up.sh --china` 自动处理；只有使用 `--skip-knowledge-runtime-deps` 跳过后才按 `services/knowledge-runtime/README.md` 手工补跑 runtime 下载脚本。
- 改 Compose、基础设施镜像 tag、镜像源、Docker 环境诊断、Docker 文档或相关 Trellis spec 时，必须运行 `python3 scripts/check_docker_policy.py`、相关单元测试、`CONFIG_SECRET_FILE=.env.example ./scripts/config/load-profile.sh --print-compose-env` 和 `docker compose -f deploy/docker-compose.yml --env-file .local/config/dev.env config --quiet`。
- 不要把正常路径改成 `latest` 镜像；遇到镜像源异常时先按 Docker runbook 排查并记录环境阻断。

## 本地私有说明

- Agent 可在存在时读取 `.agents/local/AGENTS.local.md` 作为本机私有补充说明。
- 该文件应保持 Git 忽略状态，用于记录机器特定的代理、账号或凭据相关要求。
