package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var triageCmd = &cobra.Command{
	Use:   "triage [file]",
	Short: i18n.T("cmd.triage.short"),
	Long: `Analyze code changes and prioritize review by risk and impact (no daemon required).

Examples:
  axons triage                          # Triage all uncommitted changes
  axons triage src/auth/login.go        # Triage specific file
  axons triage --base main --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTriage,
}

var (
	triageJSON   bool
	triageBase   string
	triageTop    int
	triageSortBy string
)

func init() {
	rootCmd.AddCommand(triageCmd)
	triageCmd.Flags().BoolVar(&triageJSON, "json", false, "Output as JSON")
	triageCmd.Flags().StringVar(&triageBase, "base", "", "Base branch to compare (default: uncommitted changes)")
	triageCmd.Flags().IntVarP(&triageTop, "top", "n", 20, "Number of top items to show")
	triageCmd.Flags().StringVar(&triageSortBy, "sort", "risk", "Sort by: risk, impact, complexity, changes")
}

func runTriage(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	var files []string
	if len(args) > 0 {
		files = []string{args[0]}
	}

	rootDir, _ := filepath.Abs(".")
	svc := core.NewTriageService(repo)
	result, err := svc.Analyze(context.Background(), &core.TriageOptions{
		Files:   files,
		Base:    triageBase,
		Top:     triageTop,
		SortBy:  triageSortBy,
		RootDir: rootDir,
	})
	if err != nil {
		return fmt.Errorf("triage failed: %w", err)
	}

	if triageJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n=== Triage Report ===\nFiles: %d  Symbols: %d\n\n", result.TotalFiles, result.TotalSymbols)

	if len(result.Items) == 0 {
		fmt.Fprintln(os.Stderr, "No symbols to triage (no changed files found).")
		return nil
	}

	fmt.Println("Top Priority Items:")
	fmt.Println("────────────────────────────────────────────────────────────────────────────────")
	for i, item := range result.Items {
		risk := "●"
		if item.RiskScore >= 0.7 {
			risk = "[HIGH]"
		} else if item.RiskScore >= 0.4 {
			risk = "[MED] "
		} else {
			risk = "[LOW] "
		}
		fmt.Printf("%2d. %s [%s] %s\n", i+1, risk, item.Kind, item.Name)
		fmt.Printf("    File: %s:%d\n", item.File, item.Line)
		fmt.Printf("    Risk: %.2f  Impact: %.2f  Callers: %d\n", item.RiskScore, item.ImpactScore, item.Callers)
		fmt.Printf("    Reason: %s\n\n", item.Reason)
	}
	return nil
}