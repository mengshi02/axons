package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var diffImpactCmd = &cobra.Command{
	Use:   "diff-impact [<branch>]",
	Short: i18n.T("cmd.diffImpact.short"),
	Long: `Analyze the blast radius of pending changes (no daemon required).

Scans the call graph to determine what would be affected by changes
in the current working tree (uncommitted) or compared to a branch.

Examples:
  axons diff-impact                    # Analyze uncommitted changes
  axons diff-impact main               # Compare against main branch
  axons diff-impact origin/main --json # JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDiffImpact,
}

var (
	diffImpactJSON     bool
	diffImpactDepth    int
	diffImpactCallers  bool
	diffImpactDataflow bool
	diffImpactTimeout  time.Duration
)

func init() {
	rootCmd.AddCommand(diffImpactCmd)

	diffImpactCmd.Flags().BoolVar(&diffImpactJSON, "json", false, "Output as JSON")
	diffImpactCmd.Flags().IntVarP(&diffImpactDepth, "depth", "d", 3, "Max traversal depth for callers")
	diffImpactCmd.Flags().BoolVar(&diffImpactCallers, "callers", true, "Show caller chain")
	diffImpactCmd.Flags().BoolVar(&diffImpactDataflow, "dataflow", false, "Include dataflow analysis")
	diffImpactCmd.Flags().DurationVar(&diffImpactTimeout, "timeout", 30*time.Second, "Analysis timeout")
}

func runDiffImpact(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	var branch string
	if len(args) > 0 {
		branch = args[0]
	}

	rootDir, _ := filepath.Abs(".")
	svc := core.NewDiffImpactService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), diffImpactTimeout)
	defer cancel()

	result, err := svc.Analyze(ctx, &core.DiffImpactOptions{
		RootDir:  rootDir,
		Branch:   branch,
		MaxDepth: diffImpactDepth,
	})
	if err != nil {
		return fmt.Errorf("diff-impact analysis failed: %w", err)
	}

	if diffImpactJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Println("\n=== Diff Impact Analysis ===")
	if branch != "" {
		fmt.Printf("Comparing against: %s\n", branch)
	} else {
		fmt.Println("Analyzing uncommitted changes")
	}

	fmt.Printf("\nChanged files (%d):\n", len(result.ChangedFiles))
	for _, f := range result.ChangedFiles {
		fmt.Printf("  • %s\n", f)
	}

	fmt.Printf("\nImpacted nodes (%d total):\n", result.TotalAffected)
	limit := 30
	for i, n := range result.ImpactedNodes {
		if i >= limit {
			fmt.Printf("  ... and %d more\n", result.TotalAffected-limit)
			break
		}
		fmt.Printf("  • [%s] %s (%s:%d)\n", n.Kind, n.Name, n.File, n.Line)
	}

	if result.TotalAffected == 0 {
		fmt.Fprintln(os.Stderr, "No impacted nodes found.")
	}

	return nil
}