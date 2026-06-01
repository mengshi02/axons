# Configuration Guide

This document describes how to configure Axons.

## Configuration Methods

Axons can be configured through:

1. **Command-line flags** - Highest priority
2. **Configuration file** - Lowest priority (planned feature)

## Configuration File

Configuration file support is planned. The configuration structure is defined in `internal/config/config.go`.

### Default Configuration

By default, Axons uses sensible defaults:

```yaml
# Daemon configuration
daemon:
  listen: "unix://~/.axons/daemon.sock"  # Unix socket or TCP address
  pid_file: "~/.axons/daemon.pid"
  log_file: "~/.axons/daemon.log"
  log_level: "info"  # debug, info, warn, error
  clones_dir: "~/.axons/repos"  # Directory for cloned repositories

# Database configuration
database:
  path: "~/.axons/axons.db"
  pool_size: 10

# API configuration
api:
  tcp: ""  # Optional TCP address for Web UI (e.g., ":8080")
  read_timeout: 30   # seconds
  write_timeout: 0   # seconds (0 = disabled for SSE streams)

# Build configuration
build:
  concurrency: 4      # Number of concurrent workers
  watch: false        # Enable file watching

# Embed configuration
embed:
  model: "text-embedding-3-small"
  batch_size: 100

# MCP configuration
mcp:
  enabled: true
  transport: "stdio"  # stdio, websocket

# Agent configuration
agent:
  enabled: false
  provider: "openai"  # openai, anthropic, ollama
  api_key: ""
  model: "gpt-4o"
  base_url: ""
  max_rounds: 10
  system_prompt: ""

# Terminal configuration
terminal:
  enabled: true
  max_sessions: 20    # Maximum terminal sessions per user
  session_timeout: 30 # Session timeout in minutes
```

## Command-Line Interface

### Daemon Commands

```bash
# Start the daemon
axons daemon start

# Start with TCP listener for Web UI
axons daemon start --tcp :8080

# Start in debug mode (foreground with debug logging)
axons daemon start --debug

# Start with custom log file
axons daemon start --log /path/to/logfile

# Stop the daemon
axons daemon stop

# Check daemon status
axons daemon ps
```

