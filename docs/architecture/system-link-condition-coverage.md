# 系统链路条件覆盖文档

本文从系统需求和当前 `develop` 文档出发，记录主要用户、管理员和系统后台链路。目标是让开发、测试和评审能够快速看到一次业务动作会经过哪些服务、每个依赖提供什么能力，以及至少需要覆盖哪些条件分支。

本文不是 Gateway OpenAPI 的逐 operation 矩阵。接口方法、字段、状态码和 schema 仍以 [Gateway OpenAPI 契约](../services/gateway/api/public.openapi.yaml) 和 [Gateway Active API Owner Map](../services/gateway/docs/active-api-owner-map.md) 为准；服务边界以 [服务边界矩阵](service-boundaries.md) 为准；当前实现状态以 [当前能力矩阵](current-capability-matrix.md) 和各服务 `docs/implementation.md` 为准。

## 阅读规则

- 链路按主要工作流归类，不按每个 endpoint 穷举。
- 每条链路都标出 owner service。拥有业务状态的服务负责校验业务规则和修改数据。
- `gateway` 可以暴露公开路径、注入上下文和归一化响应，但不得承载领域业务流程。
- `file`、Knowledge RAGFlow runtime、`ai-gateway`、Redis/asynq、runtime doc engine、Qdrant 和 File Service storage backend 等依赖只提供基础能力，不拥有调用方的领域状态。当前 Knowledge 主路径的对象、chunks 和索引归 RAGFlow runtime 边界，不等同于 File Service 或 Go Qdrant client。
- 标记“目标”或“缺口”的链路不能当作当前已实现能力宣传或验收。

## 全局不变量

| 编号 | 不变量 | 影响 |
| --- | --- | --- |
| G1 | 前端、管理端和其他公开 HTTP 调用方只能调用 `gateway` 的 `/api/v1/**`。 | 前端测试不得直连 `auth`、`file`、`knowledge`、`qa`、`document`、`ai-gateway`。 |
| G2 | 用户身份由 `auth` 签发，`gateway` 基于 Redis session cache 注入 `X-User-*` 和 `X-Request-Id`。 | 下游服务不能信任前端自填身份 header，仍需在本服务边界校验权限。 |
| G3 | PostgreSQL 是大多数 Go owner service 的业务事实来源。 | Redis/asynq 只做缓存、队列、短期协调；File Service 对象 bytes 由 File Service 封装；Knowledge 文档/chunks/索引由 RAGFlow runtime 管理并经 adapter 映射。 |
| G4 | 领域服务必须通过 `ai-gateway` 调模型。 | `gateway`、`qa`、`knowledge`、`document` 不得直接保存 provider key 或直连 provider。 |
| G5 | 通用文件对象通过 `file` 服务封装；Knowledge runtime 对象归 RAGFlow runtime 封装。 | Owner service 只保存不透明引用；公开响应不得暴露 bucket、object key、内部 URL、签名 URL 或存储凭据。 |
| G6 | Knowledge RAGFlow runtime 是当前文档处理运行时。 | Knowledge Go 进程不得引入 PaddleOCR/PaddlePaddle/OpenCV/CUDA 运行时依赖。 |
| G7 | OpenAPI active path 是协作契约，不等于全链路 smoke 已完成。 | 判断可演示能力时必须结合 implementation 文档和 runbook。 |

## 条件分类

后续链路中的“条件分支”使用下列分类，便于测试和评审按条件覆盖检查：

| 分类 | 需要覆盖的条件 |
| --- | --- |
| Auth | 未登录、token 无效或过期、已登录。 |
| Permission | 角色、权限字符串、owner 访问和权限不足分支；具体规则以各服务权限矩阵为准。 |
| Request | 合法请求、缺字段、字段非法、multipart/JSON 类型不匹配、分页或过滤参数越界。 |
| Resource | 资源存在、不存在、已删除、未 ready、状态冲突、重复名称或重复激活配置。 |
| Dependency | 下游可用、下游超时、下游 4xx/5xx、数据库/Redis/runtime doc engine/Qdrant/File storage/provider 不可用。 |
| Async | pending、queued、running、succeeded、failed、cancelled、retry、partial/占位状态。 |
| Streaming | 非流式、SSE 流式、客户端断开、事件回放、流式错误。 |
| Config | profile/config 存在、缺失、disabled、model mismatch、credential 未配置、service token 不匹配。 |
| Current State | 已实现、部分实现、占位、缺失、目标未落地、真实 smoke 缺失。 |
| Leakage | 不泄露 token、API key、prompt、provider 原始错误、MCP 原始参数/结果、object key、内部 URL、完整原文或向量 payload。 |

## 链路 1：认证与会话生命周期

**Owner**：`auth`。
**触发入口**：`POST /api/v1/users`、`POST /api/v1/sessions`、`DELETE /api/v1/sessions/current`、`GET /api/v1/users/me`。
**参与方**：前端、`gateway`、Redis、`auth`、Auth PostgreSQL。

**正常路径**

1. 前端通过 `gateway` 创建用户或会话。
2. `gateway` 转发给 `auth`。
3. `auth` 校验或创建用户，签发 opaque bearer token 和 session identity。
4. `gateway` 只用 token hash 写 Redis session cache，并把原始 access token 返回给前端一次。
5. 后续业务请求由 `gateway` 从 Redis 读取身份并注入下游 header。
6. 删除当前会话时，`gateway` 调 `auth` 撤销 session，再删除 Redis 缓存。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 创建用户和创建会话不需要 bearer auth；`/users/me` 和删除当前会话必须有有效 token。 |
| Request | 邮箱/用户名/密码等字段合法 vs 缺失/格式错误；登录凭证正确 vs 错误。 |
| Resource | 用户不存在、用户已存在、session 已撤销或过期。 |
| Dependency | Auth PostgreSQL 不可用；Redis 不可用导致 `gateway` 无法缓存或读取会话。 |
| Permission | 当前阶段主要依赖角色权限集合；管理端权限初始化仍需联调。 |
| Leakage | 原始 token 不得进入数据库、Redis 可读字段、日志、错误响应或链路追踪。 |

