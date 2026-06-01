---
name: dependency-tracker
description: Analyze and visualize code dependencies, find circular dependencies, and map module relationships. Use when users need to understand code coupling, identify refactoring opportunities, or analyze the impact of changes. Traces function calls, imports, and architectural relationships.
license: MIT
metadata:
  author: axons
  version: "1.0"
allowed-tools: Bash(axons:*) Bash(jq:*) Read Write
---

# Dependency Tracker

This skill analyzes code dependencies to identify circular dependencies, map module relationships, and understand code coupling patterns using the axons CLI tool.

## When to Use

- When analyzing code coupling and dependencies
- When identifying circular dependencies
- When planning refactoring or architectural changes
- When understanding the impact of code changes
- When mapping module boundaries and interfaces

## Prerequisites

The axons CLI must be available and built. If not, build it first:
```bash
cd /path/to/axons && make build
```

## Workflow

### 1. Project Setup

```bash
# Register and build project if not already done
axons registry add /path/to/project --name project-name
cd /path/to/project
axons build -v
```

### 2. Dependency Analysis

```bash
# Find circular dependencies
axons audit --fail-on-cycle --json > cycles.json

# Analyze function call paths
axons path --from "sourceFunction" --to "targetFunction"

# Generate sequence diagrams
axons sequence "keyFunction" --depth 5

# Analyze data flow
axons dataflow "importantFunction"

# Find callers of a function
axons query callers --name "Database.connect"

# Find callees (what a function calls)
axons query callees --name "UserService.create"
```

### 3. Impact Analysis

```bash
# Analyze change impact
axons query impact --name "Config.load" --depth 3

# Find all call chains
axons sequence "handleRequest" --depth 5

# Check architecture rules
axons audit --json | jq '.architectureViolations'
```

## Output Format

Generates:
- JSON reports with dependency metrics
- Mermaid diagrams for visualization
- Text summaries of findings
- Call chain traces

## Example Usage

Analyze circular dependencies in a TypeScript project:
```bash
cd /path/to/ts-project
axons build -v

# Find all cycles
axons audit --json > audit.json

# Extract cycle details
jq '.cycles[] | {nodes: .nodes, length: (.nodes | length)}' audit.json

# Find the path between two modules
axons path --from "UserService.login" --to "Database.query" --max-depth 6
```

## Common Edge Cases

- **Deep call chains**: Use `--max-depth` to limit search depth
- **Circular dependencies**: Start with high-impact cycles for refactoring
- **Missing symbols**: Ensure project is fully indexed before analysis