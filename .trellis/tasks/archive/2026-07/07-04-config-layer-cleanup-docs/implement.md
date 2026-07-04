# Implementation Plan

## Checklist

1. [x] 读取配置层、启动脚本和旧文档中的配置路径引用。
2. [x] 改写 `config/README.md` 为中文配置管理主文档。
3. [x] 清理历史环境变量入口文档：优先处理 Knowledge runtime、Document、Document MCP 相关 README/docs，以及 runbook 中容易误导的片段。
4. [x] 检查 `.env.example` 与 `config/base.yaml` 的模型/PaddleOCR 模板是否一致。
5. [x] 如文档契约测试依赖旧 token，更新测试期望或文档文字，使测试仍验证真实约束而非过时路径。
6. [x] 运行配置层和脚本契约验证。
7. [x] 汇总剩余风险，不提交本地 secret 或生成物。
8. [x] 删除 tracked 兼容提示文件和本机旧环境文件，并将 `.env.example`、`.env.local` 注释中文化。

## Validation

- `cd config/ctl && go test ./...`
- `CONFIG_SECRET_FILE=.env.example ./scripts/config/load-profile.sh --print-compose-env`
- `uv run --no-project --with pytest --with pyyaml python -m pytest scripts/tests -q`
- `python3 scripts/check_docker_policy.py`
- `docker compose -f deploy/docker-compose.yml --env-file .local/config/dev.env config --quiet`
- `git diff --check`

## Risk Points

- 文档中仍可能有历史环境入口引用；需要用 `rg` 做最终扫描并区分历史报告和当前启动契约。
- `scripts/verify_local_seed_contract.py` 会校验文档 token，中文文档改写后可能需要同步测试。
- `.env.local` 含真实本机密钥，必须保持未跟踪。
