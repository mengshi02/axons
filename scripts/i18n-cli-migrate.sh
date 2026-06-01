#!/bin/bash
# CLI i18n migration script - replaces Short strings with i18n.T() calls
set -e
cd /Users/mengshi3/go/src/github.com/mengshi02/axons

# Helper: replace Short and add import
migrate_cmd() {
    local file="$1"
    local key="$2"
    local old_short="$3"
    
    # Replace Short string
    sed -i '' "s|Short: \"${old_short}\"|Short: i18n.T(\"${key}\")|" "$file"
    
    # Add i18n import if not already present
    if ! grep -q 'internal/i18n' "$file"; then
        sed -i '' 's|"github.com/spf13/cobra"|"github.com/mengshi02/axons/internal/i18n"\n\t"github.com/spf13/cobra"|' "$file"
    fi
}

migrate_cmd cmd/axons/cmd/query.go "cmd.query.short" "Query the code graph"
migrate_cmd cmd/axons/cmd/search.go "cmd.search.short" "Search code using semantic, keyword, or hybrid search"
migrate_cmd cmd/axons/cmd/audit.go "cmd.audit.short" "Run comprehensive code audit"
migrate_cmd cmd/axons/cmd/cfg.go "cmd.cfg.short" "Show control flow graph for a function"
migrate_cmd cmd/axons/cmd/owners.go "cmd.owners.short" "Show CODEOWNERS mapping for files and functions"
migrate_cmd cmd/axons/cmd/path.go "cmd.path.short" "Find call paths between two symbols"
migrate_cmd cmd/axons/cmd/sequence.go "cmd.sequence.short" "Generate a Mermaid sequence diagram from call graph"
migrate_cmd cmd/axons/cmd/check.go "cmd.check.short" "CI gate checks for code quality"
migrate_cmd cmd/axons/cmd/complexity.go "cmd.complexity.short" "Analyze code complexity"
migrate_cmd cmd/axons/cmd/cochange.go "cmd.cochange.short" "Analyze git history for files that change together"
migrate_cmd cmd/axons/cmd/dataflow.go "cmd.dataflow.short" "Analyze data flow for a function"
migrate_cmd cmd/axons/cmd/diff_impact.go "cmd.diffImpact.short" "Analyze impact of uncommitted changes or branch diff"
migrate_cmd cmd/axons/cmd/branch_compare.go "cmd.branchCompare.short" "Compare code structure between two branches/refs"
migrate_cmd cmd/axons/cmd/stats.go "cmd.stats.short" "Show database statistics"
migrate_cmd cmd/axons/cmd/export.go "cmd.export.short" "Export the code graph"
migrate_cmd cmd/axons/cmd/snapshot.go "cmd.snapshot.short" "Save and restore graph database snapshots"
migrate_cmd cmd/axons/cmd/triage.go "cmd.triage.short" "Triage and prioritize code review"
migrate_cmd cmd/axons/cmd/daemon.go "cmd.daemon.short" "Manage the axons daemon"
migrate_cmd cmd/axons/cmd/watch.go "cmd.watch.short" "Manage file watchers for incremental updates"
migrate_cmd cmd/axons/cmd/registry.go "cmd.registry.short" "Manage multi-repository registry"
migrate_cmd cmd/axons/cmd/embed.go "cmd.embed.short" "Build semantic embeddings for code symbols"

# Daemon sub-commands (daemon.go already has i18n import)
sed -i '' 's|Short: "Start the axons daemon"|Short: i18n.T("cmd.daemon.start.short")|' cmd/axons/cmd/daemon.go
sed -i '' 's|Short: "Stop the axons daemon"|Short: i18n.T("cmd.daemon.stop.short")|' cmd/axons/cmd/daemon.go

echo "All CLI commands migrated successfully"