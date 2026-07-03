# 知识管理模块审查报告（文档解析 & RAG）

> 任务：`.trellis/tasks/07-03-review-optimize-knowledge-rag` · 分支：`L1nggTeam/feat/ragflow-runtime-vendor`（PR #536）
> 日期：2026-07-03 · 模式：**仅审查与方案，未修改任何源码**
> 方法：4 路并行审查（worker / 解析 / 检索 / adapter）+ orchestrator 对抗验证。范围限定"实际执行路径"（见 `research/reachability.md`）。

---

## 执行摘要

| 严重度 | 数量 | 说明 |
|---|---|---|
| **P0** | 1 | RAPTOR/GraphRAG/mindmap 任务 100% 静默失败并锁死 KB 索引槽（已验证，vendoring 回归） |
| **P1** | 12（去重后） | 卡死文档、静默丢数据、错误契约失真、结果数错误等 |
| **P2** | ~30 | 潜伏缺陷、边界问题、观察项（详见各 findings 文件） |

验证修正 2 项：1 项溯源纠错（book.py 是上游继承而非项目回归）、1 项降级（多租户 break 在本部署不可达 → P2 潜伏）。

审查明确为**干净**的关键面：租户隔离（worker 全部 doc-engine 调用 + API 每路由 `accessible` 前置）、auth 链（timing-safe 比较、fail-closed、provisioning 幂等）、chunk-id 幂等性、metadata 过滤器三方语义一致、PR #440/#536 的既有加固完整有效。

---

## P0（必须修）

### P0-1 · 数据集级任务（raptor/graphrag/mindmap）必然被丢弃，且一次尝试永久锁死该 KB 的索引触发

- **根因**：vendoring 删除 canvas 代码时误删了 `get_task` 的 join 键替换逻辑。`api/db/services/task_service.py:82` `get_task(task_id, doc_ids=[])` 接受 `doc_ids` 但从未使用；join 恒为 `Task.doc_id == Document.id`。而 `queue_raptor_o_graphrag_tasks`（`document_service.py:1061`）写入的 Task 行 `doc_id="graph_raptor_x"`（哨兵，无对应 Document 行）。worker `collect()`（`task_executor.py:230`）传入 `msg["doc_ids"]` 期待替换 → 实际 join 落空 → 返回 `None` → 消息被 ack 丢弃。Task 行停在 `progress=0`，`dataset_api_service.py:487-493` 据此永久拒绝重试。
- **验证状态**：orchestrator 亲自核验证据链全环节 ✅（见 `research/verification-log.md`）
- **修复草图**（2 行，`task_service.py:96`）：
  ```python
  doc_id = cls.model.doc_id
  if doc_ids:                      # dataset-scope task: join via first real source doc
      doc_id = doc_ids[0]
  ```
- **验证计划**：单测——构造哨兵 doc_id 的 Task + 真实 doc_ids，断言 `get_task` 返回非 None 且 kb/tenant 字段来自 doc_ids[0]；集成——`POST /datasets/{id}/index?type=raptor` 后任务被 worker 领取。
- **风险**：极低。恢复上游语义；`doc_ids` 仅在哨兵分支传入。注意与 `test_runtime_guardrails.py` 现有哨兵测试的兼容。

---

## P1（应修，按修复批次分组）

### 批次 A：入队/状态机（runtime Python）

**P1-A1 · `queue_tasks` 生成 0 任务 → 文档永久 RUNNING**（R1+R2 双独立发现 ✅）
- 根因：损坏/加密 PDF `total_page_number` 返回 None→0 页→`range(0,0,…)` 空；`begin2parse` 仍置 RUNNING；`_sync_progress` 跳过无任务文档（`task_service.py:400-419/:466-473`、`document_service.py:922-924`）。变体：`table` 遇非 xls/csv/txt 文件名 `row_number` 返回 None → `range(0,None)` TypeError（先 RUNNING 后 500）。
- 修复：`parse_task_array` 为空（或构建抛异常）时置 doc FAIL + 明确 progress_msg 后返回。
- 验证：单测空任务数组分支；集成传损坏 PDF 断言 run=FAIL 而非 RUNNING。
- 风险：低；注意与 digest 重用路径（`ck_num`）的交互。

