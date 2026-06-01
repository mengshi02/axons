# Axons Skills

This directory contains three Agent Skills following the [Agent Skills Specification](https://agentskills.io/specification) for automated code analysis.

## 📦 Included Skills

### 1. code-graph-analyzer
Automatically analyze codebases for architecture, dependencies, and code quality.

**Use Cases**:
- Understanding the overall architecture of a codebase
- Analyzing code quality and identifying hotspots
- Preparing for refactoring or major changes
- Onboarding new team members

### 2. dependency-tracker
Analyze and visualize code dependencies, find circular dependencies, map module relationships.

**Use Cases**:
- Analyzing code coupling and dependency relationships
- Identifying circular dependencies
- Planning refactoring or architectural changes
- Understanding the impact scope of code changes

### 3. code-search-assistant
Intelligent code search with support for keyword, semantic, regex, and embedding modes.

**Use Cases**:
- Searching for specific code patterns or functions
- Understanding code relationships and call chains
- Discovering similar implementations
- Analyzing code usage patterns

## 🚀 Usage

These skills follow the Agent Skills specification and can be used with any compatible AI agent or assistant.

### Skill Structure

Each skill contains:
- **SKILL.md**: Metadata and instructions (required)
- **Prerequisites**: How to build and configure axons CLI
- **Workflow**: Step-by-step usage instructions
- **Examples**: Practical usage scenarios

### Supported Tools

All skills use the following pre-approved tools:
- `Bash(axons:*)` - Execute axons CLI commands
- `Bash(jq:*)` - Process JSON output
- `Read` - Read files
- `Write` - Write files

## 📋 Prerequisites

All skills require the axons CLI tool:

```bash
# Clone and build
git clone https://github.com/mengshi02/axons.git
cd axons
make build

# Verify installation
axons version
```

For semantic search features, configure embedding provider:
```bash
axons config set embedding.provider ollama
axons config set embedding.model nomic-embed-text
```

## 📚 Specification

These skills comply with the [Agent Skills Specification v1.0](https://agentskills.io/specification):

- **name**: 1-64 characters, lowercase letters, numbers, and hyphens
- **description**: Max 1024 characters, describes what and when to use
- **license**: MIT
- **metadata**: Author and version information
- **allowed-tools**: Pre-approved tool list

## 🔗 Learn More

- [Axons Website](https://www.axons.chat)
- [Agent Skills Specification](https://agentskills.io/specification)
- [Agent Skills Documentation](https://agentskills.io/)
- [Axons Documentation](../docs/)

## 📄 License

MIT License