**输出/状态**

- `auth` PostgreSQL 保存用户、角色、权限、session token hash 和撤销状态。
- `gateway` Redis 保存短期 session cache。
- 前端只持有 opaque access token。

**当前状态**

- Auth 用户、会话和权限上下文标为“已实现”。
- Gateway/Auth/Redis 完整本地 E2E、种子数据和管理端权限配置仍需联调。

## 链路 2：Gateway 公开入口与 owner proxy

**Owner**：`gateway` 负责公开入口；业务状态归各 owner service。
**触发入口**：所有 `/api/v1/**` active paths、`/healthz`、`/readyz`。
**参与方**：前端、`gateway`、Redis、owner services。

**正常路径**

1. 前端调用 Gateway active path。
2. `gateway` 校验公开契约要求的认证。
3. 需要认证时，`gateway` 从 Redis 读取 session identity。
4. `gateway` 注入 `X-User-Id`、`X-User-Roles`、`X-User-Permissions`、`X-Request-Id` 和服务间 token。
5. `gateway` 代理到 owner service，并归一化 JSON error envelope。
6. 文件内容或 SSE 成功响应按二进制/流式协议透传，不包 JSON envelope。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 无 bearerAuth 的 health/auth create paths；需要 bearerAuth 的业务路径。 |
| Permission | Gateway 认证上下文和 owner service 复核责任见 [Gateway 权限矩阵](../services/gateway/docs/permission-matrix.md)。 |
| Resource | owner service 返回 not found、conflict、not ready 时由 gateway 映射为公开错误。 |
| Dependency | Redis 未命中、Redis 不可用、owner base URL 缺失、owner timeout。 |
| Streaming | QA SSE 流式转发 vs 普通 JSON；content 二进制代理 vs JSON 错误。 |
| Config | 非 `/admin` 前缀但声明 `x-required-permissions` 的管理资源（如 QA config）必须匹配具体权限；`admin` role 不能绕过这些显式权限。 |
| Current State | active operation 代表公开契约稳定，但真实 downstream smoke 仍可能缺失。 |
| Leakage | 不透传 SQL、MinIO、runtime index/Qdrant、provider、MCP 原始错误细节给前端。 |

**输出/状态**

- Gateway 不拥有业务数据库。
- Gateway 只拥有路由、session cache、request id、响应归一化和 metrics baseline。

**当前状态**

- Gateway active route proxy、session cache 和 admin aggregation 为“部分实现”。
- active route matrix 覆盖 110 个 operation；QA session attachments、Auth
  profile/password-change/admin-users 和 admin overview/metrics 已进入 active contracts。
- Admin overview/metrics 当前仍是 Gateway 稳定 `not_implemented` 占位；真实
  Redis/downstream 跨服务 smoke、admin aggregation 后端实现和跨服务 metrics
  运行证据仍未自动化。

## 链路 3：File 基础文件对象生命周期

**Owner**：`file` 只拥有基础 file object；业务资源归调用方 owner service。
**触发入口**：内部 `/internal/v1/files/**`；公开知识文档和报告文件路径分别由 `knowledge`、`document` 暴露。
**参与方**：`knowledge` 或 `document`、`file`、File PostgreSQL、File storage backend（MinIO/local/memory）。

**正常路径**

1. Owner service 接收公开 multipart 或生成文件 bytes。
2. Owner service 在自己的权限边界内校验业务资源。
3. Owner service 调 `file` 创建基础文件对象。
4. `file` 校验 service token、文件大小、content type、checksum。
5. `file` 保存 metadata 到 PostgreSQL 或 memory repository，保存 bytes 到 File Service storage backend。
6. Owner service 保存 `file_ref` 和展示元数据，但不向前端暴露 file 内部 ID。
7. 读取内容时，owner service 先校验业务可见性，再调用 `file` content API。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | `file` 只接受可信服务调用；前端不得直连。 |
| Permission | 业务权限由 `knowledge` 或 `document` 判断；`file` 只判断服务身份和基础操作权限。 |
| Request | multipart 缺文件、文件过大、checksum 不匹配、content type 不支持。 |
| Resource | file 存在、已删除、metadata 存在但 object 缺失。 |
| Dependency | File PostgreSQL 不可用、File storage backend 不可用、本地存储不可写。 |
| Async | 对象物理清理 worker 仍未实现，删除后清理可能是目标/后续链路。 |
| Leakage | 不返回 bucket、object key、内部 URL、MinIO 凭据、数据库连接串或完整敏感内容。 |

**输出/状态**

- `file` 保存基础 file metadata 和 File Service 内部存储引用。
- Owner service 保存业务资源 ID、业务状态、`file_ref` 和展示字段。

**当前状态**

- File 基础文件对象为“部分实现”。
- `/internal/v1/files/**`、PostgreSQL metadata runtime、memory/local/MinIO adapter 和 service-token 校验已存在。
- PostgreSQL + MinIO 联合 smoke 默认跳过；跨服务 smoke 仍缺。

## 链路 4：Knowledge RAGFlow runtime 文档处理

**Owner**：`knowledge` 拥有文档业务状态和 runtime 适配边界。
**触发入口**：`knowledge` adapter 上传文档并调用 RAGFlow runtime API/worker。
**参与方**：`knowledge`、`knowledge-runtime`、PostgreSQL、Redis、MinIO、Elasticsearch/索引后端。

**正常路径**

