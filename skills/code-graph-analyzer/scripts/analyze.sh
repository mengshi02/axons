#!/bin/bash
# Analyze a codebase and generate comprehensive reports

set -e

PROJECT_PATH="${1:-.}"
PROJECT_NAME="${2:-$(basename "$(cd "$PROJECT_PATH" && pwd)")}"
OUTPUT_DIR="${3:-./analysis-reports}"

echo "=== Code Graph Analysis ==="
echo "Project: $PROJECT_NAME"
echo "Path: $PROJECT_PATH"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Register project
echo "1. Registering project..."
axons registry add "$PROJECT_PATH" --name "$PROJECT_NAME"

# Navigate to project
cd "$PROJECT_PATH"

# Build code graph
echo "2. Building code graph..."
axons build -v

# Generate reports
echo "3. Generating analysis reports..."

# Audit report
echo "  - Audit report..."
axons audit --json > "$OUTPUT_DIR/audit.json"

# Complexity analysis
echo "  - Complexity metrics..."
axons complexity --top 50 > "$OUTPUT_DIR/complexity.txt"

# Code ownership
echo "  - Code ownership..."
axons owners . --json > "$OUTPUT_DIR/ownership.json"

# Statistics
echo "  - Project statistics..."
axons stats > "$OUTPUT_DIR/stats.txt"

# Co-change analysis
echo "  - Co-change patterns..."
axons cochange --min-count 2 --json > "$OUTPUT_DIR/cochange.json" 2>/dev/null || echo "    (skipped - no git history)"

echo ""
echo "=== Analysis Complete ==="
echo "Reports saved to: $OUTPUT_DIR"
echo ""
echo "Summary:"
echo "  - Audit: $OUTPUT_DIR/audit.json"
echo "  - Complexity: $OUTPUT_DIR/complexity.txt"
echo "  - Ownership: $OUTPUT_DIR/ownership.json"
echo "  - Statistics: $OUTPUT_DIR/stats.txt"
echo "  - Co-change: $OUTPUT_DIR/cochange.json"

# Quick summary
if command -v jq &> /dev/null; then
    echo ""
    echo "Quick Summary:"
    jq -r '"  Cycles: \(.cycles | length)\n  Dead Code: \(.deadCode | length)\n  High Complexity: \(.highComplexity | length)"' "$OUTPUT_DIR/audit.json"
fi