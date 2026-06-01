# API 文档

本文档描述了 Axons 提供的 HTTP REST API。

## 基础 URL

```
http://localhost:8080
```

## 认证

目前，API 不需要认证。未来版本将支持 API 密钥和 OAuth。

## 接口列表

### 图谱操作

#### 构建图谱

```http
POST /v1/build
```

**请求体：**

```json
{
  "path": "/path/to/codebase",
  "full": false,
  "dataflow": false,
  "ast": false,
  "exclude": ["vendor/*", "node_modules/*"]
}
```

**响应：**

```json
{
  "task_id": "task_123",
  "status": "running"
}
```

#### 查询节点

```http
POST /v1/query
```

**请求体：**

```json
{
  "query": "getUser",
  "kind": "function",
  "file": "internal/service",
  "callers": false,
  "callees": false,
  "limit": 20
}
```

**响应：**

```json
{
  "results": [
    {
      "id": 123,
      "name": "getUser",
      "kind": "function",
      "file": "internal/service/user.go",
      "line": 15,
      "end_line": 30
    }
  ]
}
```

#### 搜索代码

```http
POST /v1/search
```

**请求体：**

```json
{
  "query": "function name",
  "mode": "keyword",
  "limit": 50
}
```

**搜索模式：**

| 模式 | 描述 |
|------|------|
| `keyword` | 基于 FTS5 BM25 的关键词搜索 |
| `semantic` | 语义向量相似度搜索 |
| `hybrid` | FTS5 + 向量搜索 + RRF 融合搜索 |

**响应：**

```json
{
  "results": [
    {
      "id": 123,
      "name": "myFunction",
      "kind": "function",
      "file": "internal/handler.go",
      "line": 10,
      "score": 0.95
    }
  ]
}
```

#### 语义搜索

```http
POST /v1/semantic-search
```

**请求体：**

```json
{
  "query": "处理用户认证的函数",
  "provider": "ollama",
  "model": "nomic-embed-text",
  "limit": 10,
  "threshold": 0.5
}
```

#### 获取统计信息

```http
GET /v1/stats
```

**响应：**

```json
{
  "total_nodes": 5000,
  "total_edges": 15000,
  "total_files": 500,
  "nodes_by_kind": {
    "function": 2500,
    "method": 1000,
    "class": 800,
    "interface": 500,
    "variable": 200
  },
  "edges_by_kind": {
    "CALLS": 8000,
    "IMPORTS": 2000,
    "IMPLEMENTS": 500,
    "REFERENCES": 4500
  }
}
```

#### 列出文件

```http
GET /v1/files
```

**响应：**

```json
{
  "files": [
    {
      "path": "internal/service/user.go",
      "language": "go",
      "nodes": 15
    }
  ]
}
```

### 符号操作

#### 根据 ID 获取符号

```http
GET /v1/symbols/{id}
```

**响应：**

```json
{
  "id": 123,
  "name": "getUser",
  "kind": "function",
  "file": "internal/service/user.go",
  "line": 15,
  "end_line": 30,
  "signature": "func getUser(id int) (*User, error)"
}
```

#### 获取调用者

```http
GET /v1/symbols/{id}/callers
```

**响应：**

```json
{
  "callers": [
    {
      "id": 456,
      "name": "handleRequest",
      "kind": "function",
      "file": "internal/handler.go",
      "line": 20
    }
  ]
}
```

#### 获取被调用者

```http
GET /v1/symbols/{id}/callees
```

**响应：**

```json
{
  "callees": [
    {
      "id": 789,
      "name": "db.Query",
      "kind": "function",
      "file": "internal/db/query.go",
      "line": 50
    }
  ]
}
```

#### 获取符号影响

```http
GET /v1/symbols/{id}/impact
```

**查询参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `max_depth` | int | 最大 BFS 深度（默认：3） |

**响应：**

```json
{
  "symbol": { "id": 123, "name": "getUser" },
  "impacted": [
    { "id": 456, "name": "handleRequest", "depth": 1 },
    { "id": 789, "name": "processRequest", "depth": 2 }
  ]
}
```

#### 获取符号控制流图

```http
GET /v1/symbols/{id}/cfg
```

**响应：**

```json
{
  "nodes": [...],
  "edges": [...]
}
```

### 向量嵌入操作

#### 开始嵌入

```http
POST /v1/embed
```

**请求体：**

```json
{
  "provider": "ollama",
  "model": "nomic-embed-text",
  "strategy": "incremental",
  "batch_size": 50
}
```

**响应：**

```json
{
  "task_id": "embed_123",
  "status": "running"
}
```