1. Knowledge adapter 创建 RAGFlow dataset，并把项目 parser config 映射为 RAGFlow `parser_config`。
2. Knowledge adapter 上传文档到 runtime dataset。
3. 宿主机 runtime worker 读取任务，执行 PDF 解析、版面/OCR、切块、embedding 和索引写入。
4. Knowledge adapter 轮询 runtime 文档状态和 chunks，并把结果映射成项目内部文档/chunk/检索响应。
5. Knowledge 保持 gateway-facing 权限、错误 envelope、request id 和敏感信息脱敏规则。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Request | 文件过大、content type 不支持、请求超时、runtime 返回非成功 code。 |
| Dependency | Runtime API/worker 不可用、PostgreSQL/Redis/MinIO/Elasticsearch/provider 不可用、模型资源缺失。 |
| Async | Runtime parse/index 任务 pending/running/succeeded/failed/retry。 |
| Config | Parser config 缺失使用 fallback；管理员 parser config 由 Knowledge 管理并映射到 RAGFlow。 |
| Resource | Runtime dataset/document 不存在、文档已删除或 tenant 不匹配。 |
| Leakage | Knowledge 不返回 object key、bucket、内部路径、OCR debug output、prompt、provider body 或 secret。 |

**输出/状态**

- Runtime 保存处理所需的运行时数据和索引数据。
- Knowledge adapter 输出项目契约中的文档状态、chunks、content 和检索结果。

**当前状态**

- 旧 `services/parser` 已退役。
- Knowledge RAGFlow runtime API/worker 已成为当前 PDF 解析、切块、embedding、索引和检索链路。
- 真实 PDF E2E 需要本地 runtime、对象存储、索引后端和 provider credential。

## 链路 5：Knowledge 知识库与文档生命周期

**Owner**：`knowledge`。
**触发入口**：`/api/v1/knowledge-bases/**`、`/api/v1/documents/**`、`/api/v1/admin/parser-configs/**`。
**参与方**：前端、`gateway`、`knowledge`、Knowledge adapter PostgreSQL（parser config）、RAGFlow runtime、runtime PostgreSQL、MinIO、Elasticsearch/索引后端。

**正常路径**

1. 用户通过 Gateway 创建或维护知识库。
2. 用户向知识库上传文档，Knowledge adapter 创建 runtime dataset/document 并上传文件。
3. RAGFlow runtime 保存文档、metadata、tags、处理状态和索引所需运行时状态。
4. runtime worker 推进文档到 ready，adapter 将状态/chunks/content 映射为项目契约。
5. 用户可查询文档详情、列表、chunks、content，或更新 tags、软删除文档。
6. 删除文档时 Knowledge adapter 执行 runtime 文档删除/软删除映射；runtime 内部对象和索引清理需要后续 smoke 证明。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 所有业务路径需要 bearer token；admin parser configs 的认证入口见 [Gateway 权限矩阵](../services/gateway/docs/permission-matrix.md)。 |
| Permission | 知识库可见性、parser config 管理权限和 forbidden 分支见 [Knowledge 权限矩阵](../services/knowledge/docs/permission-matrix.md)。 |
| Request | 创建/更新知识库字段合法 vs validation_error；上传文件合法 vs multipart/大小/content type 错误。 |
| Resource | knowledge base 存在、已删除；document 存在、已删除、processing、ready、failed。 |
| Dependency | RAGFlow runtime API/worker 不可用、runtime PostgreSQL/MinIO/Elasticsearch/provider 失败、adapter PostgreSQL parser-config 管理不可用。 |
| Async | runtime processing pending/running/succeeded/failed/retry；adapter 需要把 runtime 状态稳定映射为公开 `DocumentStatus`。 |
| Current State | document lifecycle、chunks/content、`knowledge-queries` active routes 已转 owner proxy；真实 runtime/provider/MCP 端到端 smoke 仍缺。 |
| Leakage | 不暴露 runtime object key、索引 payload、embedding model、provider body、内部 URL 或凭据。 |

**输出/状态**

- RAGFlow runtime 保存文档、chunks、embedding、索引和检索运行时事实。
- Knowledge adapter 暴露项目契约，parser configs 可选保存在 adapter PostgreSQL。

**当前状态**

- Knowledge 知识库 CRUD、上传和 runtime 写入链路为“部分实现”。
- 已有 KB CRUD、文档上传、RAGFlow runtime API/worker 适配、parser config 映射、document lifecycle、chunks/content、`knowledge-queries` 和 Knowledge MCP server 基础实现。
- 完整 Gateway/MCP/#125 E2E、真实 provider 运维、QA 默认 MCP 接入和并发/外部副作用一致性仍需补齐。

## 链路 6：Knowledge 入库处理链路

**Owner**：`knowledge`。
**触发入口**：文档上传后 adapter 调 RAGFlow runtime 创建 document 并按配置触发 parse/index。
**参与方**：`knowledge`、`knowledge-runtime`、runtime Redis/worker、MinIO、Elasticsearch/索引后端、可选 `ai-gateway` provider、Knowledge adapter PostgreSQL（parser config）。

**正常路径**

