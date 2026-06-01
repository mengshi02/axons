---
name: code-search-assistant
description: Intelligent code search with multi-mode support including keyword, semantic, regex, and embedding-based search. Use when users need to find specific code patterns, understand code relationships, or discover similar implementations across the codebase. Provides advanced search capabilities with context-aware results.
license: MIT
metadata:
  author: axons
  version: "1.0"
allowed-tools: Bash(axons:*) Bash(jq:*) Read Write
---

# Code Search Assistant

This skill provides intelligent code search capabilities using the axons CLI tool with multiple search modes to find code patterns, understand relationships, and discover similar implementations.

## When to Use

- When searching for specific code patterns or functions
- When understanding code relationships and call chains
- When discovering similar implementations across the codebase
- When analyzing code usage and dependencies
- When preparing for refactoring or code reviews

## Prerequisites

The axons CLI must be available and built. If not, build it first:
```bash
cd /path/to/axons && make build
```

For semantic search, configure an embedding provider:
```bash
# Configure embedding provider (one-time setup)
axons config set embedding.provider ollama
axons config set embedding.model nomic-embed-text
```

## Workflow

### 1. Project Setup

```bash
# Register and build project if not already done
axons registry add /path/to/project --name project-name
cd /path/to/project
axons build -v

# Generate embeddings for semantic search (optional)
axons embed
```

### 2. Multi-Mode Search

```bash
# Keyword search (fast, exact matches)
axons search "functionName" --mode keyword --limit 10

# Semantic search using embeddings (natural language)
axons search "handle user authentication" --mode semantic --limit 5

# Regex pattern search
axons search "func.*Handler.*Request" --mode regex --limit 10

# Exact symbol search
axons search "UserService" --mode exact --limit 5

# Search with file type filter
axons search "error handling" --mode keyword --limit 5 --file "service.go"
```

### 3. Advanced Search Features

```bash
# Find function call paths
axons path --from "callerFunction" --to "calleeFunction"

# Analyze function usage
axons query callers --name "importantFunction"

# Get source code for symbols
axons query source --id 123

# Find symbols in a file
axons query nodes --file "handler.go"
```

### 4. Context-Aware Search

```bash
# Search with context (surrounding lines)
axons search "database connection" --context 5

# Find related code
axons query impact --name "targetFunction" --depth 2

# Search by symbol kind
axons search "create" --kind function
axons search "User" --kind class
```

## Output Format

Returns structured results with:
- Symbol name and kind (function, class, etc.)
- File location and line numbers
- Source code snippets
- Relationship information (callers, callees)

## Example Usage

Search for authentication-related code:
```bash
cd /path/to/project
axons build -v

# Semantic search
axons search "user login and authentication" --mode semantic --limit 10

# Keyword search with filters
axons search "auth" --kind function --file "api/" --limit 20

# Get details for found symbols
axons query source --id 42
axons query callers --id 42
```

## Common Edge Cases

- **No results**: Try different search modes or broaden query terms
- **Too many results**: Use filters (`--kind`, `--file`) to narrow down
- **Semantic search unavailable**: Run `axons embed` to generate embeddings first
- **Large codebases**: Use `--limit` to control result size

## Search Mode Comparison

| Mode | Speed | Use Case | Example |
|------|-------|----------|---------|
| keyword | Fast | Exact matches | `"getUserById"` |
| semantic | Medium | Natural language | `"handle user authentication"` |
| regex | Fast | Pattern matching | `"func.*Handler"` |
| exact | Fast | Direct lookup | `"UserService"` |