**P1-A2 · 布局引擎/OCR 故障被 "No chunk built"+prog=1.0 掩盖 → 全部 PDF"成功"解析出 0 chunk**（R2）
- 根因：错误分支写 `-1` 后续又写 `1.0`，进度 ratchet 接受 `prog>=1` 覆盖（与 R1 P2"超大文件 DONE"同机制交叉印证）。`by_paddleocr` 分支在 worker 上下文 `callback(-1)` 为死代码。
- 修复：错误分支 `return`/`raise` 后不得再触发成功回调；或 ratchet 规则拒绝从 -1 恢复为 1.0（除非显式重试）。
- 验证：模拟 OCR 后端 down，断言 doc FAIL 且 0-chunk 不落 DONE。
- 风险：中——进度 ratchet 是共享机制，改动需全链路回归（建议优先改错误分支的控制流而非 ratchet 规则）。

### 批次 B：解析数据完整性（vendor 最小 diff）

**P1-B1 · book 分块器 PDF 分支丢弃全部表格/图片**（R2，✅ 溯源修正：上游继承，非项目回归）
- 根因：`rag/app/book.py:107` 结果进 `tables`，`book.py:176` 却 tokenize 恒空的 `tbls`。
- 修复：1 行——PDF 分支后 `tbls = tables`（或合并）。上游同病，可顺手回报 upstream。
- 验证：book 分块器解析带表格 PDF，断言输出含 table chunk。
- 风险：极低。

**P1-B2 · `VisionFigureParser` 把 descriptions 从 list 变异为 str → Excel 图片描述被逐字符换行**（R2，数据损坏）
- 修复：消除类型变异（保持 list 直到最终 join），或 `table.py` join 前类型判断。
- 验证：含图 Excel 解析，断言描述文本完整。

**P1-B3 · markdown 视觉增强对 <11px 图片/视觉模型异常必然 TypeError → 整个任务失败**（R2；其他 wrapper 均有 try/except，唯此路径裸奔）
- 修复：对齐其他 wrapper 的 try/except 降级模式（跳过增强，保留原文）。
- 验证：构造含微型图片的 markdown，断言任务成功、跳过增强。

### 批次 C：检索错误契约（runtime ↔ adapter）

**P1-C1 · 空索引/未解析数据集检索 → 502 而非空结果**（R3）
- 根因：`search_datasets` 无 `index_not_found` 捕获（`dataset_api_service.py:1352-1366`），异常经全局 handler 变 HTTP200 code=100 → adapter 502。违反 api-contracts.md"零命中→空结果"。
- 修复：捕获 index_not_found → 返回 `{"total":0,"chunks":[],...}`（`retrieval_test` 已有同类守卫可对齐）。
- 验证：Go 侧 contract test + runtime 单测：查询未解析 KB 断言 200 空结果。

**P1-C2 · 业务校验失败统一坍缩为 code=102 → adapter 全部映射 502**（R3；PR #536 稳定错误码目标被架空）
- 根因：`dataset_api.py:497-504` 把所有 `(False,msg)` 变 HTTP200 code=102；adapter 只按 HTTP 401/403/404 分类。
- 修复：runtime 侧按类型返回不同 RetCode/HTTP（校验→400 类、越权→403 类、容量→独立码）；adapter `mapVendorError` 增加 body-code 分类。属**契约变更**，需同 commit 更新 OpenAPI 三方。
- 验证：contract_test.go 逐错误类断言状态码；runtime 路由测试。
- 风险：中——错误码语义变化影响前端提示逻辑，建议列出映射表评审后实施。

**（SDK-only，降优先）** `retrieval_test` 错误吞噬、raw `/retrieval` 的 top_k/threshold 无上界（R3）——不在 adapter 路径，修复时顺手对齐即可。

### 批次 D：Go adapter 契约

**P1-D1 · `topK` 未映射为 runtime `size` → 结果数被静默钉在 30**（R4）
- 根因：`buildRetrievalBody`（`map.go:594-632`）只发 `top_k`（候选池），仅 rerank+rerankTopN 时才发 `size`；runtime `size` 默认 30（`dataset_api_service.py:1265`）。topK=5 返回最多 30 条、topK=100 被截到 30；Trace.SearchTopK 还谎报。
- 修复：恒设 `payload["size"] = topK`（保持 `top_k >= size` 与 rerank clamp）。
- 验证：map_test.go 断言 body 含 size；集成断言返回条数==topK。
- 风险：低。行为变化对前端可见（终于拿到正确条数），改动前确认前端分页假设。

**P1-D2 · `DownloadDocument` 把 runtime JSON 错误信封当文件内容返回**（R4）
- 根因：runtime 下载错误是 HTTP200+JSON（`document_api.py:1916-1975`），`client.go:330-353` 只拒 status>=400 → 客户端下载到一个 JSON 错误体。
- 修复：Content-Type 为 application/json 时尝试信封解码，code!=0 → APIError。
- 验证：client_test.go 模拟 200+错误信封，断言映射为错误。