1. Knowledge adapter 创建 RAGFlow dataset/document，并将业务 parser config 映射为 runtime `parser_config`。
2. Adapter 上传文档到 runtime 并触发 parse/index。
3. Runtime worker 读取任务，执行 PDF 解析、切块、embedding 和索引写入。
4. Adapter 轮询 runtime 文档状态和 chunks。
5. Adapter 将 runtime 结果映射为 Knowledge 文档状态、chunks/content 和 `knowledge-queries` 响应。
6. 文档状态推进到 ready，失败则记录脱敏错误和尝试摘要。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Async | job queued、running、succeeded、failed、retry；重复投递需要幂等。 |
| Resource | 文档处理期间被删除；知识库被删除；runtime document/content 不存在。 |
| Config | parser config 匹配 vs fallback；runtime provider/索引配置可用 vs 缺失；AI profile missing/disabled/model mismatch。 |
| Dependency | Knowledge runtime、runtime PostgreSQL、MinIO、Elasticsearch/索引后端、AI Gateway provider 任一失败。 |
| Request | 原文件类型支持 vs 不支持；parsed content 为空或质量不足。 |
| Current State | RAGFlow runtime adapter、runtime route/contract tests 和真实 PDF E2E 路径已落地；完整 Gateway/MCP/#125 E2E 和真实 provider 运维仍未闭环。 |
| Leakage | worker 日志和 job error 不得包含完整文档全文、object key、prompt、API key、向量 payload。 |

**输出/状态**

- runtime chunks 和最小索引 payload 经 adapter 映射为检索稳定交接面。
- DocumentStatus 可被公开查询。
- 失败状态应保留可排查但脱敏的摘要。

**当前状态**

- 入库主链路为“部分实现”。
- 当前测试包含 fake vendor/runtime、targeted runtime tests 和真实 PDF E2E 路径；真实共享环境联调仍是关键缺口。

## 链路 7：Knowledge 检索与 `knowledge-queries`

**Owner**：`knowledge`。
**触发入口**：`POST /api/v1/knowledge-queries`，也可由 QA 检索测试或后续报告上下文间接调用。
**参与方**：前端或领域服务、`gateway`、`knowledge`、RAGFlow runtime、runtime PostgreSQL、Elasticsearch/索引后端、可选 `ai-gateway` provider。

**正常路径**

1. 调用方提交 query、knowledgeBaseIds、topK、scoreThreshold、tags、metadataFilter 和 rerank 配置。
2. Knowledge 校验用户对知识库的可见性。
3. Knowledge adapter 调 RAGFlow runtime 执行检索。
4. Runtime 使用配置的 embedding/索引后端召回候选。
5. Adapter 将 runtime 结果映射为项目 chunks、documents、knowledge bases。
6. Knowledge 过滤未 ready、已删除或不可见文档。
7. 可选调用 AI Gateway rerank。
8. 返回查询摘要、召回结果、分数和 trace。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 公开调用必须认证；服务间调用仍需可信上下文。 |
| Permission | 有权限知识库 vs forbidden；部分 knowledgeBaseIds 不可见。 |
| Request | query 为空、topK 越界、filter 非法、rerank 配置非法。 |
| Resource | 无 ready 文档、命中文档已删除、chunk hydrate 失败。 |
| Config | runtime embedding/索引/provider 可用 vs 缺失；rerank disabled、no-op fallback、AI Gateway profile 缺失或 model mismatch。 |
| Dependency | runtime/索引后端不可用、runtime PostgreSQL 不可用、AI Gateway rerank 失败。 |
| Current State | `knowledge-queries` 已实现 adapter contract 和 Gateway proxy；真实 provider/rerank smoke 仍缺。 |
| Leakage | 不返回原始向量、完整 runtime/index payload、prompt、object key、provider 原始响应体。 |

**输出/状态**

- 返回可展示的 `documentId`、`chunkId`、`documentName`、`sectionPath`、`score`、`contentPreview` 和 trace。
- 不改变索引事实，除非是后续重处理任务。

**当前状态**

- Knowledge 检索为“部分实现”。
- 最新 develop 已包含 `knowledge-queries` 和 RAGFlow runtime adapter，但真实 provider/rerank/QA MCP 闭环仍未证明。

## 链路 8：AI Gateway 模型配置和模型调用

**Owner**：`ai-gateway`。
**触发入口**：公开管理端通过 `/api/v1/admin/model-profiles/**` 进入 Gateway；内部模型调用通过 `/internal/v1/chat/completions`、`/internal/v1/embeddings`、`/internal/v1/rerankings`。
**参与方**：管理员、`gateway`、`ai-gateway`、AI Gateway PostgreSQL、外部 provider、调用方领域服务。

**正常路径**

1. 管理员通过 Gateway 管理 model profile。
2. Gateway 做管理员鉴权和响应归一化，转发到 AI Gateway。
3. AI Gateway 保存 profile、provider、model、默认参数、超时和 credential 写入状态。
4. `qa`、`knowledge` 或 `document` 使用内部 service token、`X-Caller-Service` 和 request id 调模型 endpoint。
5. AI Gateway 解析 profile，校验 purpose、enabled、credential 和 model exact-match。
6. AI Gateway 调 provider 并归一化响应或错误。
7. AI Gateway 保存脱敏 invocation summary 和 usage aggregate。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 管理端 profile API 需要 bearer auth；内部调用需要 service token。 |
| Permission | Model profile 管理权限见 [AI Gateway 权限矩阵](../services/ai-gateway/docs/permission-matrix.md)。 |
| Request | profile 字段非法、敏感 default parameter、chat/embedding/rerank body 非法。 |
| Resource | profile 不存在、disabled、deleted、credential 未配置、purpose 不匹配。 |
| Config | model 与 profile model 精确匹配 vs mismatch；默认 profile 存在 vs 缺失。 |
| Dependency | PostgreSQL 不可用、provider 超时、provider 401/403/429/5xx。 |
| Streaming | chat 非流式、chat streaming、stream cancel、provider chunk 异常。 |
| Current State | chat、embedding、rerank 均已实现；真实 provider smoke 和跨服务接入仍需补。 |
| Leakage | 不保存或返回 API key、provider bearer token、完整 prompt、embedding payload、rerank 文档正文、provider 原始 body。 |

**输出/状态**