#### 获取嵌入状态

```http
GET /v1/embed/status
```

**响应：**

```json
{
  "status": "running",
  "progress": 75,
  "provider": "ollama",
  "model": "nomic-embed-text"
}
```

#### 取消嵌入

```http
POST /v1/embed/cancel
```

#### 测试嵌入配置

```http
POST /v1/embed/test
```

**请求体：**

```json
{
  "provider": "openai",
  "model": "text-embedding-3-small",
  "api_key": "sk-...",
  "base_url": ""
}
```

### 分析操作

#### 代码审计

```http
POST /v1/audit
```

#### 代码健康检查

```http
POST /v1/check
```

#### 复杂度分析

```http
POST /v1/complexity
```

#### 查找符号间路径

```http
POST /v1/path
```

**请求体：**

```json
{
  "from_id": 123,
  "to_id": 456,
  "max_depth": 6
}
```

#### 分析调用序列

```http
POST /v1/sequence
```

#### 导出图谱

```http
POST /v1/export
```

#### 分析数据流

```http
POST /v1/dataflow
```

#### Diff 影响分析

```http
POST /v1/diff-impact
```

#### 代码所有权

```http
POST /v1/owners
```

#### 问题分类

```http
POST /v1/triage
```

#### 协同变更分析

```http
POST /v1/cochange
```

#### 分支对比

```http
POST /v1/branch-compare
```

#### 快照操作

```http
POST /v1/snapshot/{action}
```

操作类型：`create`、`list`、`restore`

#### 生成控制流图

```http
POST /v1/cfg
```

### 图算法路由

#### 图谱指标

```http
GET /v1/graph/metrics
```

#### 社区检测

```http
GET /v1/graph/communities
```

**查询参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `min_size` | int | 最小社区大小（默认：2） |
| `limit` | int | 最大社区数量（默认：20） |

#### PageRank

```http
GET /v1/graph/pagerank
```

**查询参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `limit` | int | 最大返回数量（默认：20） |

#### 循环依赖检测

```http
GET /v1/graph/cycles
```

### 分析路由

#### 热点分析

```http
GET /v1/analysis/hotspots
```

**查询参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `limit` | int | 最大返回数量（默认：20） |

#### 死代码检测

```http
GET /v1/analysis/deadcode
```

#### 协同变更查询

```http
GET /v1/analysis/cochange
```

### 项目操作

#### 列出项目

```http
GET /v1/projects
```

#### 创建项目

```http
POST /v1/projects
```

#### 获取项目

```http
GET /v1/projects/{id}
```

#### 删除项目

```http
DELETE /v1/projects/{id}
```

#### 获取项目统计

```http
GET /v1/projects/{id}/stats
```

#### 项目监控操作

```http
POST /v1/projects/{id}/watch/start
POST /v1/projects/{id}/watch/stop
GET  /v1/projects/{id}/watch/status
```

### 仓库注册操作

#### 列出仓库

```http
GET /v1/repos
```

#### 注册仓库

```http
POST /v1/repos
```

#### 获取仓库

```http
GET /v1/repos/{name}
```

#### 取消注册仓库

```http
DELETE /v1/repos/{name}
```

#### 清理仓库

```http
POST /v1/repos/prune
```

### 文件监控操作

```http
POST /v1/watch/start
POST /v1/watch/stop
GET  /v1/watch/status
GET  /v1/watch/list
POST /v1/watch/restore
```

### 任务管理

#### 列出任务

```http
GET /v1/tasks
```

#### 获取任务

```http
GET /v1/tasks/{id}
```

**响应：**

```json
{
  "id": "task_123",
  "type": "build",
  "status": "completed",
  "progress": 100,
  "created_at": "2026-04-15T10:00:00Z",
  "completed_at": "2026-04-15T10:01:00Z"
}
```

#### 取消任务

```http
POST /v1/tasks/{id}/cancel
```

### 设置管理

#### 获取设置

```http
GET /v1/settings
```

#### 更新设置

```http
PUT /v1/settings
```

#### 检查嵌入配置

```http
GET /v1/settings/check
```

#### 测试连接

```http
POST /v1/settings/test-connection
```

#### 按类别获取设置

```http
GET /v1/config/{category}
```

#### 设置配置项

```http
PUT /v1/config/{key}
```

#### 删除配置项

```http
DELETE /v1/config/{key}
```

### Cognitive Context Engine (CCE) 路由

#### 获取代码上下文

```http
POST /v1/cce/context
```

**请求体：**

```json
{
  "query": "认证是如何工作的？",
  "template": "general",
  "max_tokens": 4000,
  "max_results": 15,
  "min_score": 0.15
}
```

