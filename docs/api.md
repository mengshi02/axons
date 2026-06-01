# API Documentation

This document describes the HTTP REST API provided by Axons.

## Base URL

```
http://localhost:8080
```

## Authentication

Currently, the API does not require authentication. Future versions will support API keys and OAuth.

## Endpoints

### Graph Operations

#### Build Graph

```http
POST /v1/build
```

**Request Body:**

```json
{
  "path": "/path/to/codebase",
  "full": false,
  "dataflow": false,
  "ast": false,
  "exclude": ["vendor/*", "node_modules/*"]
}
```

**Response:**

```json
{
  "task_id": "task_123",
  "status": "running"
}
```

#### Query Nodes

```http
POST /v1/query
```

**Request Body:**

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

**Response:**

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

#### Search Code

```http
POST /v1/search
```

**Request Body:**

```json
{
  "query": "function name",
  "mode": "keyword",
  "limit": 50
}
```

**Search Modes:**

| Mode | Description |
|------|-------------|
| `keyword` | FTS5 BM25 keyword-based search |
| `semantic` | Semantic vector similarity search |
| `hybrid` | Combined FTS5 + vector search with RRF fusion |

**Response:**

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

#### Semantic Search

```http
POST /v1/semantic-search
```

**Request Body:**

```json
{
  "query": "function that handles user authentication",
  "provider": "ollama",
  "model": "nomic-embed-text",
  "limit": 10,
  "threshold": 0.5
}
```

#### Get Statistics

```http
GET /v1/stats
```

**Response:**

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

#### List Files

```http
GET /v1/files
```

**Response:**

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

### Symbol Operations

#### Get Symbol by ID

```http
GET /v1/symbols/{id}
```

**Response:**

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

#### Get Callers

```http
GET /v1/symbols/{id}/callers
```

**Response:**

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

#### Get Callees

```http
GET /v1/symbols/{id}/callees
```

**Response:**

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

#### Get Symbol Impact

```http
GET /v1/symbols/{id}/impact
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `max_depth` | int | Maximum BFS depth (default: 3) |

**Response:**

```json
{
  "symbol": { "id": 123, "name": "getUser" },
  "impacted": [
    { "id": 456, "name": "handleRequest", "depth": 1 },
    { "id": 789, "name": "processRequest", "depth": 2 }
  ]
}
```

#### Get Symbol CFG

```http
GET /v1/symbols/{id}/cfg
```

**Response:**

```json
{
  "nodes": [...],
  "edges": [...]
}
```

### Embedding Operations

#### Start Embedding

```http
POST /v1/embed
```

**Request Body:**

```json
{
  "provider": "ollama",
  "model": "nomic-embed-text",
  "strategy": "incremental",
  "batch_size": 50
}
```

**Response:**

```json
{
  "task_id": "embed_123",
  "status": "running"
}
```

#### Get Embedding Status

```http
GET /v1/embed/status
```

**Response:**

```json
{
  "status": "running",
  "progress": 75,
  "provider": "ollama",
  "model": "nomic-embed-text"
}
```

#### Cancel Embedding

```http
POST /v1/embed/cancel
```

#### Test Embedding Config

```http
POST /v1/embed/test
```

**Request Body:**

```json
{
  "provider": "openai",
  "model": "text-embedding-3-small",
  "api_key": "sk-...",
  "base_url": ""
}
```

### Analysis Operations

#### Audit Code

```http
POST /v1/audit
```

#### Check Code Health

```http
POST /v1/check
```

#### Analyze Complexity

```http
POST /v1/complexity
```

#### Find Path Between Symbols

```http
POST /v1/path
```

**Request Body:**

```json
{
  "from_id": 123,
  "to_id": 456,
  "max_depth": 6
}
```

#### Analyze Call Sequence

```http
POST /v1/sequence
```

#### Export Graph

```http
POST /v1/export
```

#### Analyze Data Flow

```http
POST /v1/dataflow
```

#### Diff Impact Analysis

```http
POST /v1/diff-impact
```

#### Code Ownership

```http
POST /v1/owners
```

#### Issue Triage

```http
POST /v1/triage
```

#### Co-Change Analysis

```http
POST /v1/cochange
```

#### Branch Comparison

```http
POST /v1/branch-compare
```

#### Snapshot Operations

```http
POST /v1/snapshot/{action}
```

Actions: `create`, `list`, `restore`

#### Generate CFG

```http
POST /v1/cfg
```

### Graph Algorithm Routes

#### Graph Metrics

```http
GET /v1/graph/metrics
```

#### Community Detection

```http
GET /v1/graph/communities
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `min_size` | int | Minimum community size (default: 2) |
| `limit` | int | Maximum number of communities (default: 20) |

#### PageRank

```http
GET /v1/graph/pagerank
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | int | Maximum number of results (default: 20) |

#### Cycle Detection

```http
GET /v1/graph/cycles
```

### Analysis Routes

#### Hotspots

```http
GET /v1/analysis/hotspots
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | int | Maximum number of results (default: 20) |

#### Dead Code

```http
GET /v1/analysis/deadcode
```

#### Co-Change Query

