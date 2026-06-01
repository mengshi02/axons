package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var branchCompareCmd = &cobra.Command{
	Use:   "branch-compare <base> <target>",
	Short: i18n.T("cmd.branchCompare.short"),
	Long: `Compare symbol-level changes between two git branches or refs (no daemon required).

Shows changed files and all symbols in those files.

Examples:
  axons branch-compare main feature-branch
  axons branch-compare HEAD~10 HEAD --json`,
	Args: cobra.ExactArgs(2),
	RunE: runBranchCompare,
}

var (
	branchCompareDepth   int
	branchCompareNoTests bool
	branchCompareJSON    bool
)

func init() {
	rootCmd.AddCommand(branchCompareCmd)
	branchCompareCmd.Flags().IntVarP(&branchCompareDepth, "depth", "d", 3, "Max transitive caller depth")
	branchCompareCmd.Flags().BoolVarP(&branchCompareNoTests, "no-tests", "T", false, "Exclude test/spec files")
	branchCompareCmd.Flags().BoolVarP(&branchCompareJSON, "json", "j", false, "Output as JSON")
}

func runBranchCompare(cmd *cobra.Command, args []string) error {
	baseRef := args[0]
	targetRef := args[1]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	rootDir, _ := filepath.Abs(".")
	svc := core.NewBranchCompareService(repo)
	result, err := svc.Compare(context.Background(), &core.BranchCompareOptions{
		BaseRef:   baseRef,
		TargetRef: targetRef,
		RootDir:   rootDir,
		Depth:     branchCompareDepth,
		NoTests:   branchCompareNoTests,
	})
	if err != nil {
		return fmt.Errorf("branch-compare failed: %w", err)
	}

	if branchCompareJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\nbranch-compare: %s...%s\n", result.BaseRef, result.TargetRef)
	fmt.Printf("  Files changed: %d\n", len(result.ChangedFiles))
	for _, f := range result.ChangedFiles {
		fmt.Printf("    • %s\n", f)
	}

	if len(result.Changed) > 0 {
		fmt.Printf("\n  ~ Changed symbols (%d):\n", len(result.Changed))
		limit := 30
		for i, sym := range result.Changed {
			if i >= limit {
				fmt.Printf("    ... and %d more\n", len(result.Changed)-limit)
				break
			}
			fmt.Printf("    [~] %s (%s) -- %s:%d\n", sym.Name, sym.Kind, sym.File, sym.Line)
		}
	}

	fmt.Printf("\n  Summary: ~%d symbol(s) in %d changed file(s)\n",
		result.TotalImpacted, len(result.ChangedFiles))
	return nil
}

func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}