- Profile 管理响应只暴露 `apiKeyConfigured` 等脱敏字段。
- 模型调用返回 OpenAI-compatible body 或 OpenAI-style error。
- Invocation summary 只保存低敏摘要。

**当前状态**

- AI Gateway model profile、credential 安全、chat、embedding、rerank 标为“已实现”。
- 真实 provider smoke、secret manager、token 轮换、Knowledge/QA/Document 跨服务接入验证仍需补。

## 链路 9：QA 会话、回答运行、SSE、工具和引用

**Owner**：`qa`。
**触发入口**：`/api/v1/qa-sessions/**`（含 session attachments）、`/api/v1/response-runs/**`、`/api/v1/messages/{messageId}/citations`、`/api/v1/citations/**`、配置、检索测试和指标路径。
**参与方**：前端、`gateway`、`qa`、QA PostgreSQL、`file`、Knowledge RAGFlow runtime、`ai-gateway`、MCP Client/MCP servers、`knowledge`、可选 `document` MCP server。

**正常路径**

1. 用户创建或选择 QA session。
2. 用户可上传会话临时附件；QA 保存附件 metadata/状态，内部调用 file 保存原始 bytes，解析时走 Knowledge/RAGFlow runtime 边界，并保存临时 chunk。
3. 用户创建 message，可关联 ready attachments，并可请求 JSON 或 `text/event-stream`。
4. QA 创建用户消息、助手占位、response run、初始事件。
5. QA 加载 QA/LLM config，准备工具白名单、会话附件上下文、知识库检索上下文、全局 `systemPrompt` snapshot 和模型上下文；长期 Knowledge RAG 和本会话附件检索均可被模型选择，二者不互斥。
6. QA 调 AI Gateway chat/function calling。
7. 若模型返回 tool calls，QA 通过 MCP Client 执行 `tools/call`，保存脱敏 tool-call summary；Document report-generation 工具结果只映射为 `reportArtifact` 安全摘要。
8. QA 将工具结果裁剪脱敏后追加为 tool message，继续下一轮 ReAct。
9. QA 生成最终回答，保存消息、run 状态、model invocation summary、citations 和 SSE replay events。

会话附件只生成 QA 临时 chunks 并通过 `search_session_attachments` 查询，不自动写入
Knowledge 长期知识库。长期知识库检索通过内置 `search_knowledge` 或 Knowledge MCP
`knowledge__search` 访问 Knowledge 拥有的索引；请求级和默认 `knowledgeBaseIds` 只用于
收窄检索范围。QA 默认 `knowledgeBaseIds` 为空表示使用项目全局长期知识库，不按最终用户
的 Knowledge 管理权限或 runtime 用户可见数据集收窄；禁用长期 RAG 应通过工具白名单移除
长期知识库检索工具。
10. 前端查询 response run、tool calls、citations、retrieval test 或 metrics。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 所有 QA 公开路径需要 bearer token。 |
| Permission | QA owner 约束、settings 管理权限和管理员跨用户限制见 [QA 权限矩阵](../services/qa/docs/permission-matrix.md)。 |
| Request | session/message/config/test 请求合法 vs validation_error。 |
| Resource | session 不存在/已删除；message/run/citation 不存在；事件回放为空；引用来源不可用。 |
| Config | QA config 缺失、`systemPrompt` 缺失/继承/超过 20000 bytes、LLM profile 缺失、tool whitelist 不允许、AI Gateway token/profile 不匹配、Document MCP server alias 未注册、Knowledge MCP alias/endpoint/allowlist 未收敛。 |
| Dependency | File 或 Knowledge runtime 处理会话附件失败、AI Gateway 失败、Knowledge retrieval 失败、MCP server 不可用、Document worker/download 链路不可用、PostgreSQL 失败。 |
| Async | response run completed、model_error、timeout、cancelled、max_iterations；PATCH 取消运行。 |
| Streaming | 非流式回答、SSE answer.delta、tool events、error event、heartbeat、断线后 events replay。 |
| Current State | QA session/message/attachments/SSE/config/citation/retrieval test/metrics active paths 已存在；QA config version `systemPrompt` 契约已进 Gateway OpenAPI 和前端生成类型，完整 prompt 仅 admin config 端点可返回；最小 Gateway -> Knowledge -> QA RAG smoke 和 QA -> Document MCP report tools smoke 可显式运行；Knowledge MCP server 已存在但默认 QA 接入、四个 `knowledge__*` 目标工具和 citation 仍未收敛；完整 QA + Knowledge + AI Gateway + Document MCP + frontend smoke 未证明。 |
| Leakage | 不返回私有 chain-of-thought、完整 prompt 或 `systemPrompt`、MCP 原始参数/结果、内部 URL、原始文档全文、provider 原始错误、attachment/file object key。 |

**输出/状态**

- QA PostgreSQL 保存 sessions、messages、session attachments、attachment chunks、response_runs、agent_model_invocations、agent_tool_calls、response_stream_events、citations、config versions、retrieval test runs 和 metrics 所需事实。
- 前端只看到安全处理摘要、SSE 事件、脱敏工具调用摘要、`reportArtifact` 摘要和引用快照。

**当前状态**

- QA 会话、消息、附件、配置、引用和统计资源为“部分实现”。
- ResponseRun Agent Loop、function-calling adapter、SSE heartbeat/replay safeguards、versioned/global `systemPrompt` 管理员配置契约、MCP/local tool 基础、Document report artifact 摘要映射、QA -> AI Gateway env-gated smoke、Gateway -> Knowledge -> QA 最小 RAG smoke 和 QA -> Document MCP report tools env-gated smoke 已实现；Knowledge MCP 仍需要默认 QA 配置、工具名和 citation 收敛。
- 真实 provider、prompt 生效且不向普通 QA responses/SSE/tool summary/error/log/metrics 泄漏的完整 E2E、citation snapshot/detail/batch query、artifact 持久化回放恢复、前端完整 E2E 和 #125 完整跨服务 smoke 待收口。