**模板：** `understand_function`、`change_impact`、`debug_trace`、`explore_module`、`general`

#### 为 Cognitive Context Engine (CCE) 生成嵌入

```http
POST /v1/cce/embed
```

#### 获取 Cognitive Context Engine (CCE) 状态

```http
GET /v1/cce/status
```

#### 列出 Cognitive Context Engine (CCE) 模板

```http
GET /v1/cce/templates
```

### 架构规则引擎

#### 列出架构规则

```http
GET /v1/arch/rules
```

#### 创建架构规则

```http
POST /v1/arch/rules
```

**请求体：**

```json
{
  "name": "数据层不得依赖 UI",
  "from_pattern": "internal/data/*",
  "to_pattern": "ui/*",
  "description": "数据层不得依赖 UI"
}
```

#### 删除架构规则

```http
DELETE /v1/arch/rules/{id}
```

#### 校验架构规则

```http
POST /v1/arch/validate
```

### 执行流进程

#### 列出进程

```http
GET /v1/processes
```

**查询参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `limit` | int | 最大进程数量（默认：50） |

#### 获取进程

```http
GET /v1/processes/{id}
```

#### 检测进程

```http
POST /v1/processes/detect
```

### 调用链

```http
POST /v1/callchain
```

**请求体：**

```json
{
  "from_id": 123,
  "to_id": 456,
  "max_depth": 5
}
```

### MCP 接口

MCP（模型上下文协议）接口通过 HTTP 暴露：

```http
POST /mcp
```

**协议**：基于 HTTP 的 JSON-RPC 2.0

**可用的 MCP 工具：**

| 类别 | 工具 | 描述 |
|------|------|------|
| 搜索 | `keyword_search` | FTS5 BM25 全文搜索 |
| 搜索 | `hybrid_search` | FTS5 + 向量 + RRF 融合搜索 |
| 搜索 | `semantic_search` | 向量相似度搜索 |
| 搜索 | `rerank_results` | 重排序搜索结果以提升相关性 |
| 搜索 | `search_symbols` | 按名称模式搜索符号 |
| 图谱 | `get_symbol` | 根据 ID 获取符号详情 |
| 图谱 | `find_callers` | 查找符号的所有调用者 |
| 图谱 | `find_callees` | 查找符号的所有被调用者 |
| 图谱 | `path` | 查找两个符号之间的最短路径 |
| 分析 | `list_files` | 列出所有已索引的文件 |
| 分析 | `get_stats` | 获取代码图谱统计信息 |
| 分析 | `find_dead_code` | 查找潜在的死代码 |
| 分析 | `find_hotspots` | 查找代码热点 |
| 分析 | `find_impact` | BFS 影响分析 |
| 分析 | `find_call_chain` | BFS 查找两个符号之间的调用链 |
| 分析 | `get_complexity` | 获取复杂度指标 |
| 分析 | `get_cochanges` | 协同变更耦合分析 |
| 分析 | `get_pagerank` | PageRank 重要性排名 |
| 分析 | `arch_check` | 架构规则校验 |
| 分析 | `list_communities` | Louvain 社区检测 |
| 分析 | `get_modules` | 顶层模块概览 |
| 分析 | `get_node_by_file` | 查找文件中的符号 |
| 分析 | `list_processes` | 列出物化执行流 |
| 分析 | `get_process` | 获取进程步骤详情 |
| 源码 | `get_source_code` | 获取符号的源代码 |
| 源码 | `embedding_status` | 获取嵌入状态 |
| 源码 | `read_file` | 读取原始文件内容 |
| 源码 | `smart_read` | 根据文件大小智能读取 |
| 源码 | `write_file` | 写入文件内容 |
| 源码 | `run_command` | 执行 Shell 命令 |
| Cognitive Context Engine (CCE) | `get_context` | 获取并组装代码上下文 |
| Cognitive Context Engine (CCE) | `list_context_templates` | 列出可用的上下文模板 |

### 对话 API（智能体）

#### 发送对话消息

```http
POST /api/chat
```

#### 流式对话响应

```http
POST /api/chat/stream
```

#### 清除对话会话

```http
POST /api/chat/clear
```

#### 列出对话会话

```http
GET /api/chat/sessions
```

#### 获取会话历史

```http
GET /api/chat/sessions/{id}/history
```

### 智能体配置 API

#### 列出智能体工具

```http
GET /api/agent-tools
```

#### 列出智能体

```http
GET /api/agents
```

#### 创建智能体

```http
POST /api/agents
```

#### 获取智能体

```http
GET /api/agents/{id}
```

