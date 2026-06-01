# 配置指南

本文档描述了如何配置 Axons。

## 配置方式

Axons 可以通过以下方式配置：

1. **命令行标志** — 最高优先级
2. **配置文件** — 最低优先级（计划中的功能）

## 配置文件

配置文件支持正在计划中。配置结构定义在 `internal/config/config.go` 中。

### 默认配置

默认情况下，Axons 使用合理的默认值：

```yaml
# 守护进程配置
daemon:
  listen: "unix://~/.axons/daemon.sock"  # Unix socket 或 TCP 地址
  pid_file: "~/.axons/daemon.pid"
  log_file: "~/.axons/daemon.log"
  log_level: "info"  # debug, info, warn, error
  clones_dir: "~/.axons/repos"  # 克隆仓库的目录

# 数据库配置
database:
  path: "~/.axons/axons.db"
  pool_size: 10

# API 配置
api:
  tcp: ""  # Web UI 的可选 TCP 地址（例如 ":8080"）
  read_timeout: 30   # 秒
  write_timeout: 0   # 秒（0 = 为 SSE 流禁用）

# 构建配置
build:
  concurrency: 4      # 并发工作数
  watch: false        # 启用文件监控

# 嵌入配置
embed:
  model: "text-embedding-3-small"
  batch_size: 100

# MCP 配置
mcp:
  enabled: true
  transport: "stdio"  # stdio, websocket

# 智能体配置
agent:
  enabled: false
  provider: "openai"  # openai, anthropic, ollama
  api_key: ""
  model: "gpt-4o"
  base_url: ""
  max_rounds: 10
  system_prompt: ""

# 终端配置
terminal:
  enabled: true
  max_sessions: 20    # 每个用户的最大终端会话数
  session_timeout: 30 # 会话超时时间（分钟）
```

## 命令行接口

### 守护进程命令

```bash
# 启动守护进程
axons daemon start

# 启动带 TCP 监听器用于 Web UI
axons daemon start --tcp :8080

# 以调试模式启动（前台运行并启用调试日志）
axons daemon start --debug

# 启动并指定自定义日志文件
axons daemon start --log /path/to/logfile

# 停止守护进程
axons daemon stop

# 检查守护进程状态
axons daemon ps
```

**守护进程标志：**
- `--tcp string` — 要监听的 TCP 地址（例如 `:8080`）用于 Web UI
- `--debug, -d` — 前台运行并启用调试日志（不分叉）
- `--log string` — 日志文件路径（调试模式下默认为 stdout）
- `--fork` — 作为分叉守护进程运行（内部使用）

### 构建命令

```bash
# 为当前目录构建图谱
axons build

# 为特定目录构建图谱
axons build /path/to/code

# 强制完全重建
axons build --full

# 带排除项构建
axons build --exclude "vendor/*" --exclude "node_modules/*"

# 带数据流分析构建
axons build --dataflow

# 带 AST 节点构建
axons build --ast

# 详细输出
axons build --verbose

# 带超时构建
axons build --timeout 15m
```

**构建标志：**
- `--full, -f` — 强制完全重建
- `--exclude, -e` — 排除模式（可多次指定）
- `--dataflow` — 包含数据流分析
- `--ast` — 包含 AST 节点
- `--verbose, -v` — 详细输出
- `--timeout duration` — 构建超时时间（默认：10m）

### 查询命令

```bash
# 按名称查询符号
axons query getUser

# 带符号类型过滤查询
axons query --kind function getUser

# 带文件过滤查询
axons query --file "internal/service" getUser

# 查找符号的调用者
axons query --callers getUser

# 查找符号的被调用者（被符号调用的函数）
axons query --callees main

# 排除测试文件
axons query --no-tests getUser

# 限制结果数量
axons query --limit 50 getUser
```

**查询标志：**
- `--kind, -k string` — 按符号类型过滤（function、method、class 等）
- `--file, -f string` — 按文件路径过滤
- `--callers` — 显示符号的调用者
- `--callees` — 显示符号的被调用者
- `--no-tests, -t` — 排除测试文件
- `--limit, -l int` — 限制结果数量（默认：20）

### 监控命令

管理增量更新的文件监控器。

```bash
# 开始监控当前目录
axons watch start

# 开始监控特定目录
axons watch start /path/to/project

# 检查监控状态
axons watch status

# 列出所有活动监控器
axons watch list

# 停止监控
axons watch stop
axons watch stop /path/to/project
```

### 嵌入命令

为代码符号构建语义嵌入。

```bash
# 使用默认设置嵌入（ollama）
axons embed

# 使用 OpenAI 嵌入
axons embed --provider openai

# 使用特定模型嵌入
axons embed --provider ollama --model nomic-embed-text

# 强制重新嵌入所有符号
axons embed --strategy full

# 自定义批处理大小
axons embed --batch 100
```

