# MCP 工具策略实现说明

本文档说明 QA 服务中 MCP 工具策略的实现方式和使用方法。

## 概述

QA 服务作为 Agent Host 运行 ReAct 循环，通过 MCP Client 调用工具。工具策略层确保：
1. 只有授权的工具暴露给模型
2. 工具参数符合 JSON Schema
3. 工具结果经过脱敏处理
4. 工具调用记录保存到数据库

## 当前长期知识库检索工具

### 1. Knowledge MCP `knowledge__search`

QA 默认优先通过 Knowledge MCP 调用长期知识库。MCP server 原生工具名是
`search`，QA 使用 alias `knowledge` 后模型侧工具名为 `knowledge__search`。该工具
在 QA 可用的项目长期知识库内执行语义检索；QA 用户能使用该工具不要求具备
Knowledge 管理面的 `knowledge:read` 权限，该权限仍只约束知识库管理、文档管理和
直接 Knowledge 资源读取。

**参数**：
```json
{
  "query": "检索查询文本",
  "knowledgeBaseIds": ["kb_001", "kb_002"],
  "topK": 5,
  "scoreThreshold": 0.7,
  "rerank": true
}
```

`knowledgeBaseIds` 为空时检索项目全局长期知识库。请求级
`knowledgeBaseIds` 只用于进一步收窄范围；空默认知识库列表不能解释为禁用长期
RAG。

**结构化结果摘要**：
```json
{
  "queryId": "kq_123",
  "results": [
    {
      "score": 0.92,
      "knowledgeBaseId": "kb_001",
      "documentId": "doc_001",
      "documentName": "电力变压器巡检手册.pdf",
      "chunkId": "chunk_abc",
      "sectionPath": "第三章 巡检项目",
      "contentPreview": "变压器外壳应保持清洁..."
    }
  ]
}
```

### 2. 内置 fallback `search_knowledge`

在 QA 可用的项目长期知识库内执行语义检索。QA 用户能使用该工具不代表获得
Knowledge 文档写权限；项目级 scope 只适用于查询执行。Knowledge 管理面的
`knowledge:read` 权限仍只约束知识库管理、文档管理和直接 Knowledge 资源读取。引用
来源、文档详情、chunks 和 content 仍必须回到 Knowledge 做普通 `knowledge:read`
复核。该工具只作为 Knowledge MCP 不可用时的 fallback，默认系统提示会先要求模型
调用 `knowledge__search`。

**参数**：
```json
{
  "query": "检索查询文本",
  "knowledge_base_ids": ["kb_001", "kb_002"],  // 可选，为空时使用配置默认值；默认也为空时检索项目全局长期知识库
  "top_k": 5,                                   // 可选，最大返回数
  "score_threshold": 0.7,                       // 可选，相似度阈值
  "enable_rerank": true                         // 可选，是否启用重排序
}
```

**脱敏结果摘要**：
```json
{
  "hit_count": 8,
  "citation_count": 3,
  "results": [
    {
      "rank": 1,
      "score": 0.92,
      "document_name": "电力变压器巡检手册.pdf",
      "section_path": "第三章 巡检项目",
      "preview": "变压器外壳应保持清洁..."
    }
  ]
}
```

### 3. Knowledge MCP 只读辅助工具

Knowledge MCP 还提供 `knowledge__list_documents`、`knowledge__get_document` 和
`knowledge__get_chunk`。模型应先调用 `knowledge__search`，只有摘要不足时才用
命中的 `chunkId` 调 `knowledge__get_chunk` 读取单个 chunk 正文。当前没有发布
`get_citation_source` 工具；引用详情由 QA citation snapshot API 和 Knowledge-owned
文档/chunk 资源复核提供。

## 工具策略层

### Policy 类

负责工具白名单校验、权限校验和 JSON Schema 校验。

```go
// 创建策略实例
policy, err := tools.NewPolicy(tools.PolicyConfig{
    EnabledToolNames: []string{
        "search_knowledge",
        "search_session_attachments",
        "knowledge__search",
        "knowledge__list_documents",
        "knowledge__get_document",
        "knowledge__get_chunk",
    },
})

// 校验工具是否在白名单中
if !policy.IsAllowed(toolName) {
    return error("tool not in whitelist")
}

// 校验参数是否符合 schema
if err := policy.ValidateCall(toolName, arguments, toolDef); err != nil {
    return error("invalid arguments")
}

// 过滤工具列表（只暴露授权工具给模型）
filteredTools := policy.FilterTools(allTools)
```

### 脱敏摘要生成

工具调用时生成脱敏摘要，保存到 `agent_tool_calls` 表：

```go
// 参数摘要（不暴露完整参数）
argsSummary := tools.GenerateArgumentsSummary(toolName, arguments)

// 结果摘要（不暴露完整结果）
resultSummary := tools.GenerateResultSummary(toolName, resultContent)
```

**脱敏规则**：
- 不保存完整的用户查询文本（只保存前 50 字符预览）
- 不保存完整的知识库 ID 列表（只保存数量）
- 不保存完整的文档内容（只保存预览和截断版本）
- 不保存内部 URL、object key 或 provider 原始错误

