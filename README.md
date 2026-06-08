# Axons

<p align="center">
  <img src="ui/public/favicon.svg" width="128" height="128" alt="axons Logo">
</p>

<p align="center">
  <strong>The Ultra-Lightweight AI-First Code Workbench</strong>
</p>

<p align="center">
  Lightweight by Design · Extensible by Nature · Instant Startup · Native AI Intelligence · Private Deployment
</p>

<p align="center">
  <a href="https://github.com/mengshi02/axons/actions/workflows/ci.yml">
    <img src="https://github.com/mengshi02/axons/actions/workflows/ci.yml/badge.svg" alt="CI">
  </a>
  <a href="https://github.com/mengshi02/axons/releases">
    <img src="https://img.shields.io/github/v/release/mengshi02/axons?include_prereleases" alt="Release">
  </a>
  <a href="https://github.com/mengshi02/axons/blob/main/LICENSE">
    <img src="https://img.shields.io/github/license/mengshi02/axons" alt="License">
  </a>
  <a href="https://golang.org">
    <img src="https://img.shields.io/github/go-mod/go-version/mengshi02/axons" alt="Go Version">
  </a>
  <a href="https://github.com/mengshi02/axons/issues">
    <img src="https://img.shields.io/github/issues/mengshi02/axons" alt="Issues">
  </a>
</p>

---

Axons is an ultra-lightweight AI-First code workbench — self-built four-dimensional intelligent engine core, AI capabilities natively embedded not bolted on. 5+ AI expert agents ready out-of-the-box, covering architecture governance, tech debt cleanup, and legacy system iteration. Your code never leaves your machine, fully private end-to-end.

