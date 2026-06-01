# Code Graph Analyzer Reference

## Command Reference

### axons build
Build code graph from source files.

```bash
axons build [path] [flags]

Flags:
  --verbose, -v      Show detailed progress
  --force           Force complete rebuild
  --incremental     Only process changed files (default: true)
  --output string   Output format (text, json)
```

### axons audit
Perform comprehensive code quality audit.

```bash
axons audit [flags]

Flags:
  --json                 Output as JSON
  --fail-on-cycle        Exit with error if cycles found
  --max-complexity int   Max complexity threshold (default: 20)
  --max-cycles int       Max cycles allowed (default: 0)
```

**Output Fields**:
- `cycles`: Circular dependency information
- `deadCode`: Uncalled functions
- `highComplexity`: Functions exceeding complexity threshold
- `entryPoints`: Public/exported functions
- `summary`: Aggregate statistics

### axons complexity
Analyze code complexity metrics.

```bash
axons complexity [flags]

Flags:
  --top int           Show top N complex functions (default: 20)
  --threshold int     Minimum complexity to report (default: 10)
  --json              Output as JSON
  --sort-by string    Sort by: cyclomatic, cognitive (default: cyclomatic)
```

### axons owners
Map code ownership from git history.

```bash
axons owners [path] [flags]

Flags:
  --json              Output as JSON
  --since string      Git history start date (default: 6 months ago)
  --top int           Top contributors per file (default: 3)
```

### axons stats
Display project statistics.

```bash
axons stats [flags]

Flags:
  --json              Output as JSON
```

**Statistics Include**:
- Total nodes (functions, classes, interfaces)
- Total edges (relationships)
- File count
- Language breakdown
- Average complexity

### axons cochange
Analyze co-change patterns from git history.

```bash
axons cochange [flags]

Flags:
  --since string      Git history start date
  --min-support int   Minimum co-change count (default: 2)
  --min-jaccard float Minimum Jaccard similarity (default: 0.3)
  --json              Output as JSON
  --limit int         Max results (default: 50)
```

## Output Formats

### JSON Output
Most commands support `--json` flag for structured output:

```json
{
  "cycles": [
    {
      "nodes": ["func1", "func2", "func3"],
      "length": 3
    }
  ],
  "deadCode": [
    {
      "name": "unusedFunc",
      "file": "service.go",
      "line": 42
    }
  ],
  "highComplexity": [
    {
      "name": "complexFunc",
      "complexity": 25,
      "file": "handler.go",
      "line": 100
    }
  ]
}
```

### Text Output
Default human-readable format with tables and summaries.

## Interpreting Results

### Circular Dependencies
- **Severity**: High
- **Action**: Break the cycle by introducing interfaces or refactoring
- **Priority**: Focus on cycles with critical functions

### Dead Code
- **Severity**: Medium
- **Action**: Verify if truly unused, then remove
- **Priority**: Functions in stable/production code

### High Complexity
- **Severity**: Medium-High
- **Action**: Refactor into smaller functions
- **Priority**: Functions with complexity > 30

## Common Workflows

### Pre-commit Check
```bash
axons audit --fail-on-cycle --max-complexity 30
```

### Architecture Review
```bash
axons build -v
axons audit --json | jq '.cycles'
axons complexity --top 20
axons stats
```

### Team Onboarding
```bash
axons stats
axons owners . --json
axons complexity --top 10
```