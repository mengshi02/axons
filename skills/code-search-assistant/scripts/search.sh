#!/bin/bash
# Multi-mode code search with context and filters

set -e

QUERY="$1"
MODE="${2:-keyword}"
OUTPUT_DIR="${3:-./search-results}"

if [ -z "$QUERY" ]; then
    echo "Usage: $0 <query> [mode] [output-dir]"
    echo "Modes: keyword (default), semantic, regex, exact"
    echo "Example: $0 'user authentication' semantic ./results"
    exit 1
fi

echo "=== Code Search ==="
echo "Query: $QUERY"
echo "Mode: $MODE"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build search command based on mode
SEARCH_CMD="axons search \"$QUERY\" --mode $MODE --limit 50"

echo "Searching..."

# Execute search
case "$MODE" in
    keyword)
        echo "Using keyword search (fast, exact matches)..."
        eval "$SEARCH_CMD --json" > "$OUTPUT_DIR/results.json" 2>/dev/null
        ;;
    semantic)
        echo "Using semantic search (natural language understanding)..."
        echo "Note: Requires embeddings to be generated first (axons embed)"
        eval "$SEARCH_CMD --json" > "$OUTPUT_DIR/results.json" 2>/dev/null || {
            echo "Warning: Semantic search failed. Did you run 'axons embed'?"
            echo "Falling back to keyword search..."
            axons search "$QUERY" --mode keyword --limit 50 --json > "$OUTPUT_DIR/results.json"
        }
        ;;
    regex)
        echo "Using regex pattern search..."
        eval "$SEARCH_CMD --json" > "$OUTPUT_DIR/results.json" 2>/dev/null
        ;;
    exact)
        echo "Using exact symbol lookup..."
        eval "$SEARCH_CMD --json" > "$OUTPUT_DIR/results.json" 2>/dev/null
        ;;
    *)
        echo "Error: Unknown mode '$MODE'"
        echo "Valid modes: keyword, semantic, regex, exact"
        exit 1
        ;;
esac

# Count results
RESULT_COUNT=$(jq 'length' "$OUTPUT_DIR/results.json" 2>/dev/null || echo "0")

echo ""
echo "=== Search Complete ==="
echo "Found: $RESULT_COUNT results"
echo "Results: $OUTPUT_DIR/results.json"
echo ""

if [ "$RESULT_COUNT" -gt 0 ]; then
    echo "Top 10 Results:"
    jq -r '.[:10][] | "  - \(.name) (\(.kind)) - \(.file):\(.line)"' "$OUTPUT_DIR/results.json"

    # Generate additional analysis for top results
    echo ""
    echo "Generating analysis for top results..."

    # Get source code for top 5 results
    echo "  - Extracting source code..."
    jq -r '.[:5][].id' "$OUTPUT_DIR/results.json" | while read -r ID; do
        if [ -n "$ID" ]; then
            axons query source --id "$ID" >> "$OUTPUT_DIR/sources.txt" 2>/dev/null
            echo "---" >> "$OUTPUT_DIR/sources.txt"
        fi
    done

    # Find callers for top result
    TOP_ID=$(jq -r '.[0].id' "$OUTPUT_DIR/results.json")
    if [ -n "$TOP_ID" ] && [ "$TOP_ID" != "null" ]; then
        echo "  - Finding callers for top result..."
        axons query callers --id "$TOP_ID" --json > "$OUTPUT_DIR/top-callers.json" 2>/dev/null || echo "[]" > "$OUTPUT_DIR/top-callers.json"
    fi

    echo ""
    echo "Additional files:"
    echo "  - Sources:    $OUTPUT_DIR/sources.txt"
    echo "  - Callers:    $OUTPUT_DIR/top-callers.json"
else
    echo "No results found. Try:"
    echo "  - Different search terms"
    echo "  - Different mode (keyword, semantic, regex, exact)"
    echo "  - Broadening your query"
fi