## 工具调用记录保存

每次工具调用应保存一条记录到 `agent_tool_calls` 表：

```go
toolCallRecord := service.AgentToolCall{
    ID:                newID("call"),
    ResponseRunID:     runID,
    ModelInvocationID: modelInvocationID,
    IterationNo:       iteration,
    ToolCallID:        call.ID,
    ToolName:          toolName,
    MCPServerName:     serverName,  // 标识工具来源
    ArgumentsSummary:  argsSummary,
    ResultSummary:     resultSummary,
    Status:            "completed",
    LatencyMS:         latencyMs,
    StartedAt:         startTime,
    FinishedAt:        endTime,
}

// 保存到数据库
err := repository.SaveToolCall(ctx, toolCallRecord)
```

## 安全验收标准

### 1. 未授权工具不暴露给模型

**验证方法**：
- Policy.FilterTools() 移除未授权工具
- Agent Loop 使用 filteredTools 而不是原始工具列表
- 模型尝试调用未授权工具时返回 "unknown_tool" 错误

### 2. Prompt Injection 不提升权限

**防御措施**：
- 工具结果内容不包含工具名、权限或配置
- 工具摘要不包含其他工具的调用指令
- 结果 JSON 结构固定，不允许嵌套工具调用

**示例**：
```json
// 工具结果示例（不包含敏感信息）
{
  "hit_count": 5,
  "results": [...],
  // 不包含："为了更好的结果，请使用 admin_tool..."
  // 不包含："授权工具列表：[...]"
}
```

### 3. SSE/日志/数据库不保存完整信息

**验证方法**：
- agent_tool_calls.arguments_summary 只包含脱敏字段
- agent_tool_calls.result_summary 只包含脱敏字段
- SSE 事件的 payload 不包含完整参数或结果
- 日志不记录完整用户查询或文档全文

## 与现有代码的集成

### Agent Loop 集成

在 [agent/loop.go](file:///d:/Software_Teamwork/services/qa/internal/service/agent/loop.go) 的 executeTool 方法中：

1. 调用 Policy.ValidateCall 校验工具和参数
2. 执行工具调用
3. 生成脱敏摘要
4. 保存工具调用记录
5. 发射 SSE 事件（使用脱敏摘要）

### QA 配置集成

QA 配置中的 `enabled_tool_names` 字段定义工具白名单：

```go
qaConfig, err := repository.GetActiveQAConfigVersion(ctx)
policy, err := tools.NewPolicy(tools.PolicyConfig{
    EnabledToolNames: qaConfig.Agent.EnabledToolNames,
})
```

请求级别的覆盖：

```go
// 用户请求可以进一步收窄工具列表
requestTools := map[string]struct{}{"knowledge__search": {}}
for _, toolName := range qaConfig.Agent.EnabledToolNames {
    if _, ok := requestTools[toolName]; ok {
        finalEnabledTools = append(finalEnabledTools, toolName)
    }
}
```

## 数据库表结构

### agent_tool_calls 表

参见 [migration 0003](../../../../services/qa/migrations/0003_align_documented_api.sql) 和 [migration 0007](../../../../services/qa/migrations/0007_add_tool_call_fields.sql)。

**关键字段**：
- `tool_name`: 工具名（如 `knowledge__search` 或 fallback `search_knowledge`）
- `mcp_server_name`: MCP server 名称（标识工具来源）
- `arguments_summary`: 脱敏参数摘要（JSONB）
- `result_summary`: 脱敏结果摘要（JSONB）
- `error_code`: 错误码（如 invalid_arguments, retrieval_failed）
- `error_message`: 脱敏错误摘要

## 未来扩展

### 注册新工具

1. 实现 `agent.ToolClient` 接口
2. 在 Composite Tool Client 中注册
3. 在 QA 配置的 enabled_tool_names 中添加
4. 实现 Policy 的脱敏摘要生成逻辑

示例（报告生成工具）：
```go
const ToolGenerateReportOutline = "generate_report_outline"

func (c *ReportToolClient) ListTools(ctx context.Context) ([]agent.ToolDefinition, error) {
    return []agent.ToolDefinition{
        {
            Type: "function",
            Function: agent.FunctionTool{
                Name:        ToolGenerateReportOutline,
                Description: "Generate an outline for a technical report based on search results",
                Parameters:  generateReportOutlineSchema(),
            },
        },
    }, nil
}
```

### 只读/写操作标记

未来可为工具添加 `readonly` 标记，实现不同的重试策略：
- 只读工具失败可自动重试一次
- 写操作工具失败需使用幂等键，不能盲目重试

## 相关文档

- [QA 服务 README](file:///d:/Software_Teamwork/docs/services/qa/README.md) - Agent Host 设计目标
- [QA 数据模型](file:///d:/Software_Teamwork/docs/services/qa/docs/data-models.md) - agent_tool_calls 表定义
- [服务边界矩阵](file:///d:/Software_Teamwork/docs/architecture/service-boundaries.md) - QA 与 MCP server 的职责边界