**Daemon Flags:**
- `--tcp string` - TCP address to listen on (e.g., `:8080`) for Web UI
- `--debug, -d` - Run in foreground with debug logging (don't fork)
- `--log string` - Log file path (default: stdout in debug mode)
- `--fork` - Run as forked daemon process (internal use)

### Build Commands

```bash
# Build graph for current directory
axons build

# Build graph for specific directory
axons build /path/to/code

# Force full rebuild
axons build --full

# Build with exclusions
axons build --exclude "vendor/*" --exclude "node_modules/*"

# Build with dataflow analysis
axons build --dataflow

# Build with AST nodes
axons build --ast

# Verbose output
axons build --verbose

# With timeout
axons build --timeout 15m
```

**Build Flags:**
- `--full, -f` - Force full rebuild
- `--exclude, -e` - Exclude patterns (can be specified multiple times)
- `--dataflow` - Include dataflow analysis
- `--ast` - Include AST nodes
- `--verbose, -v` - Verbose output
- `--timeout duration` - Build timeout (default: 10m)

### Query Commands

```bash
# Query symbols by name
axons query getUser

# Query with symbol kind filter
axons query --kind function getUser

# Query with file filter
axons query --file "internal/service" getUser

# Find callers of a symbol
axons query --callers getUser

# Find callees (functions called by symbol)
axons query --callees main

# Exclude test files
axons query --no-tests getUser

# Limit results
axons query --limit 50 getUser
```

**Query Flags:**
- `--kind, -k string` - Filter by symbol kind (function, method, class, etc.)
- `--file, -f string` - Filter by file path
- `--callers` - Show callers of the symbol
- `--callees` - Show callees of the symbol
- `--no-tests, -t` - Exclude test files
- `--limit, -l int` - Limit number of results (default: 20)

### Watch Commands

Manage file watchers for incremental updates.

```bash
# Start watching current directory
axons watch start

# Start watching specific directory
axons watch start /path/to/project

# Check watch status
axons watch status

# List all active watchers
axons watch list

# Stop watching
axons watch stop
axons watch stop /path/to/project
```

### Embed Commands

Build semantic embeddings for code symbols.

```bash
# Embed with default settings (ollama)
axons embed

# Embed with OpenAI
axons embed --provider openai

# Embed with specific model
axons embed --provider ollama --model nomic-embed-text

# Force re-embed all symbols
axons embed --strategy full

# Custom batch size
axons embed --batch 100
```

**Embed Flags:**
- `--timeout duration` - Embedding timeout (default: 10m)
- `--provider, -p string` - Embedding provider: openai, ollama (default: "ollama")
- `--model, -m string` - Embedding model
- `--strategy, -s string` - Embedding strategy: incremental, full (default: "incremental")
- `--batch int` - Batch size for embedding API calls (default: 50)
- `--base-url string` - Custom base URL for embedding API
- `--api-key string` - API key (or set OPENAI_API_KEY env var)

## Configuration Options

### Daemon Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `daemon.listen` | string | `unix://~/.axons/daemon.sock` | Listen address (unix:// or tcp://) |
| `daemon.pid_file` | string | `~/.axons/daemon.pid` | PID file path |
| `daemon.log_file` | string | `~/.axons/daemon.log` | Log file path |
| `daemon.log_level` | string | `info` | Log level (debug, info, warn, error) |
| `daemon.clones_dir` | string | `~/.axons/repos` | Directory for cloned repositories |

### Database Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `database.path` | string | `~/.axons/axons.db` | Database file path |
| `database.pool_size` | int | `10` | Connection pool size |

### API Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api.tcp` | string | `""` | TCP address for Web UI (e.g., `:8080`) |
| `api.read_timeout` | int | `30` | Read timeout in seconds |
| `api.write_timeout` | int | `0` | Write timeout in seconds (0 = disabled for SSE streams) |

### Build Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `build.concurrency` | int | `4` | Number of concurrent workers |
| `build.watch` | bool | `false` | Enable file watching |

### Embed Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `embed.model` | string | `text-embedding-3-small` | Embedding model |
| `embed.batch_size` | int | `100` | Batch size for embedding |

### MCP Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `mcp.enabled` | bool | `true` | Enable MCP server |
| `mcp.transport` | string | `stdio` | Transport mode (stdio, websocket) |

### Agent Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `agent.enabled` | bool | `false` | Enable agent service |
| `agent.provider` | string | `openai` | LLM provider (openai, anthropic, ollama) |
| `agent.api_key` | string | `""` | API key for the LLM provider |
| `agent.model` | string | `gpt-4o` | Model name |
| `agent.base_url` | string | `""` | Base URL (for custom endpoints) |
| `agent.max_rounds` | int | `10` | Max rounds for tool calls |
| `agent.system_prompt` | string | `""` | Custom system prompt |

### Terminal Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `terminal.enabled` | bool | `true` | Enable terminal feature |
| `terminal.max_sessions` | int | `20` | Maximum terminal sessions per user |
| `terminal.session_timeout` | int | `30` | Session timeout in minutes |

## Other Commands

Axons provides additional commands for code analysis:

```bash
# Search commands
axons search <query>              # Search code symbols

# Analysis commands
axons complexity [path]           # Analyze code complexity
axons dataflow <symbol>           # Analyze data flow
axons path <from> <to>            # Find path between symbols
axons cochange <file>             # Find co-changing files
axons owners <symbol>             # Find code owners
axons sequence <symbol>           # Analyze call sequence
axons diff-impact <commit>        # Analyze diff impact
axons audit [path]                # Audit code quality
axons triage <issue>              # Triage issues
axons check [path]                # Check code health
axons branch-compare <branch>     # Compare branches

# Snapshot commands
axons snapshot create [path]      # Create a snapshot
axons snapshot list               # List snapshots
axons snapshot restore <id>       # Restore a snapshot

# Export commands
axons export [format]             # Export code graph

# Registry commands
axons registry list               # List registered projects
axons registry add <path>         # Register a project
axons registry remove <name>      # Unregister a project

# Stats command
axons stats                       # Get project statistics
```

## Performance Tuning

### For Large Codebases

```yaml
database:
  pool_size: 20

build:
  concurrency: 8
```

Use these flags when building:
```bash
axons build --timeout 30m --exclude "vendor/*" --exclude "node_modules/*"
```

### For Development

```bash
# Start daemon in debug mode
axons daemon start --debug

# Quick builds with verbose output
axons build --verbose
```

## Security Considerations

1. **Unix Socket**: By default, the daemon uses a Unix socket which provides better security than TCP
2. **TCP Listener**: If using `--tcp` for Web UI, consider binding to `127.0.0.1` if behind a proxy
3. **Database Path**: Ensure the database directory has appropriate permissions
4. **API Keys**: Store API keys securely using environment variables, not in config files

## Data Directory

Axons stores all data in `~/.axons/` by default:

```
~/.axons/
├── daemon.sock      # Unix socket for daemon communication
├── daemon.pid       # PID file for daemon process
├── daemon.log       # Daemon log file
├── axons.db         # SQLite database
├── repos/           # Cloned repositories
└── journals/        # File change journals for incremental builds
```