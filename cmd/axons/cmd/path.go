package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var pathCmd = &cobra.Command{
	Use:   "path <from> <to>",
	Short: i18n.T("cmd.path.short"),
	Long: `Find all call paths from one symbol to another (no daemon required).

Examples:
  axons path main handler
  axons path main handler --max-depth 10
  axons path UserService.create Database.save --json`,
	Args: cobra.ExactArgs(2),
	RunE: runPath,
}

var (
	pathJSON     bool
	pathMaxDepth int
	pathAll      bool
	pathFile     string
)

func init() {
	rootCmd.AddCommand(pathCmd)
	pathCmd.Flags().BoolVar(&pathJSON, "json", false, "Output as JSON")
	pathCmd.Flags().IntVarP(&pathMaxDepth, "max-depth", "d", 10, "Maximum search depth")
	pathCmd.Flags().BoolVar(&pathAll, "all", false, "Find all paths (can be slow)")
	pathCmd.Flags().StringVarP(&pathFile, "file", "f", "", "Filter by file path (partial match)")
}

func runPath(cmd *cobra.Command, args []string) error {
	fromName := args[0]
	toName := args[1]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewPathService(repo)
	result, err := svc.Find(context.Background(), &core.PathOptions{
		From:     fromName,
		To:       toName,
		MaxDepth: pathMaxDepth,
		File:     pathFile,
	})
	if err != nil {
		return err
	}

	fmt.Printf("\n=== Call Paths: %s -> %s ===\n\n", result.From, result.To)

	if len(result.Paths) == 0 {
		fmt.Fprintln(os.Stderr, "No paths found.")
		if pathMaxDepth < 20 {
			fmt.Fprintln(os.Stderr, "Try increasing --max-depth for deeper searches.")
		}
		return nil
	}

	fmt.Printf("Found %d path(s):\n\n", result.TotalPaths)

	display := result.Paths
	if len(display) > 10 {
		display = display[:10]
		fmt.Println("(Showing first 10 paths, use --all to find more)")
	}

	for i, path := range display {
		fmt.Printf("Path %d (%d steps):\n", i+1, len(path)-1)
		for j, step := range path {
			indent := "  "
			if j > 0 {
				indent = "    " + strings.Repeat("  ", j-1)
			}
			prefix := "* "
			if j > 0 {
				prefix = "-> "
			}
			fmt.Printf("%s%s[%s] %s (%s:%d)\n", indent, prefix, step.Kind, step.Name, step.File, step.Line)
		}
		fmt.Println()
	}
	return nil
}