## 链路 10：QA 检索体验测试和指标

**Owner**：`qa`，正式知识检索仍由 `knowledge` 执行。
**触发入口**：`POST /api/v1/retrieval-test-runs`、`GET /api/v1/retrieval-test-runs/{testRunId}`、`/api/v1/qa-metrics/**`。
**参与方**：管理员、`gateway`、`qa`、QA PostgreSQL、`knowledge`。

**正常路径**

1. 管理员发起检索体验测试。
2. QA 使用当前 QA config 或请求参数构造 Knowledge query；retrieval 测试不得暴露完整 `systemPrompt`。
3. QA 调 Knowledge retrieval client。
4. QA 保存测试 run 和脱敏结果快照。
5. 管理员查询 test run 或 QA metrics。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Permission | QA 配置、检索测试和指标权限见 [QA 权限矩阵](../services/qa/docs/permission-matrix.md)。 |
| Request | query、knowledgeBaseIds、threshold、topK 合法 vs validation_error。 |
| Resource | QA config 不存在、知识库不存在、无命中、testRun 不存在。 |
| Dependency | Knowledge 返回 dependency_error、timeout、validation/forbidden/not_found。 |
| Current State | 最新 develop 包含 QA retrieval tests 修复和 Gateway -> Knowledge -> QA 最小 RAG smoke；完整 RAG/citation/前端 E2E 仍是缺口。 |
| Leakage | 测试结果只保存可展示摘要，不保存完整内部 query payload、prompt、`systemPrompt`、provider body、object key。 |

**输出/状态**

- QA 保存 retrieval test run 和 result snapshot。
- QA metrics 默认从 QA 权威表聚合，必要时可调用 Knowledge 获取知识库/文档数量。

**当前状态**

- Retrieval test/metrics 属于 QA “部分实现”能力。
- Gateway -> Knowledge -> QA 最小 RAG smoke 可显式运行；更完整的 Gateway/Auth/Knowledge/AI Gateway/provider/citation 场景仍需补。

## 链路 11：Document 报告资源、任务和导出

**Owner**：`document`。
**触发入口**：`/api/v1/report-*` 和 `/api/v1/reports/**`。
**参与方**：前端、`gateway`、`document`、Document PostgreSQL、Redis/asynq、`file`、`ai-gateway` 和可选 `knowledge`。

**正常路径**

1. 用户查询 report types/templates。
2. 用户创建 report，Document 保存报告草稿和 owner。
3. 用户创建 `outline_generation`、`content_generation` 或 section regeneration job，Document 保存 job/attempt/event 并投递 asynq task。
4. Worker 推进 job/attempt running/succeeded/partial_succeeded/failed。
5. 对 `summer_peak_inspection`，worker 通过 AI Gateway chat 生成基础大纲、章节骨架和逐章节正文；请求带知识库检索参数且配置了 Knowledge 时，可先获取安全 `contentPreview` 上下文。
6. 用户可同步创建或编辑 outline、sections、section versions；重新生成不得隐式覆盖用户编辑。
7. 创建 report file 时，worker 读取 report 和已保存章节，使用内置 `SimpleDOCXGenerator` 生成基础 DOCX。
8. Worker 调 `file` 保存 DOCX bytes，回写 `ReportFile(fileRef, fileSize, status=succeeded)`。
9. 用户通过 report file content 读取已成功生成的文件。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Auth | 所有 Document 公开路径需要 bearer token。 |
| Permission | Document 报告 owner 约束、settings 管理权限和拒绝规则见 [Document 权限矩阵](../services/document/docs/permission-matrix.md)。 |
| Request | reportType、templateId、jobType、section payload、multipart 文件合法 vs validation_error。 |
| Resource | template/material/report/outline/section/job/file 存在、已删除、未 ready、状态冲突。 |
| Dependency | Document PostgreSQL、Redis/asynq、File Service、AI Gateway、可选 Knowledge 不可用。 |
| Async | report job pending/running/succeeded/partial_succeeded/failed；attempt 创建、重试；events 轮询。 |
| Config | report settings 缺失、AI Gateway profile 引用无效、默认模板缺失、Pandoc/LibreOffice path 只是预留。 |
| Current State | 模板/素材/报告/大纲/章节、job 状态机、`summer_peak_inspection` 基础 AI 大纲/正文生成、基础 DOCX 导出、服务内 Document MCP tool adapter 和 Streamable HTTP MCP server 已实现；QA 侧 Document MCP 子场景已有 env-gated smoke；更多报告类型生成策略、富 DOCX 运行时和完整跨服务 smoke 仍缺。 |
| Leakage | 不返回 `file_ref`、file 内部 ID、object key、MinIO URL、prompt、provider 原始错误、API key、完整工具私有参数。 |

**输出/状态**

- Document PostgreSQL 保存报告业务状态、job、attempt、event、settings、statistics、operation logs。
- File 保存模板、材料和基础 report file DOCX bytes。
- 前端通过 Document-owned report IDs 和 content 子资源访问业务文件。

**当前状态**

- Document 模板、素材、报告、大纲、章节、report jobs/attempts/events、settings/statistics/logs、`summer_peak_inspection` 基础 AI 大纲/正文生成编排、服务内 Document MCP tool adapter 和 Streamable HTTP MCP server 已实现。
- Report files/content 和基础 DOCX 导出已完成服务内基础闭环，但仍依赖 File Service 内容可读和跨服务 smoke。
- QA -> Document MCP report tools smoke 可显式连接 Document MCP endpoint 并验证 `reportArtifact` 安全摘要；真实 provider 触发的 QA Agent 端到端、更多报告类型生成策略、Pandoc 富 DOCX 运行时和 #125 完整跨服务 content smoke 仍缺。