**P1-D3 · PATCH 文档 tags 后响应缺失刚写入的 tags**（R4）
- 根因：runtime update 后重读的 peewee Document 模型无 meta_fields 列 → 响应无 tags；下次 GET 才可见。
- 修复（adapter 侧最小）：Update 成功后 re-fetch `GetDatasetDocument` 再映射；或将请求 tags 合并进响应。
- 验证：contract_test.go PATCH 后断言响应含 tags。

**P1-D4 · knowledge-statistics 无界 N+1 扇出**（R4；正确性/性能边界项）
- 修复：直接聚合 dataset 列表响应中已有的 `doc_num`（`map.go:216` 已在读），消除逐 KB `ListDocuments`；或短 TTL 缓存。
- 验证：handlers 测试断言 vendor 调用次数 = 分页次数。

**P1-D5 · JSON 端点缺请求体大小上限**（R4；纵深防御）
- 修复：`decodeJSONBody` 加 `http.MaxBytesReader`（1 MiB）+ `MaxBytesError`→validation error。

---

## 验证修正记录（对抗验证产出）

| 原始发现 | 修正 |
|---|---|
| R2："book.py 丢表格是项目自引入回归" | **溯源纠错**：上游 `45fc7fea` 同文件逐行相同 → 上游继承缺陷（bug 本身成立，见 P1-B1） |
| R3 P1："多租户 break 丢其他租户索引" | **降级 P2（潜伏）**：本部署租户关系严格 1:1（仅 gateway provisioning 创建，无邀请/加入路由），循环单次迭代不可触发。接入团队功能时自动升 P1，激活条件已记录 |

## P2 清单（摘要，共 ~30 项）

详见各 findings 文件；重点提示：

- **worker**（10 项，`findings-worker.md`）：`@timeout` 装饰器因 `ENABLE_TIMEOUT_ASSERTION` 未设全部为 no-op（挂起任务占死并发槽，建议部署加 env）；单任务 cancel 撕裂多任务文档；`finally` 中 ack 可被日志异常跳过；TOC 覆写 `Task.chunk_ids` 等。
- **解析**（14 项，`findings-parsing.md`）：txt 路径遗留 `print(sections)` 全文泄漏到日志（建议随批次 B 顺手删）；PlainParser 页级异常静默截断；qa CSV 行号错位；JSON 语法错误静默 0 chunk 等。
- **检索**（8 项，`findings-retrieval.md`）：`list_chunks` page<1 无下界；多租户 break（潜伏）；其余为验证干净项记录。
- **adapter**（7 项，`findings-adapter.md`）：`documentChunkFromVendor` metadata 死代码（TokenCount/CreatedAt 恒零值——若未来接线，排除表需改白名单）；`mapRetrievalChunk` UTF-8 字节截断致中文预览尾部"�"（中文产品几乎必现，1 行修）+ chunkIndex 恒伪造 0；panic 二次写头；统计端点缺头返回 200 等。

## 建议实施顺序（供后续修复任务参考）

1. **P0-1**（2 行）+ P1-A1（同文件邻近）→ 一个 commit，runtime 单测
2. P1-D1/D2/D3（adapter 契约，Go 测试齐全）→ 每项独立 commit
3. P1-B1/B2/B3（vendor 最小 diff）→ 一个 commit
4. P1-C1（低风险守卫）先行；P1-C2（契约变更）单独评审映射表后做，需三方 OpenAPI 同步
5. P1-A2（进度 ratchet 交互，风险最高）最后做，回归面最大
6. P2 择机：`print(sections)` 删除、UTF-8 截断、`ENABLE_TIMEOUT_ASSERTION` 部署配置三项性价比最高

## 覆盖率与未尽事项（诚实声明）

- `vendorclient/client.go` 未全文深审（错误映射主题已从 runtime 侧覆盖 + download 路径已审）；`handlers_parser.go`、`internal/mcp/*` 未审。
- R2 的 P1-B2/B3、A2 的部分细节由单一 agent 报告 + 机制交叉印证，未做全部逐行复核（B1 已亲验）。实施修复时按"修前先写复现测试"原则兜底。
- 审查中两个 agent 因网关 524 超时损失过一轮工作；R2/R4 结果来自重试 + 增量落盘，覆盖完整性如上所述。