Website: [axons.chat](https://www.axons.chat)

## Core Capabilities

### Ultra-Lightweight & Efficient

Ditch bloated traditional IDE features. Instant startup, minimal memory footprint, zero lag. Even low-spec devices run smoothly through the entire development cycle.

### Self-Built 4D Intelligent Engine

Graph Computing Engine (GCE), Analysis Engine (ACE), Cognitive Context Engine (CCE), and LLM — four engines deeply fused into a native AI code intelligence foundation, delivering full-dimension code perception and precise context understanding.

### Native Expert-Level AI Coding

5+ vertical-domain AI expert agents built-in, ready out-of-the-box with zero configuration. Covering code writing, bug fixing, architecture optimization, tech debt cleanup, and documentation generation. AI capabilities natively empowered, not simple plugin adaptations.

- **AI Orchestrator** — intelligent task decomposition and delegation, multi-expert coordination
- **Architect** — identifies module boundaries, analyzes dependencies, checks architecture compliance
- **Quality Analyst** — detects code smells, dead code, hotspot functions, excessive coupling
- **Impact Analyst** — evaluates change impact scope and blast radius, aids Code Review decisions
- **Code Engineer** — read/write files, execute commands, end-to-end coding tasks, integrated native terminal
- **Custom Agent** — create domain-specific agents with one click, persistent memory

- AI Coding Safety: precise scope modification based on graph dependencies, rejects large-scale brute rewrites
- Operation rollback: every AI edit is atomically recorded, supporting line-level precise rollback

### Open Extension Ecosystem

Plugin system covering development languages, frameworks, tools, MCP protocol, custom Skills, and enterprise components. Highly customizable for individuals and teams. Assemble on demand, scale flexibly — zero resource usage when not enabled.

- Full plugin lifecycle: install, activate, deactivate, uninstall with manifest protocol
- Frontend plugin SDK with iframe isolation, component library, and theme support
- Backend plugin support with health check, environment variables, and platform overrides
- Lazy loading via activation events — plugins only load when needed
- Official extension repository: [axons-extension-packages](https://github.com/mengshi02/axons-extension-packages)

### Full-Scenario Remote Development

Native compatibility with Docker, WSL, and SSH remote development. Remote environment feels identical to local. The local side stays ultra-lightweight, perfectly adapting to cloud-native, distributed, and remote collaboration scenarios.

### Open Source Without Limits

MIT license, fully open source, no paywalls, no feature cuts, no commercial restrictions. Supports secondary development, custom modifications, and internal enterprise deployment.

### Incremental Graph Dynamic Update — Core Performance Edge

Only detects changed files, functions, and dependencies — recomputes only the delta, never the entire project graph.

- ⚡ Sub-second architecture graph refresh, million-line projects update seamlessly
- 💾 Background resident memory footprint ultra-low, virtually imperceptible
- 🔄 Intelligent cascade detection: Journal → mtime → content hash
- 🗄️ Multi-project isolated storage, cache auto-reuse

## Supported Languages

Go, C, C++, Java, Python, Rust, C#, JavaScript, TypeScript

## Installation

### Download

Download the latest release from [GitHub Releases](https://github.com/mengshi02/axons/releases):

- macOS: Apple Silicon / Intel
- Windows: x86_64 / ARM64
- Linux: x86_64 / ARM64 (AppImage / DEB / RPM)
- Web: cross-platform browser access

### Build from Source

```bash
git clone https://github.com/mengshi02/axons.git
cd axons
make build
```

## Quick Start

```bash
# Start the daemon
./axons daemon start --tcp :8080

# Access the Web UI
open http://localhost:8080

# Build code graph
./axons build /path/to/your/code

# Search code
./axons search "functionName"
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `build` | Build code graph (with incremental updates) |
| `query` | Query nodes, edges, and relationships |
| `search` | Multi-mode search (keyword/semantic/regex) |
| `audit` | Code quality audit and cycle detection |
| `complexity` | Code complexity analysis |
| `owners` | Map code ownership from git history |
| `path` | Find paths between symbols |
| `sequence` | Generate call sequence diagrams |
| `cfg` | Generate control flow graphs |
| `dataflow` | Data flow analysis |
| `cochange` | Identify co-changing files |
| `diff-impact` | Analyze impact of git diffs |
| `branch-compare` | Compare branches for differences |
| `snapshot` | Create and manage code snapshots |
| `watch` | Real-time file monitoring and graph updates |
| `embed` | Generate embeddings for semantic search |
| `export` | Export graph data |
| `registry` | Manage multiple projects |
| `stats` | Project statistics |
| `triage` | Triage issues and locate affected code |

## Agent Skills

Three built-in agent skills following the [Agent Skills Specification](https://agentskills.io/specification):

| Skill | Description |
|-------|-------------|
| code-graph-analyzer | Code architecture and quality analysis |
| dependency-tracker | Dependency tracing and circular dependency detection |
| code-search-assistant | Intelligent code search (multi-mode support) |

See [skills/README.md](skills/README.md) for details.

## MCP Integration

Axons provides an MCP (Model Context Protocol) server exposing 30+ tools for AI assistants, including:

- Keyword search, semantic search, hybrid search
- Symbol lookup, call chain analysis, path finding
- Impact analysis, complexity metrics, PageRank
- Architecture compliance checking, community detection
- File system operations and command execution
- Cognitive context engine (bimodal embedding + scenario templates)
- Agent delegation (multi-agent collaboration)

## LLM Support

- OpenAI (GPT-4, GPT-3.5, etc.)
- Anthropic (Claude)
- Custom LLM endpoints

## Embedding Providers

- OpenAI (text-embedding-3-small/large)
- Jina AI
- Custom embedding endpoints

## Development

### Prerequisites

- Go 1.25+
- Node.js 22+

### Build

```bash
make deps          # Install dependencies
make build         # Build binary
make test          # Run tests
make lint          # Run linter
```

### Development Mode

```bash
make frontend-dev  # Start frontend dev server
make daemon        # Start backend in another terminal
```

### Desktop App

The project provides a desktop client built with Electron:

```bash
make desktop-dev           # Development mode
make desktop-build         # Build desktop app
make desktop-build-mac     # Build for macOS
make desktop-build-windows # Build for Windows
```

## Documentation

- [Architecture](docs/architecture.md)
- [API Reference](docs/api.md)
- [Configuration](docs/configuration.md)
- [Deployment](docs/deployment.md)
- [Plugin Developer Guide](docs/plugin-developer-guide.md)
- [Plugin System Design](docs/plugin-system-design.md)

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)

## License

MIT License. See [LICENSE](LICENSE) for details.

## Contact

- Website: [axons.chat](https://www.axons.chat)
- Email: [contact@axons.chat](mailto:contact@axons.chat)
- Issues: [GitHub Issues](https://github.com/mengshi02/axons/issues)