```http
GET /v1/analysis/cochange
```

### Project Operations

#### List Projects

```http
GET /v1/projects
```

#### Create Project

```http
POST /v1/projects
```

#### Get Project

```http
GET /v1/projects/{id}
```

#### Delete Project

```http
DELETE /v1/projects/{id}
```

#### Get Project Statistics

```http
GET /v1/projects/{id}/stats
```

#### Project Watch Operations

```http
POST /v1/projects/{id}/watch/start
POST /v1/projects/{id}/watch/stop
GET  /v1/projects/{id}/watch/status
```

### Registry Operations

#### List Repositories

```http
GET /v1/repos
```

#### Register Repository

```http
POST /v1/repos
```

#### Get Repository

```http
GET /v1/repos/{name}
```

#### Unregister Repository

```http
DELETE /v1/repos/{name}
```

#### Prune Repositories

```http
POST /v1/repos/prune
```

### Watch Operations

```http
POST /v1/watch/start
POST /v1/watch/stop
GET  /v1/watch/status
GET  /v1/watch/list
POST /v1/watch/restore
```

### Task Management

#### List Tasks

```http
GET /v1/tasks
```

#### Get Task

```http
GET /v1/tasks/{id}
```

**Response:**

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

#### Cancel Task

```http
POST /v1/tasks/{id}/cancel
```

### Settings Management

#### Get Settings

```http
GET /v1/settings
```

#### Update Settings

```http
PUT /v1/settings
```

#### Check Embedding Configuration

```http
GET /v1/settings/check
```

#### Test Connection

```http
POST /v1/settings/test-connection
```

#### Get Settings by Category

```http
GET /v1/config/{category}
```

#### Set Setting

```http
PUT /v1/config/{key}
```

#### Delete Setting

```http
DELETE /v1/config/{key}
```

### CCE (Cognitive Context Engine) Routes

#### Get Code Context

```http
POST /v1/cce/context
```

**Request Body:**

```json
{
  "query": "How does authentication work?",
  "template": "general",
  "max_tokens": 4000,
  "max_results": 15,
  "min_score": 0.15
}
```

**Templates:** `understand_function`, `change_impact`, `debug_trace`, `explore_module`, `general`

#### Embed for CCE

```http
POST /v1/cce/embed
```

#### Get CCE Status

```http
GET /v1/cce/status
```

#### List CCE Templates

```http
GET /v1/cce/templates
```

### Architecture Rules Engine

#### List Architecture Rules

```http
GET /v1/arch/rules
```

#### Create Architecture Rule

```http
POST /v1/arch/rules
```

**Request Body:**

```json
{
  "name": "No UI in data layer",
  "from_pattern": "internal/data/*",
  "to_pattern": "ui/*",
  "description": "Data layer must not depend on UI"
}
```

#### Delete Architecture Rule

```http
DELETE /v1/arch/rules/{id}
```

#### Validate Architecture Rules

```http
POST /v1/arch/validate
```

### Process Execution Flow

#### List Processes

