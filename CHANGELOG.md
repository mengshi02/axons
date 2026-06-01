# Changelog

All notable changes to this project will be documented in this file.

## v1.0.0 (2026-05-25)

### Highlights

**Code Graph Engine**: Tree-sitter based multi-language code parsing with automatic relationship graph building for functions, classes, interfaces, and dependencies.

**AI-Powered Analysis**: Deep code understanding with MCP (Model Context Protocol) server support, providing tools for semantic search, dependency tracking, and impact analysis for AI assistants.

**Comprehensive Tooling**: 20+ CLI commands for building, querying, auditing, and analyzing codebases with incremental indexing for efficient large-scale code handling.

**Multi-Language Support**: First-class support for Go, TypeScript, JavaScript, Python, Java, Rust, C/C++, and C# with extensible parser architecture.

**Visual Interface**: React 19 + Sigma.js web UI with interactive graph visualization, code browsing, and AI assistant integration.

**Advanced Analytics**: Code complexity metrics, circular dependency detection, co-change analysis, data flow tracking, and control flow graph generation.

### Features

**Core Analysis Capabilities**:
- Code graph building with automatic node and edge extraction
- Incremental indexing for efficient updates on large codebases
- Multiple search modes: keyword, semantic (embedding-based), regex, and hybrid
- Path finding and call chain analysis between symbols
- Impact analysis for change propagation understanding

**Code Quality & Metrics**:
- Circular dependency detection and reporting
- Code complexity analysis with cyclomatic complexity metrics
- Dead code identification
- Code ownership mapping from git history
- Co-change analysis to identify files that frequently change together

**Advanced Analysis Tools**:
- Control Flow Graph (CFG) generation for functions
- Data flow analysis to track variable usage
- Branch comparison for understanding changes across git branches
- Snapshot creation for temporal code analysis
- Diff impact analysis for pull request review
- Process detection and tracing for execution flows
- Community detection using Louvain algorithm
- PageRank for code importance ranking
- Architecture rule checking with deny rules

**MCP Server Integration**:
- 30+ tools for AI assistants via Model Context Protocol
- Keyword search, semantic search, and hybrid search capabilities
- Symbol lookup, callers/callees analysis, and path finding
- Impact analysis and call chain discovery
- Complexity metrics, co-change information, and PageRank
- Architecture compliance checking and community detection
- Process tracing for execution flow understanding
- Cognitive context engine (bimodal embedding + scenario templates)
- File system tools: read_file, write_file, run_command
- Agent delegation for multi-agent collaboration