## 链路 12：Document 管理配置、统计和操作日志

**Owner**：`document`。
**触发入口**：`/api/v1/report-settings`、`/api/v1/report-statistics/**`、`/api/v1/report-operation-logs`。
**参与方**：管理员、`gateway`、`document`、Document PostgreSQL、AI Gateway profile client。

**正常路径**

1. 管理员读取或更新 report settings。
2. Document 保存 AI Gateway profile 引用、默认模板和文件默认值。
3. Document 可通过 AI Gateway profile client 校验 profile 引用。
4. 管理员读取统计概览、每日趋势和操作日志。
5. 操作日志保存脱敏参数摘要和结果摘要。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Permission | Report settings、统计和日志权限见 [Document 权限矩阵](../services/document/docs/permission-matrix.md)。 |
| Request | settings patch 合法 vs 字段非法；统计时间范围合法 vs 越界。 |
| Resource | 默认模板/profile 不存在、已删除、disabled。 |
| Dependency | AI Gateway profile 校验失败、Document PostgreSQL 不可用。 |
| Current State | settings、statistics、operation logs 已在服务端基础实现；管理端和跨服务 smoke 仍需补。 |
| Leakage | settings/logs 不保存或返回 provider API key、prompt、内部 URL、object key、完整工具参数。 |

**输出/状态**

- Document 保存配置版本和脱敏操作日志。
- 统计只反映 Document-owned 报告域，不等同于跨服务 admin metrics。

**当前状态**

- 属于 Document “部分实现”能力。
- Document 自身 statistics/logs 已进入 active paths；跨服务 admin overview/metrics 由 Gateway 聚合且已是 active contract，仍缺完整运行证据。

## 链路 13：管理端 runtime configuration

**Owner**：模型 profile 归 `ai-gateway`；parser config 归 `knowledge`；QA runtime config 归 `qa`。
**触发入口**：`/api/v1/admin/model-profiles/**`、`/api/v1/admin/parser-configs/**`、`/api/v1/qa-config-versions/**`。
**参与方**：管理员、`gateway`、`ai-gateway`、`knowledge`、`qa`。

**正常路径**

1. 管理员通过 Gateway 访问 admin runtime config。
2. Gateway 做 bearer auth、管理员权限检查和响应归一化。
3. Model profile 请求转发给 AI Gateway。
4. Parser config 请求转发给 Knowledge。
5. QA config 请求转发给 QA；`GET/POST /api/v1/qa-config-versions` 是完整 `systemPrompt` 唯一公开返回面。
6. Owner service 保存配置、校验冲突和脱敏响应。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Permission | Admin/runtime config 权限见 [Gateway 权限矩阵](../services/gateway/docs/permission-matrix.md)、[AI Gateway 权限矩阵](../services/ai-gateway/docs/permission-matrix.md)、[Knowledge 权限矩阵](../services/knowledge/docs/permission-matrix.md) 和 [QA 权限矩阵](../services/qa/docs/permission-matrix.md)。QA config 路径虽然不是 `/api/v1/admin/**`，仍必须要求 `qa:settings:read/write`。 |
| Request | 创建/更新字段合法 vs validation_error；重复名称 conflict。 |
| Resource | profile/config 存在、不存在、deleted、enabled/disabled。 |
| Config | API key write-only；parser config fallback；model profile purpose/model/dimensions/topN 校验；`systemPrompt` 创建时可继承当前 active prompt，管理员写入按 1-20000 bytes 校验。 |
| Dependency | AI Gateway、Knowledge 或 QA 不可用，Gateway 返回 dependency_error。 |
| Leakage | 不返回 API key 明文、parser 内部路径、provider 原始错误或 secret ref；完整 `systemPrompt` 只能在 QA admin config 响应中出现，不得出现在普通 QA 返回面。 |

**输出/状态**

- AI Gateway 保存 provider/profile/credential 写入状态。
- Knowledge 保存 parser backend、并发限制和文档处理行为配置。
- QA 保存 versioned retrieval/agent/tool whitelist 和全局 `systemPrompt` 配置；历史 response runs 引用当时配置版本。

**当前状态**

- Admin model profile、parser config 和 QA config version 都是 active Gateway 契约。
- AI Gateway core profile 能力已实现；parser config 管理已在 Knowledge 落地。
- QA config version `systemPrompt` contract 已进入 Gateway OpenAPI 和前端生成类型；完整运行时 prompt 生效/防泄漏 E2E 仍需跨服务验收。

## 链路 14：本地联调、ready 和 smoke

**Owner**：各服务负责自己的 ready；跨服务 smoke 仍是当前缺口。
**触发入口**：`deploy/docker-compose.yml`、host-run migrations/seed、`/readyz`、env-gated tests。
**参与方**：所有 host-run 服务、PostgreSQL、Redis、MinIO、Qdrant（本地 infra）、Knowledge RAGFlow runtime、runtime Elasticsearch/索引后端、AI Gateway/provider。

**正常路径**

1. 本地先跑单服务 test 和 host-run 启动命令。
2. 有 migration 的服务执行 goose apply smoke。
3. 启动 infra Compose，再启动 Auth、Gateway、目标领域服务和对应数据库连接。
4. 需要模型调用时启动 AI Gateway 并创建对应 `purpose=chat|embedding|rerank` 的 enabled/default profile。
5. 需要文件 bytes 时启动 File Service。
6. 前端可见能力通过 Gateway public `/api/v1/**` 验证；服务间 smoke 才直连 `/internal/v1/**`。

**条件分支**

