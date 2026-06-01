---
name: code-graph-analyzer
description: Automatically analyze codebases for architecture, dependencies, and code quality. Use when users need to understand code structure, identify risks, or generate architectural insights. Handles project registration, code graph building, and comprehensive analysis.
license: MIT
metadata:
  author: axons
  version: "1.0"
allowed-tools: Bash(axons:*) Bash(jq:*) Read Write
---

# Code Graph Analyzer

This skill automatically analyzes codebases to provide architectural insights, dependency analysis, and code quality metrics using the axons CLI tool.

## When to Use

- When a user wants to understand the architecture of a codebase
- When analyzing code quality and identifying hotspots
- When preparing for refactoring or major changes
- When onboarding to a new codebase
- When generating reports for code reviews or audits

## Prerequisites

The axons CLI must be available and built. If not, build it first:
```bash
cd /path/to/axons && make build
```

## Workflow

### 1. Project Registration and Analysis

```bash
# Register the project
axons registry add /path/to/project --name project-name

# Navigate to project directory
cd /path/to/project

# Build the code graph
axons build -v

# Generate comprehensive analysis
axons audit --json > audit-report.json
axons complexity --top 20
axons owners . --json > ownership.json
axons stats > stats.json
```

### 2. Analyze Architecture

```bash
# Get project statistics
axons stats

# Identify modules
axons query modules

# Find circular dependencies
axons audit --fail-on-cycle

# Analyze code ownership
axons owners . --json
```

### 3. Identify Quality Issues

```bash
# Find high-complexity functions
axons complexity --top 20

# Detect dead code
axons audit --json | jq '.deadCode'

# Identify hotspots
axons query hotspots --limit 20

# Check co-change patterns
axons cochange --min-count 3
```

## Output Format

The skill generates structured reports:
- JSON audit reports with cycles, dead code, and complexity metrics
- Complexity rankings for refactoring prioritization
- Ownership maps for team assignments
- Statistics for project overview

## Example Usage

For a Go project analysis:
```bash
# Register and analyze
cd /path/to/go-project
axons registry add . --name go-project
axons build -v
axons audit --json | jq '{
  cycles: .cycles | length,
  deadCode: .deadCode | length,
  highComplexity: .highComplexity | length
}'
```

## Common Edge Cases

- **Large codebases**: Use `--incremental` flag for faster updates
- **Multiple languages**: Axons automatically detects and parses supported languages
- **Missing dependencies**: Build will still succeed, but some relationships may be incomplete