**Plugin System**:
- Full plugin lifecycle management: install, activate, deactivate, uninstall
- Plugin manifest protocol with permissions, activation events, and platform overrides
- Frontend plugin SDK with iframe isolation, component library, and theme support
- Backend plugin support with health check, environment variables, and CORS handling
- Plugin data directory isolation per plugin with configurable uninstall modes
- Shared state and event bus for inter-plugin communication
- Lazy loading via activation events — zero resource usage when not enabled
- Official extension repository: [axons-extension-packages](https://github.com/mengshi02/axons-extension-packages)

**Web Interface**:
- Interactive graph visualization using Sigma.js
- Code browser with syntax highlighting (Prism.js)
- Project management dashboard
- AI-powered chat interface
- Settings panel for embedding providers and LLM configuration
- Real-time build progress and event streaming

**CLI Tools**:
- `build` - Build code graph from source with incremental updates
- `query` - Query nodes, edges, and relationships
- `search` - Multi-mode search (keyword/semantic/regex)
- `audit` - Comprehensive code quality audit with cycle detection
- `complexity` - Analyze code complexity metrics
- `owners` - Map code ownership from git history
- `path` - Find paths between symbols
- `sequence` - Generate call sequence diagrams
- `cfg` - Generate control flow graphs
- `dataflow` - Analyze data flow relationships
- `cochange` - Identify co-changing files
- `diff-impact` - Analyze impact of git diffs
- `branch-compare` - Compare branches for differences
- `snapshot` - Create and manage code snapshots
- `triage` - Triage issues and identify affected code
- `embed` - Generate embeddings for semantic search
- `export` - Export graph data in various formats
- `watch` - Real-time file monitoring and graph updates
- `registry` - Manage multiple projects
- `stats` - Get comprehensive project statistics

**Storage & Performance**:
- SQLite-based persistent storage with project isolation
- Global database for projects, settings, and agent profiles
- Per-project databases for nodes, edges, files, and embeddings
- Embedding cache for reuse until code changes
- Lazy loading of project databases on demand

**Embedding Providers**:
- OpenAI (text-embedding-3-small/large)
- Ollama (nomic-embed-text, etc.)
- Jina AI embeddings
- Custom embedding endpoints
- Multi-provider support with configurable defaults

**AI Agent System**:
- **ReAct Agent**: Reasoning and Acting pattern with iterative tool execution
- **Multi-Agent Orchestration**: Master orchestrator delegates tasks to specialized sub-agents
- **5 Built-in Agent Profiles**:
  - **Orchestrator** (default): Task decomposition and agent delegation
  - **Architect**: Module boundaries, dependency analysis, architecture compliance
  - **Code Quality Analyst**: Complexity, dead code, hotspots, coupling detection
  - **Impact Analyst**: Change impact scope, call chains, blast radius assessment
  - **Code Engineer**: Read/write files, execute commands, complete coding tasks
- **30+ Agent Tools**: Search, graph analysis, architecture, quality metrics, file operations, command execution
- **Agent Delegation**: Orchestrator can delegate subtasks to specialized agents via `delegate_to_agent` tool
- **Conversation Memory**: SQLite-based session management with context persistence
- **Streaming Events**: Real-time thinking, tool execution, and token streaming

**LLM Integration**:
- OpenAI (GPT-4, GPT-3.5, etc.)
- Anthropic (Claude)
- Ollama (local models)
- Custom LLM endpoints
- Tool calling support for function execution
- Multi-round conversation with context management

### Technical Details

**Architecture**:
- CLI and Web UI share core business logic (internal/core)
- HTTP API server for web interface with REST endpoints
- WebSocket/SSE for real-time event streaming
- MCP server for AI assistant integration
- ReAct agent with reasoning and tool execution

**Requirements**:
- Go 1.25+ for backend services
- Node.js 22+ for frontend development
- Tree-sitter for code parsing
- SQLite for storage

**Build & Deploy**:
- Cross-platform compilation (Linux, macOS, Windows)
- Docker containerization with embedded frontend
- Systemd service support
- Makefile-based build automation

**Dependencies**:
- github.com/modelcontextprotocol/go-sdk for MCP
- gonum.org/v1/gonum for graph algorithms
- modernc.org/sqlite for pure Go SQLite
- github.com/spf13/cobra for CLI
- github.com/odvcencio/gotreesitter for Tree-sitter bindings

### Agent Skills

Three skills following the [Agent Skills Specification](https://agentskills.io/specification):

| Skill | Description |
|-------|-------------|
| **code-graph-analyzer** | Architecture analysis, code quality metrics, and comprehensive audit reports |
| **dependency-tracker** | Dependency mapping, circular dependency detection, and impact analysis |
| **code-search-assistant** | Multi-mode search (keyword, semantic, regex, embedding) with context-aware results |

All skills:
- Follow the Agent Skills standard specification
- Include YAML frontmatter with name, description, license, and metadata
- Provide CLI-based workflows with axons commands
- Can be used with any Agent Skills compatible AI assistant

## Breaking Changes

Initial release - no breaking changes from previous versions.

### Maintenance

**Language Support**:
- Go, TypeScript, JavaScript (production-ready)
- Python, Java, Rust, C/C++, C# (supported)
- Extensible architecture for adding new languages

**Testing**:
- Unit tests for extractors (Go, TypeScript, Python, Java, Rust)
- Integration tests for graph operations
- Test data fixtures for parser validation