| 分类 | 分支 |
| --- | --- |
| Dependency | 根级 Compose 只启动 PostgreSQL、Redis、Qdrant、MinIO 和 `minio-init`，业务服务必须 host-run；该依赖基线不证明完整 E2E。 |
| Config | `deploy/.env.example` / `deploy/.env`、service token hash、AI profile seed、NO_PROXY/proxy、host-run 端口设置。 |
| Resource | seed data 只覆盖本地登录、基础报告类型、示例知识库、QA 会话样例和 AI profile placeholder。 |
| Current State | File PostgreSQL + MinIO smoke、Knowledge RAGFlow runtime PDF E2E、Gateway -> Knowledge -> QA RAG smoke、QA -> Document MCP report tools smoke 和 Issue #125 smoke slices 可显式启用；AI Gateway real provider smoke env-gated。 |
| Leakage | 本地日志和失败输出不应包含 token、API key、数据库连接串、object key、完整 prompt。 |

**输出/状态**

- 服务 ready 只能证明服务及必要依赖满足本服务最低条件。
- 单服务 smoke 不等同于前端到多服务全链路验收。

**当前状态**

- 本地联调环境为“部分实现”。
- 根级 Compose 是本地基础设施基线，不启动业务服务容器，不是生产部署基线，也不是完整一键 E2E smoke；AI Gateway placeholder profile 不代表真实 provider 可用。
- 当前 seed 和 runbook 已覆盖 QA 会话样例、report sample、最小 RAG smoke
  和 QA/Document MCP 子场景；这些仍不能替代前端到所有后端服务的一键 E2E。

## 条件覆盖检查表

下表用于确认主要条件类型至少在某些链路中被显式覆盖。它不是测试用例清单，测试实现仍应回到对应服务和 OpenAPI。

| 链路 | Auth | Permission | Request | Resource | Dependency | Async | Streaming | Config | Current State | Leakage |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 认证与会话 | Y | Y | Y | Y | Y | N/A | N/A | N/A | Y | Y |
| Gateway proxy | Y | Y | N/A | Y | Y | N/A | Y | Y | Y | Y |
| File 对象 | Y | Y | Y | Y | Y | Y | N/A | Y | Y | Y |
| Knowledge runtime 文档处理 | N/A | N/A | Y | Y | Y | Y | N/A | Y | Y | Y |
| Knowledge 生命周期 | Y | Y | Y | Y | Y | Y | N/A | Y | Y | Y |
| Knowledge 入库 | N/A | Y | Y | Y | Y | Y | N/A | Y | Y | Y |
| Knowledge 检索 | Y | Y | Y | Y | Y | N/A | N/A | Y | Y | Y |
| AI Gateway | Y | Y | Y | Y | Y | N/A | Y | Y | Y | Y |
| QA 回答 | Y | Y | Y | Y | Y | Y | Y | Y | Y | Y |
| QA 检索测试/指标 | Y | Y | Y | Y | Y | N/A | N/A | Y | Y | Y |
| Document 报告 | Y | Y | Y | Y | Y | Y | N/A | Y | Y | Y |
| Document 管理 | Y | Y | Y | Y | Y | N/A | N/A | Y | Y | Y |
| Runtime config | Y | Y | Y | Y | Y | N/A | N/A | Y | Y | Y |
| 本地联调 | N/A | N/A | N/A | Y | Y | N/A | N/A | Y | Y | Y |

## 当前不能承诺的链路

以下链路在需求或目标设计中存在，但当前不能作为已完成能力验收：

- 一键前端到 Auth/Gateway/File/Knowledge/RAGFlow runtime/runtime doc engine/AI Gateway/QA/Document 的完整 E2E smoke。
- Knowledge 上传到真实 runtime、真实索引后端、真实 AI Gateway embedding/rerank provider 和 Gateway/MCP 总入口的端到端验收；当前 PDF E2E、ingestion real deps 和 Gateway RAG smoke 是显式 opt-in，不能替代完整 #125。
- QA 完整 RAG/citation 闭环，包括真实 provider、`systemPrompt` 生效且不向普通 QA 返回面泄漏、真实 Knowledge retrieval/rerank trace、citation snapshot/detail/batch query、artifact 持久化回放恢复、前端和跨 Gateway/Auth smoke。
- Document `coal_inventory_audit` 等更多报告类型的 AI 生成业务策略。
- Document 未配置 AI Gateway profile、Redis、File Service、worker 时的 AI 生成或 DOCX 生成链路。
- Document MCP 已有服务内工具适配层、Streamable HTTP server 和 QA env-gated report tools smoke；仍不能承诺真实 provider 触发的 QA Agent 端到端、共享环境 Gateway/Auth/worker/download 完整验收或 #125 一键 smoke 已完成。
- Pandoc 富 DOCX 工具链运行时接入；`pandoc/core:3.10` 只是已固定选型，不代表当前导出链路已使用 Pandoc。
- Knowledge RAGFlow runtime OCR/版面模型在普通 CI 中运行。
- AI Gateway 真实 provider chat/embedding/rerank smoke 的稳定运行记录。
- 管理后台概览和跨服务指标聚合的完整运行证据；公开契约已是 active。

## 维护规则

出现下列改动时，应同步更新本文：

- Gateway active operation 新增、删除或 owner service 变化。
- 服务边界、数据归属或跨服务依赖变化。
- 当前能力矩阵中某条能力从“部分实现/缺失”变为“已实现”，或新增关键缺口。
- 新增跨服务 smoke、E2E 验收路径或部署基线。
- QA、Knowledge、Document、AI Gateway 的模型调用、MCP 工具、Knowledge runtime、runtime doc engine、File、Qdrant、MinIO 链路发生语义变化。

更新本文时不要复制完整 OpenAPI schema。路径、字段和错误码仍维护在对应 OpenAPI 与服务文档中。
