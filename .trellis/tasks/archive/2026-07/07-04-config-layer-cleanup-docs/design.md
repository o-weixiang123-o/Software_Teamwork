# Design

## Boundary

配置管理层只负责“把配置来源整理成可审计、可渲染的环境变量集合”。当前服务仍通过环境变量读取配置，不引入新的运行时配置服务或动态配置中心。

默认路径固定为：

```text
config/base.yaml + config/<profile>.yaml
  + .env.local / CI secret / process env
  -> scripts/config/load-profile.sh
  -> config/ctl render
  -> .local/config/<profile>.env      # Docker Compose --env-file
  -> .local/config/<profile>.env.sh   # host-run source
```

## Documentation Shape

- `config/README.md` 是配置层主入口，使用中文，覆盖：
  - 文件职责；
  - 优先级；
  - 本地启动；
  - 模型和 OCR 本地覆盖；
  - staging/production secret 注入；
  - 验证命令；
  - 常见反模式。
- 服务 README 只保留服务本身的运行说明，默认配置来源统一指向根 `.env.example`、`.env.local` 和 `config/README.md`。
- runbook 保留操作细节，但不重复维护另一套配置架构说明。

## Cleanup Rules

- 删除或替换历史环境变量入口表达。
- 活跃文档、spec 和脚本契约不保留旧入口兼容提示。
- `.env.example` 只展示模板和占位符。
- `config/base.yaml` 中的敏感项必须用 `fromEnv` + `sensitive: true`。
- `--china` 继续作为脚本临时 override；文档不能暗示默认配置就是 DaoCloud/TUNA/goproxy.cn。

## Compatibility

- 不改变现有脚本入口和 env key 名称。
- 不改变 `config/ctl` 的渲染行为。
- 如果契约测试依赖文档 token，随文档同步更新测试 token。

## Rollback

文档和配置模板改动可单独 revert。若发现启动脚本渲染异常，优先回滚 `config/base.yaml` 变更，再恢复 README 文案。
