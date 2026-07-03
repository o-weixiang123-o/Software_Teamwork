# Docker 基础设施镜像拉取环境

本手册只覆盖基础设施镜像拉取。仓库默认 Docker 路径只允许：

- `postgres`
- `redis`
- `qdrant`
- `minio`
- `minio-init`

业务服务、migration、seed、Knowledge runtime 和前端都不通过 Docker 启动。
Knowledge runtime 的 `uv sync` 下载 Python 包，不走 Docker registry。uv 默认包索引由
`deploy/.env.example` 里的 `UV_DEFAULT_INDEX` 控制；默认是官方 PyPI。中国大陆网络运行
`./scripts/local/dev-up.sh --china` 时会一并准备 runtime 依赖和 GitHub release/raw 等
artifact；如果用 `--skip-knowledge-runtime-deps` 跳过，可按
`services/knowledge-runtime/README.md` 手工补跑 runtime 下载脚本。不要把
`pyproject.toml` 或 `uv.lock` 的默认 URL 改成第三方代理。
Go 后端 host-run 期间的模块下载由 `deploy/.env.example` 里的 `GOPROXY` /
`GOSUMDB` 控制；默认是官方 `proxy.golang.org` / `sum.golang.org`。中国大陆网络用
`./scripts/local/dev-up.sh --china` 或 `./scripts/local/run-backend.sh --china`
临时切换 Go mirrors，也不属于 Docker registry 问题。

## 源策略契约

当前契约是默认官方源、国内网络显式 `--china`。旧的大陆优先默认镜像契约已废弃。
PR review 和 agent 检查应把 active 第三方镜像默认值视为回归；缺少这些 active 默认值
不是回归。中国大陆用户的一等路径是带 `--china` 的本地启动命令，或本机未提交的
`deploy/.env` / 企业镜像覆盖。

## 默认路径

默认使用 Compose 里的 Docker Hub pinned tags：

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
```

中国大陆网络拉取 Docker 基础设施镜像慢时，显式使用：

```bash
./scripts/local/dev-up.sh --china
```

该模式只在本次进程设置 `POSTGRES_IMAGE`、`REDIS_IMAGE`、`QDRANT_IMAGE`、
`MINIO_IMAGE`、`MINIO_MC_IMAGE` 和 `KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE`
的 DaoCloud registry rewrite，不改写
`deploy/.env`。

脚本内部会执行 Compose config、pull、up、migration 和 seed。只想验证 Docker 配置时：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env config --quiet
docker compose -f deploy/docker-compose.yml --env-file deploy/.env config --services
```

服务清单只能包含 `postgres`、`redis`、`qdrant`、`minio`、`minio-init`、
`elasticsearch`。

## Docker 镜像源选择

Compose 文件本身保留 Docker Hub pinned defaults。需要企业 registry 或长期本地
override 时，可只在本机 `deploy/.env` 设置 `POSTGRES_IMAGE`、`REDIS_IMAGE`、
`QDRANT_IMAGE`、`MINIO_IMAGE`、`MINIO_MC_IMAGE` 和
`KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE`，不要提交成默认值。已有旧
`deploy/.env` 如果仍保留 DaoCloud 值，脚本会尊重本地配置；不传 `--china` 时会提示
这是本地覆盖。

优先级固定为：

```text
registry rewrite > daemon mirror > proxy
```

原因：

- registry rewrite 通过 `--china` 或本地 `deploy/.env` 显式选择，团队可审查、可复制。
- daemon mirror 是个人机器状态，适合已有稳定镜像站的人。
- proxy 依赖 shell、Docker daemon 和系统代理是否同时生效，最容易出现“终端能访问但 Docker 不走代理”。

## 环境诊断

中国大陆 registry rewrite 路径：

```bash
python3 scripts/check_docker_environment.py --profile china --clean-env
```

Docker Hub 默认路径：

```bash
python3 scripts/check_docker_environment.py --profile default --clean-env
```

完整诊断：

```bash
python3 scripts/check_docker_environment.py --profile all --clean-env
```

CI 只跑无网络诊断：

```bash
python3 scripts/check_docker_environment.py --skip-network --clean-env
```

## 常见现象

拉取进度卡住时，先区分是哪个镜像卡住：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull postgres
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull redis
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull qdrant
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull minio
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull minio-init
```

如果 DaoCloud 路径异常，先用环境诊断脚本确认 manifest 是否可用，再决定是否临时去掉
`--china` / 本地 `*_IMAGE` override 走 Docker Hub、切换本机 daemon mirror，或配置
Docker daemon proxy。

`minio-init` 退出是正常行为。它是 `minio/mc` 客户端，完成 bucket 创建后退出。真正的
对象存储服务是 `minio`。

`ai-gateway /readyz` 返回 `503 degraded` 不是 Docker 问题。默认 seed 写入的是
placeholder provider credential；`/healthz` 成功表示服务进程可用。host-run
默认模型 profile 指向宿主机 `http://localhost:11434/v1`，不需要
`host.docker.internal`。

## 策略检查

修改 Docker 相关文件后运行：

```bash
python3 scripts/check_docker_policy.py
python3 -m unittest scripts.tests.test_check_docker_policy scripts.tests.test_check_docker_environment
docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet
```

策略要求：

- 根 Compose 只包含五个基础设施服务。
- 根 Compose 不包含 `build:`。
- 默认镜像不能是 `latest`。
- `deploy/.env.example` 不能默认启用第三方基础设施镜像 rewrite；大陆网络使用
  `./scripts/local/dev-up.sh --china` 或本地 `deploy/.env` override。
- 所有基础设施镜像 tag 必须 pinned，不能使用 `latest`。