```http
GET /v1/processes
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | int | Maximum number of processes (default: 50) |

#### Get Process

```http
GET /v1/processes/{id}
```

#### Detect Processes

```http
POST /v1/processes/detect
```

### Call Chain

```http
POST /v1/callchain
```

**Request Body:**

```json
{
  "from_id": 123,
  "to_id": 456,
  "max_depth": 5
}
```

### MCP Endpoint

The MCP (Model Context Protocol) interface is exposed via HTTP:

```http
POST /mcp
```

**Protocol**: JSON-RPC 2.0 over HTTP

**Available MCP Tools:**

| Category | Tool | Description |
|----------|------|-------------|
| Search | `keyword_search` | FTS5 BM25 full-text search |
| Search | `hybrid_search` | FTS5 + vector + RRF fusion search |
| Search | `semantic_search` | Vector similarity search |
| Search | `rerank_results` | Rerank search results for improved relevance |
| Search | `search_symbols` | Search symbols by name pattern |
| Graph | `get_symbol` | Get symbol details by ID |
| Graph | `find_callers` | Find all callers of a symbol |
| Graph | `find_callees` | Find all callees of a symbol |
| Graph | `path` | Find shortest path between two symbols |
| Analysis | `list_files` | List all indexed files |
| Analysis | `get_stats` | Get code graph statistics |
| Analysis | `find_dead_code` | Find potentially dead code |
| Analysis | `find_hotspots` | Find code hotspots |
| Analysis | `find_impact` | BFS impact analysis |
| Analysis | `find_call_chain` | BFS call chain between two symbols |
| Analysis | `get_complexity` | Get complexity metrics |
| Analysis | `get_cochanges` | Co-change coupling analysis |
| Analysis | `get_pagerank` | PageRank importance ranking |
| Analysis | `arch_check` | Architecture rule validation |
| Analysis | `list_communities` | Louvain community detection |
| Analysis | `get_modules` | Top-level module overview |
| Analysis | `get_node_by_file` | Find symbols in a file |
| Analysis | `list_processes` | List materialized execution flows |
| Analysis | `get_process` | Get process step details |
| Source | `get_source_code` | Retrieve source code for symbols |
| Source | `embedding_status` | Get embedding status |
| Source | `read_file` | Read raw file content |
| Source | `smart_read` | Intelligent file reading based on size |
| Source | `write_file` | Write content to a file |
| Source | `run_command` | Execute a shell command |
| CCE | `get_context` | Retrieve and assemble code context |
| CCE | `list_context_templates` | List available context templates |

### Chat API (Agent)

#### Send Chat Message

```http
POST /api/chat
```

#### Stream Chat Response

```http
POST /api/chat/stream
```

#### Clear Chat Session

```http
POST /api/chat/clear
```

#### List Chat Sessions

```http
GET /api/chat/sessions
```

#### Get Session History

```http
GET /api/chat/sessions/{id}/history
```

### Agent Profiles API

#### List Agent Tools

```http
GET /api/agent-tools
```

#### List Agents

```http
GET /api/agents
```

#### Create Agent

```http
POST /api/agents
```

#### Get Agent

```http
GET /api/agents/{id}
```

#### Update Agent

```http
PUT /api/agents/{id}
```

#### Delete Agent

```http
DELETE /api/agents/{id}
```

### LLM Models API

#### List LLM Models

```http
GET /api/llm-models
```

#### Create LLM Model

```http
POST /api/llm-models
```

#### Update LLM Model

```http
PUT /api/llm-models/{id}
```

#### Delete LLM Model

```http
DELETE /api/llm-models/{id}
```

### File Changes API (AI Modifications)

#### List Changes

```http
GET /api/changes
```

#### Get Diff

```http
GET /api/changes/diff
```

#### Revert Changes

```http
POST /api/changes/revert
```

#### Revert All Changes

```http
POST /api/changes/revert-all
```

#### Clear Changes

```http
DELETE /api/changes
```

### Terminal API

#### Create Terminal Session

```http
POST /api/terminal/sessions
```

#### Get Terminal Session

```http
GET /api/terminal/sessions/{id}
```

#### Terminal WebSocket

```http
GET /api/terminal/sessions/{id}/ws
```

#### Kill Terminal Session

```http
DELETE /api/terminal/sessions/{id}
```

#### List Terminal Sessions

```http
GET /api/terminal/sessions
```

#### Resize Terminal

```http
POST /api/terminal/sessions/{id}/resize
```

#### Kill All Terminal Sessions

```http
DELETE /api/terminal/sessions
```

### Web UI Compatibility Routes

These routes provide compatibility for the Axons web frontend:

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

### Health & Status

#### Health Check

```http
GET /health
```

**Response:**

```json
{
  "status": "ok"
}
```

#### Readiness Check

```http
GET /ready
```

#### Daemon Status

```http
GET /api/v1/status
```

#### Shutdown Daemon

```http
POST /api/v1/shutdown
```

### Events (SSE)

#### Event Stream

```http
GET /v1/events
```

Real-time event stream using Server-Sent Events (SSE). Provides build progress, embedding updates, and other notifications.

## Error Responses

All endpoints return errors in a consistent format:

```json
{
  "error": "Node with ID '999' not found"
}
```

**Common HTTP Status Codes:**

| Code | Description |
|------|-------------|
| 400 | Invalid request parameters |
| 404 | Resource not found |
| 500 | Internal server error |

## Versioning

The API uses `/v1/` prefix for versioned endpoints. Web UI compatibility routes use `/api/` prefix without versioning.

## Examples

### curl

```bash
# Build code graph
curl -X POST "http://localhost:8080/v1/build" \
  -H "Content-Type: application/json" \
  -d '{"path": "/path/to/code"}'

# Search for a function
curl -X POST "http://localhost:8080/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query": "main", "mode": "keyword"}'

# Query symbols
curl -X POST "http://localhost:8080/v1/query" \
  -H "Content-Type: application/json" \
  -d '{"query": "getUser", "kind": "function"}'

# Get statistics
curl "http://localhost:8080/v1/stats"

# Get CCE context
curl -X POST "http://localhost:8080/v1/cce/context" \
  -H "Content-Type: application/json" \
  -d '{"query": "authentication flow", "template": "general"}'
```

### Python

```python
import requests

BASE_URL = "http://localhost:8080"

# Search code
response = requests.post(
    f"{BASE_URL}/v1/search",
    json={"query": "main", "mode": "keyword"}
)
results = response.json()

# Query symbols
response = requests.post(
    f"{BASE_URL}/v1/query",
    json={"query": "getUser", "kind": "function"}
)
symbols = response.json()

# Get CCE context
response = requests.post(
    f"{BASE_URL}/v1/cce/context",
    json={"query": "How does auth work?", "template": "general"}
)
context = response.json()
```

### JavaScript

```javascript
const BASE_URL = 'http://localhost:8080';

// Search code
const results = await fetch(`${BASE_URL}/v1/search`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ query: 'main', mode: 'keyword' })
}).then(res => res.json());

// Get CCE context
const context = await fetch(`${BASE_URL}/v1/cce/context`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ query: 'auth flow', template: 'general' })
}).then(res => res.json());
```