#### 更新智能体

```http
PUT /api/agents/{id}
```

#### 删除智能体

```http
DELETE /api/agents/{id}
```

### LLM 模型 API

#### 列出 LLM 模型

```http
GET /api/llm-models
```

#### 创建 LLM 模型

```http
POST /api/llm-models
```

#### 更新 LLM 模型

```http
PUT /api/llm-models/{id}
```

#### 删除 LLM 模型

```http
DELETE /api/llm-models/{id}
```

### 文件变更 API（AI 修改）

#### 列出变更

```http
GET /api/changes
```

#### 获取差异

```http
GET /api/changes/diff
```

#### 回滚变更

```http
POST /api/changes/revert
```

#### 回滚所有变更

```http
POST /api/changes/revert-all
```

#### 清除变更

```http
DELETE /api/changes
```

### 终端 API

#### 创建终端会话

```http
POST /api/terminal/sessions
```

#### 获取终端会话

```http
GET /api/terminal/sessions/{id}
```

#### 终端 WebSocket

```http
GET /api/terminal/sessions/{id}/ws
```

#### 终止终端会话

```http
DELETE /api/terminal/sessions/{id}
```

#### 列出终端会话

```http
GET /api/terminal/sessions
```

#### 调整终端大小

```http
POST /api/terminal/sessions/{id}/resize
```

#### 终止所有终端会话

```http
DELETE /api/terminal/sessions
```

### Web UI 兼容路由

这些路由为 Axons Web 前端提供兼容性支持：

```http
GET  /api/repos
GET  /api/repo
GET  /api/graph
POST /api/graph/delta
GET  /api/file
POST /api/file
DELETE /api/file
POST /api/search
POST /api/impact
POST /api/index
POST /api/clone
GET  /api/nodes/{id}/neighbors
```

### 健康与状态

#### 健康检查

```http
GET /health
```

**响应：**

```json
{
  "status": "ok"
}
```

#### 就绪检查

```http
GET /ready
```

#### 守护进程状态

```http
GET /api/v1/status
```

#### 关闭守护进程

```http
POST /api/v1/shutdown
```

### 事件（SSE）

#### 事件流

```http
GET /v1/events
```

使用 Server-Sent Events (SSE) 的实时事件流。提供构建进度、嵌入更新和其他通知。

## 错误响应

所有接口以统一格式返回错误：

```json
{
  "error": "Node with ID '999' not found"
}
```

**常见 HTTP 状态码：**

| 状态码 | 描述 |
|--------|------|
| 400 | 请求参数无效 |
| 404 | 资源未找到 |
| 500 | 服务器内部错误 |

## 版本控制

API 使用 `/v1/` 前缀作为版本化接口。Web UI 兼容路由使用 `/api/` 前缀，不带版本号。

## 示例

### curl

```bash
# 构建代码图谱
curl -X POST "http://localhost:8080/v1/build" \
  -H "Content-Type: application/json" \
  -d '{"path": "/path/to/code"}'

# 搜索函数
curl -X POST "http://localhost:8080/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query": "main", "mode": "keyword"}'

# 查询符号
curl -X POST "http://localhost:8080/v1/query" \
  -H "Content-Type: application/json" \
  -d '{"query": "getUser", "kind": "function"}'

# 获取统计信息
curl "http://localhost:8080/v1/stats"

# 获取 Cognitive Context Engine (CCE) 上下文
curl -X POST "http://localhost:8080/v1/cce/context" \
  -H "Content-Type: application/json" \
  -d '{"query": "authentication flow", "template": "general"}'
```

### Python

```python
import requests

BASE_URL = "http://localhost:8080"

# 搜索代码
response = requests.post(
    f"{BASE_URL}/v1/search",
    json={"query": "main", "mode": "keyword"}
)
results = response.json()

# 查询符号
response = requests.post(
    f"{BASE_URL}/v1/query",
    json={"query": "getUser", "kind": "function"}
)
symbols = response.json()

# 获取 Cognitive Context Engine (CCE) 上下文
response = requests.post(
    f"{BASE_URL}/v1/cce/context",
    json={"query": "认证是如何工作的？", "template": "general"}
)
context = response.json()
```

### JavaScript

```javascript
const BASE_URL = 'http://localhost:8080';

// 搜索代码
const results = await fetch(`${BASE_URL}/v1/search`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ query: 'main', mode: 'keyword' })
}).then(res => res.json());

// 获取 Cognitive Context Engine (CCE) 上下文
const context = await fetch(`${BASE_URL}/v1/cce/context`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ query: 'auth flow', template: 'general' })
}).then(res => res.json());
```