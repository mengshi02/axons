#!/bin/bash
# Trace dependencies and analyze impact for a given symbol

set -e

SYMBOL_NAME="$1"
DEPTH="${2:-3}"
OUTPUT_DIR="${3:-./dependency-reports}"

if [ -z "$SYMBOL_NAME" ]; then
    echo "Usage: $0 <symbol-name> [depth] [output-dir]"
    echo "Example: $0 Database.connect 3 ./reports"
    exit 1
fi

echo "=== Dependency Analysis for: $SYMBOL_NAME ==="
echo "Depth: $DEPTH"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Find the symbol ID
echo "1. Finding symbol..."
SYMBOL_ID=$(axons query node --name "$SYMBOL_NAME" --json 2>/dev/null | jq -r '.id // empty')

if [ -z "$SYMBOL_ID" ]; then
    echo "Error: Symbol '$SYMBOL_NAME' not found"
    exit 1
fi

echo "   Found symbol ID: $SYMBOL_ID"

# Analyze callers (who calls this symbol)
echo "2. Analyzing callers..."
axons query callers --id "$SYMBOL_ID" --json > "$OUTPUT_DIR/callers.json"
CALLER_COUNT=$(jq 'length' "$OUTPUT_DIR/callers.json")
echo "   Found $CALLER_COUNT direct callers"

# Analyze callees (what this symbol calls)
echo "3. Analyzing callees..."
axons query callees --id "$SYMBOL_ID" --json > "$OUTPUT_DIR/callees.json"
CALLEE_COUNT=$(jq 'length' "$OUTPUT_DIR/callees.json")
echo "   Found $CALLEE_COUNT direct callees"

# Analyze impact (all upstream callers)
echo "4. Analyzing impact scope..."
axons query impact --id "$SYMBOL_ID" --depth "$DEPTH" --json > "$OUTPUT_DIR/impact.json"
IMPACT_COUNT=$(jq '.nodes | length' "$OUTPUT_DIR/impact.json")
echo "   Impact radius: $IMPACT_COUNT functions (depth: $DEPTH)"

# Generate call sequence diagram
echo "5. Generating call sequence..."
axons sequence "$SYMBOL_NAME" --depth "$DEPTH" > "$OUTPUT_DIR/sequence.txt" 2>/dev/null || echo "   (skipped - no sequence data)"

# Check for circular dependencies involving this symbol
echo "6. Checking for cycles..."
axons audit --json 2>/dev/null | jq --arg sym "$SYMBOL_NAME" '.cycles[] | select(.nodes[] | contains($sym))' > "$OUTPUT_DIR/cycles.json" 2>/dev/null || echo "[]" > "$OUTPUT_DIR/cycles.json"
CYCLE_COUNT=$(jq 'length' "$OUTPUT_DIR/cycles.json")
if [ "$CYCLE_COUNT" -gt 0 ]; then
    echo "   ⚠️  Found in $CYCLE_COUNT circular dependency(ies)"
else
    echo "   ✓ No circular dependencies"
fi

echo ""
echo "=== Analysis Complete ==="
echo "Reports saved to: $OUTPUT_DIR"
echo ""
echo "Summary:"
echo "  Direct callers: $CALLER_COUNT"
echo "  Direct callees: $CALLEE_COUNT"
echo "  Impact radius:  $IMPACT_COUNT (depth $DEPTH)"
echo "  Cycles:         $CYCLE_COUNT"
echo ""
echo "Reports:"
echo "  - Callers:     $OUTPUT_DIR/callers.json"
echo "  - Callees:     $OUTPUT_DIR/callees.json"
echo "  - Impact:      $OUTPUT_DIR/impact.json"
echo "  - Sequence:    $OUTPUT_DIR/sequence.txt"
echo "  - Cycles:      $OUTPUT_DIR/cycles.json"