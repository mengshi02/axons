# Code Search Assistant Reference

## Command Reference

### axons search
Multi-mode code search with flexible filtering.

```bash
axons search <query> [flags]

Flags:
  --mode string       Search mode: keyword, semantic, regex, exact (default: keyword)
  --kind string       Filter by symbol kind (function, class, method, interface)
  --file string       Filter by file path pattern
  --limit int         Max results (default: 20)
  --json              Output as JSON
  --context int       Include N lines of context around matches
```

### Search Modes

#### Keyword Mode (Default)
Fast full-text search using FTS5 with BM25 ranking.

```bash
# Simple keyword search
axons search "getUser"

# With filters
axons search "handler" --kind function --file "api/"

# Multiple keywords
axons search "user authentication login"
```

**Best for**:
- Exact function/class names
- Known keywords or identifiers
- Quick lookups

#### Semantic Mode
Vector similarity search using embeddings.

```bash
# Natural language queries
axons search "handle user authentication" --mode semantic

# Conceptual searches
axons search "database connection pooling" --mode semantic

# With threshold
axons search "error handling" --mode semantic --threshold 0.7
```

**Requirements**:
- Generate embeddings first: `axons embed`
- Configure embedding provider: `axons config set embedding.provider ollama`

**Best for**:
- Natural language descriptions
- Finding similar implementations
- Conceptual searches

#### Regex Mode
Regular expression pattern matching.

```bash
# Function patterns
axons search "func.*Handler.*Request" --mode regex

# Naming conventions
axons search "test.*_test" --mode regex

# Complex patterns
axons search "get|set|update.*User" --mode regex
```

**Best for**:
- Pattern matching
- Naming convention searches
- Complex structural queries

#### Exact Mode
Direct symbol lookup.

```bash
# Exact symbol name
axons search "UserService" --mode exact

# With kind filter
axons search "create" --mode exact --kind function
```

**Best for**:
- Known symbol names
- Quick lookups
- Disambiguation

### axons query node
Query a specific symbol by name or ID.

```bash
axons query node --name <symbol> [flags]
axons query node --id <id> [flags]

Flags:
  --json              Output as JSON
```

### axons query nodes
Find all symbols in a file or matching criteria.

```bash
axons query nodes --file <file> [flags]

Flags:
  --kind string       Filter by symbol kind
  --limit int         Max results (default: 50)
  --json              Output as JSON
```

### axons query source
Retrieve source code for one or more symbols.

```bash
axons query source --id <id> [flags]
axons query source --ids <id1,id2,id3> [flags]

Flags:
  --context int       Lines of context (default: 5)
```

### axons embed
Generate embeddings for semantic search.

```bash
axons embed [flags]

Flags:
  --provider string   Embedding provider: openai, ollama (default: ollama)
  --model string      Model name (default: nomic-embed-text)
  --batch-size int    Batch size for embedding (default: 100)
  --force             Regenerate all embeddings
```

## Search Filters

### By Symbol Kind
```bash
--kind function      # Functions and methods
--kind class         # Classes and structs
--kind interface     # Interfaces
--kind variable      # Variables and constants
--kind method        # Methods only
```

### By File Path
```bash
--file "api/"        # Files in api directory
--file "service.go"  # Specific file
--file "_test.go"    # Test files
```

### By Context
```bash
--context 5          # 5 lines before and after
```

## Output Format

### JSON Output
```json
[
  {
    "id": 123,
    "name": "getUserByID",
    "kind": "function",
    "file": "service/user_service.go",
    "line": 42,
    "signature": "func getUserByID(id int64) (*User, error)",
    "score": 0.95
  }
]
```

### Text Output
Default tabular format with key details.

## Search Strategies

### Finding Function Definitions
```bash
# Exact match
axons search "getUserByID" --mode exact

# With kind filter
axons search "getUser" --kind function
```

### Finding Implementations
```bash
# Semantic search
axons search "user authentication logic" --mode semantic

# By interface
axons search "Repository" --kind interface
```

### Finding Usage Examples
```bash
# Find callers
axons query callers --name "Database.connect"

# Find by pattern
axons search "DB\." --mode regex --file "service/"
```

### Exploring New Codebases
```bash
# Start broad
axons search "main"

# Find entry points
axons search "Handler" --kind function --file "api/"

# Understand structure
axons stats
axons query modules
```

## Performance Tips

1. **Use keyword mode for speed** - Fastest search mode
2. **Filter early** - Use `--kind` and `--file` to narrow results
3. **Limit results** - Use `--limit` for large result sets
4. **Cache embeddings** - Run `axons embed` once, reuse for semantic searches
5. **Index incrementally** - Use `--incremental` flag with `axons build`

## Common Issues

### No Results
- Try broader search terms
- Switch search mode
- Verify file is indexed: `axons query nodes --file <file>`

### Too Many Results
- Add filters: `--kind`, `--file`
- Use exact mode
- Reduce limit

### Semantic Search Fails
- Run: `axons embed`
- Check embedding provider: `axons config get embedding.provider`
- Verify model availability