// Package cmd provides CLI commands.
package cmd

import (
	"fmt"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query <name>",
	Short: i18n.T("cmd.query.short"),
	Long: `Query the code graph for symbols and their relationships (no daemon required).

Examples:
  axons query getUser              # Find symbols named getUser
  axons query --callers getUser    # Find callers of getUser
  axons query --callees main       # Find functions called by main`,
	Args: cobra.ExactArgs(1),
	RunE: runQuery,
}

var (
	queryKind    string
	queryFile    string
	queryCallers bool
	queryCallees bool
	queryNoTests bool
	queryLimit   int
)

func init() {
	AddCommand(queryCmd)
	queryCmd.Flags().StringVarP(&queryKind, "kind", "k", "", "Filter by symbol kind (function, method, class, etc.)")
	queryCmd.Flags().StringVarP(&queryFile, "file", "f", "", "Filter by file path")
	queryCmd.Flags().BoolVar(&queryCallers, "callers", false, "Show callers of the symbol")
	queryCmd.Flags().BoolVar(&queryCallees, "callees", false, "Show callees of the symbol")
	queryCmd.Flags().BoolVarP(&queryNoTests, "no-tests", "t", false, "Exclude test files")
	queryCmd.Flags().IntVarP(&queryLimit, "limit", "l", 20, "Limit number of results")
}

func runQuery(cmd *cobra.Command, args []string) error {
	name := args[0]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewQueryService(repo)
	result, err := svc.Query(&core.QueryOptions{
		Name:    name,
		Kind:    queryKind,
		File:    queryFile,
		Callers: queryCallers,
		Callees: queryCallees,
		NoTests: queryNoTests,
		Limit:   queryLimit,
	})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if len(result.Nodes) == 0 {
		fmt.Println("No results found")
		return nil
	}

	for _, node := range result.Nodes {
		kind := string(node.Kind)
		if node.Exported {
			kind += " (exported)"
		}
		fmt.Printf("%s [%s]\n", node.Name, kind)
		fmt.Printf("  Location: %s:%d\n", node.File, node.Line)
		if node.QualifiedName != "" && node.QualifiedName != node.Name {
			fmt.Printf("  Qualified: %s\n", node.QualifiedName)
		}
	}

	if len(result.Callers) > 0 {
		fmt.Printf("\nCallers:\n")
		for _, c := range result.Callers {
			fmt.Printf("  - %s [%s] (%s:%d)\n", c.Name, c.Kind, c.File, c.Line)
		}
	}

	if len(result.Callees) > 0 {
		fmt.Printf("\nCallees:\n")
		for _, c := range result.Callees {
			fmt.Printf("  - %s [%s] (%s:%d)\n", c.Name, c.Kind, c.File, c.Line)
		}
	}

	return nil
}