# 清理配置管理层并补中文文档

## Goal

基于当前分支，把仓库配置管理层整理成一个清晰、可执行、中文可读的本地/部署配置说明，并清理环境变量入口的历史兼容路径和迁移说明。

用户价值：后续开发者按 `config/`、`.env.example`、`.env.local`、`.local/config/` 的边界即可启动、排障和接入真实模型/PaddleOCR，文档只呈现当前入口，不保留旧路径对照说明。

## Confirmed Facts

- `config/` 已经是仓库级配置来源，包含 `base.yaml`、`dev.yaml`、`staging.yaml`、`production.yaml`、`schema.yaml` 和 `ctl/`。
- `scripts/config/load-profile.sh` 会调用 `config/ctl` 渲染 `.local/config/<profile>.env` 和 `.local/config/<profile>.env.sh`。
- 根 `.env.example` 是本地 secret 模板；用户复制成未跟踪的 `.env.local`。
- `AGENTS.md` 明确要求：`config/` 是唯一默认配置来源。
- 当前 `config/README.md` 是英文，和用户“文档用中文”的要求不一致。
- 仍有部分文档写历史环境入口或迁移提示，容易让维护者误以为存在多套默认配置来源。
- 当前配置层已能覆盖 AI Gateway local seed、Knowledge runtime AI Gateway embedding/rerank、PaddleOCR cloud parser 相关变量；这些边界需要在中文文档中说明。

## Requirements

- R1. 将配置管理层主文档改为中文，说明配置来源、文件职责、优先级、secret 规则、常用命令、模型/PaddleOCR 本地配置入口和验证命令。
- R2. 清理配置相关旧文档说法：默认启动路径统一指向 `.env.example` -> `.env.local` -> `config/ctl` -> `.local/config/`，正文不保留旧入口对照或迁移说明。
- R3. 保留生产实践边界：提交的 profile 只放非敏感默认值和 secret 引用；真实 API key/token/password 只在 `.env.local`、CI/CD secret、Vault/Kubernetes Secret 等外部 secret 系统中。
- R4. 明确 `--china` 是进程内镜像选择，不改写 `config/` 或 `.env.local`；默认官方源不能被文档误写成大陆镜像默认值。
- R5. 配置层代码只做必要整理，不引入新的运行时配置中心，不改变服务消费 env 的方式。
- R6. 更新或保留现有配置/seed 契约测试，确保文档清理不会破坏本地启动脚本约束。
- R7. 删除本机旧环境文件和 tracked 兼容提示文件；`.env.example` 与本机 `.env.local` 注释使用中文，`.env.local` 不提交。

## Acceptance Criteria

- [x] A1. `config/README.md` 为中文，并覆盖 profile、secret、渲染产物、模型/PaddleOCR、本地/部署边界和验证命令。
- [x] A2. 仓库默认路径相关文档只描述当前配置入口和启动前置步骤。
- [x] A3. `.env.example` 保持模板定位，不包含真实密钥；`.env.local` 不提交。
- [x] A4. `CONFIG_SECRET_FILE=.env.example ./scripts/config/load-profile.sh --print-compose-env` 可成功渲染。
- [x] A5. `cd config/ctl && go test ./...` 通过。
- [x] A6. `uv run --no-project --with pytest --with pyyaml python -m pytest scripts/tests -q` 通过。
- [x] A7. `python3 scripts/check_docker_policy.py` 和 `docker compose -f deploy/docker-compose.yml --env-file .local/config/dev.env config --quiet` 通过。
- [x] A8. `git diff --check` 通过。
- [x] A9. 活跃文档/spec/scripts 中不再出现旧环境入口或“旧策略已废弃”式迁移话术。
- [x] A10. `.env.example` 和本机 `.env.local` 注释已中文化；`.env.local` 仍保持 Git 忽略。

## Out of Scope

- 不迁移服务内部 config parser；服务继续读取环境变量。
- 不提交 `.env.local` 或真实 provider/PaddleOCR 密钥。
- 不新增 Vault/SOPS/Kubernetes Secret 实现；仅在文档中明确这些是生产 secret 注入方式。
- 不改 Docker Compose 的业务服务策略，不恢复业务服务容器。
