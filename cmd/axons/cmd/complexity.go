// Package cmd provides CLI commands.
package cmd

import (
	"fmt"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var complexityCmd = &cobra.Command{
	Use:   "complexity",
	Short: i18n.T("cmd.complexity.short"),
	Long: `Analyze code complexity metrics for functions in the codebase (no daemon required).

Examples:
  axons complexity                      # Show top 20 most complex functions
  axons complexity --threshold 15       # Show functions with cyclomatic complexity >= 15
  axons complexity --file ./src/main.go # Show complexity for specific file
  axons complexity --top 10             # Show top 10 most complex functions`,
	RunE: runComplexity,
}

var (
	complexityThreshold int
	complexityTop       int
	complexityFile      string
)

func init() {
	AddCommand(complexityCmd)
	complexityCmd.Flags().IntVarP(&complexityThreshold, "threshold", "t", 10, "Cyclomatic complexity threshold")
	complexityCmd.Flags().IntVarP(&complexityTop, "top", "n", 20, "Number of top complex functions to show")
	complexityCmd.Flags().StringVarP(&complexityFile, "file", "f", "", "Filter by file path")
}

func runComplexity(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewComplexityService(repo)
	functions, err := svc.TopComplex(&core.ComplexityOptions{
		Threshold: complexityThreshold,
		Limit:     complexityTop,
		File:      complexityFile,
	})
	if err != nil {
		return fmt.Errorf("complexity analysis failed: %w", err)
	}

	if len(functions) == 0 {
		fmt.Println("No complexity metrics found. Run 'axons build' with complexity analysis enabled.")
		return nil
	}

	fmt.Println("=== Code Complexity Analysis ===")
	fmt.Printf("Threshold: Cyclomatic >= %d\n\n", complexityThreshold)

	for _, f := range functions {
		fmt.Printf("%s\n", f.Name)
		fmt.Printf("  Location: %s:%d\n", f.File, f.Line)
		fmt.Printf("  Cyclomatic: %d  Cognitive: %d  Nesting: %d\n", f.Cyclomatic, f.Cognitive, f.Nesting)
		fmt.Printf("  Risk: %s\n\n", getComplexityIndicator(f.Cyclomatic))
	}

	return nil
}

func getComplexityIndicator(cyclomatic int) string {
	switch {
	case cyclomatic <= 5:
		return "Low (Simple)"
	case cyclomatic <= 10:
		return "Medium (Moderate)"
	case cyclomatic <= 20:
		return "High (Complex)"
	default:
		return "Very High (Needs Refactoring)"
	}
}