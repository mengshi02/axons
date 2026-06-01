// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: i18n.T("cmd.stats.short"),
	Long: `Display statistics about the code graph database (no daemon required).

Examples:
  axons stats                    # Show statistics from local DB`,
	Run: runStats,
}

func init() {
	AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer closeDB()

	svc := core.NewStatsService(repo)
	stats, err := svc.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Code Graph Statistics ===")
	fmt.Printf("Total Nodes: %d\n", stats.TotalNodes)
	fmt.Printf("Total Edges: %d\n", stats.TotalEdges)

	if len(stats.NodesByKind) > 0 {
		fmt.Println("\n--- Nodes by Kind ---")
		for kind, count := range stats.NodesByKind {
			fmt.Printf("  %s: %d\n", kind, count)
		}
	}

	if len(stats.EdgesByKind) > 0 {
		fmt.Println("\n--- Edges by Kind ---")
		for kind, count := range stats.EdgesByKind {
			fmt.Printf("  %s: %d\n", kind, count)
		}
	}
}