**嵌入标志：**
- `--timeout duration` — 嵌入超时时间（默认：10m）
- `--provider, -p string` — 嵌入提供者：openai、ollama（默认："ollama"）
- `--model, -m string` — 嵌入模型
- `--strategy, -s string` — 嵌入策略：incremental、full（默认："incremental"）
- `--batch int` — 嵌入 API 调用的批处理大小（默认：50）
- `--base-url string` — 嵌入 API 的自定义基础 URL
- `--api-key string` — API 密钥（或设置 OPENAI_API_KEY 环境变量）

## 配置选项

### 守护进程配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `daemon.listen` | string | `unix://~/.axons/daemon.sock` | 监听地址（unix:// 或 tcp://） |
| `daemon.pid_file` | string | `~/.axons/daemon.pid` | PID 文件路径 |
| `daemon.log_file` | string | `~/.axons/daemon.log` | 日志文件路径 |
| `daemon.log_level` | string | `info` | 日志级别（debug、info、warn、error） |
| `daemon.clones_dir` | string | `~/.axons/repos` | 克隆仓库的目录 |

### 数据库配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `database.path` | string | `~/.axons/axons.db` | 数据库文件路径 |
| `database.pool_size` | int | `10` | 连接池大小 |

### API 配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `api.tcp` | string | `\"\"` | Web UI 的 TCP 地址（例如 ":8080"） |
| `api.read_timeout` | int | `30` | 读取超时时间（秒） |
| `api.write_timeout` | int | `0` | 写入超时时间（秒）（0 = 为 SSE 流禁用） |

### 构建配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `build.concurrency` | int | `4` | 并发工作数 |
| `build.watch` | bool | `false` | 启用文件监控 |

### 嵌入配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `embed.model` | string | `text-embedding-3-small` | 嵌入模型 |
| `embed.batch_size` | int | `100` | 嵌入批处理大小 |

### MCP 配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `mcp.enabled` | bool | `true` | 启用 MCP 服务器 |
| `mcp.transport` | string | `stdio` | 传输模式（stdio、websocket） |

### 智能体配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `agent.enabled` | bool | `false` | 启用智能体服务 |
| `agent.provider` | string | `openai` | LLM 提供者（openai、anthropic、ollama） |
| `agent.api_key` | string | `\"\"` | LLM 提供者的 API 密钥 |
| `agent.model` | string | `gpt-4o` | 模型名称 |
| `agent.base_url` | string | `\"\"` | 基础 URL（用于自定义端点） |
| `agent.max_rounds` | int | `10` | 工具调用的最大轮数 |
| `agent.system_prompt` | string | `\"\"` | 自定义系统提示 |

### 终端配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `terminal.enabled` | bool | `true` | 启用终端功能 |
| `terminal.max_sessions` | int | `20` | 每个用户的最大终端会话数 |
| `terminal.session_timeout` | int | `30` | 会话超时时间（分钟） |

## 其他命令

Axons 为代码分析提供额外命令：

```bash
# 搜索命令
axons search <query>              # 搜索代码符号

# 分析命令
axons complexity [path]           # 分析代码复杂度
axons dataflow <symbol>           # 分析数据流
axons path <from> <to>            # 查找符号间路径
axons cochange <file>             # 查找协同变更文件
axons owners <symbol>             # 查找代码所有者
axons sequence <symbol>           # 分析调用序列
axons diff-impact <commit>        # 分析 Diff 影响
axons audit [path]                # 审计代码质量
axons triage <issue>              # 问题分类
axons check [path]                # 检查代码健康
axons branch-compare <branch>     # 对比分支

# 快照命令
axons snapshot create [path]      # 创建快照
axons snapshot list               # 列出快照
axons snapshot restore <id>       # 恢复快照

# 导出命令
axons export [format]             # 导出图谱数据

# 注册表命令
axons registry list               # 列出已注册项目
axons registry add <path>         # 注册项目
axons registry remove <name>      # 取消注册项目

# 统计命令
axons stats                       # 获取项目统计信息
```

## 性能调优

### 对于大型代码库

```yaml
database:
  pool_size: 20

build:
  concurrency: 8
```

构建时使用这些标志：
```bash
axons build --timeout 30m --exclude "vendor/*" --exclude "node_modules/*"
```

### 对于开发

```bash
# 以调试模式启动守护进程
axons daemon start --debug

# 带详细输出的快速构建
axons build --verbose
```

## 安全考虑

1. **Unix Socket**：默认情况下，守护进程使用 Unix socket，比 TCP 更安全
2. **TCP 监听器**：如果使用 `--tcp` 用于 Web UI，考虑在代理后绑定到 `127.0.0.1`
3. **数据库路径**：确保数据库目录具有适当的权限
4. **API 密钥**：使用环境变量安全存储 API 密钥，而非配置文件

## 数据目录

Axons 默认将所有数据存储在 `~/.axons/` 中：

```
~/.axons/
├── daemon.sock      # 守护进程通信的 Unix socket
├── daemon.pid       # 守护进程进程的 PID 文件
├── daemon.log       # 守护进程日志文件
├── axons.db         # SQLite 数据库
├── repos/           # 克隆的仓库
└── journals/        # 增量构建的文件变更日志
```