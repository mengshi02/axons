package cmd

import (
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var cfgCmd = &cobra.Command{
	Use:   "cfg <name>",
	Short: i18n.T("cmd.cfg.short"),
	Long: `Display the control flow graph (CFG) for a function or method (no daemon required).

Examples:
  axons cfg myFunction
  axons cfg myFunction --format dot
  axons cfg myFunction --format mermaid`,
	Args: cobra.ExactArgs(1),
	RunE: runCFG,
}

var (
	cfgFormat  string
	cfgFile    []string
	cfgKind    string
	cfgNoTests bool
)

func init() {
	AddCommand(cfgCmd)
	cfgCmd.Flags().StringVar(&cfgFormat, "format", "text", "Output format: text, dot, mermaid")
	cfgCmd.Flags().StringArrayVarP(&cfgFile, "file", "f", nil, "Scope to file (partial match, repeatable)")
	cfgCmd.Flags().StringVarP(&cfgKind, "kind", "k", "", "Filter by symbol kind (function, method)")
	cfgCmd.Flags().BoolVar(&cfgNoTests, "no-tests", false, "Exclude test files")
}

func runCFG(cmd *cobra.Command, args []string) error {
	name := args[0]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewCFGService(repo)
	results, err := svc.Query(&core.CFGOptions{
		Name:    name,
		File:    cfgFile,
		Kind:    cfgKind,
		NoTests: cfgNoTests,
		Limit:   20,
	})
	if err != nil {
		return fmt.Errorf("CFG query failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("No symbols matching %q.\n", name)
		return nil
	}

	switch cfgFormat {
	case "dot":
		for _, r := range results {
			fmt.Printf("digraph \"%s\" {\n  rankdir=TB;\n", r.Name)
			for _, b := range r.Blocks {
				fmt.Printf("  B%d [label=\"%s L%d\"];\n", b.Index, b.Type, b.StartLine)
			}
			for _, e := range r.Edges {
				fmt.Printf("  B%d -> B%d [label=\"%s\"];\n", e.Source, e.Target, e.Kind)
			}
			fmt.Println("}")
		}
	case "mermaid":
		for _, r := range results {
			fmt.Println("graph TD")
			for _, b := range r.Blocks {
				fmt.Printf("  B%d[\"%s L%d\"]\n", b.Index, b.Type, b.StartLine)
			}
			for _, e := range r.Edges {
				fmt.Printf("  B%d -->|%s| B%d\n", e.Source, e.Kind, e.Target)
			}
		}
	default:
		for _, r := range results {
			fmt.Printf("\n%s %s  (%s:%d)\n", r.Kind, r.Name, r.File, r.Line)
			fmt.Println(strings.Repeat("─", 60))
			fmt.Printf("  Blocks: %d  Edges: %d\n", r.Summary.BlockCount, r.Summary.EdgeCount)
			for _, b := range r.Blocks {
				fmt.Printf("    [%d] %s L%d\n", b.Index, b.Type, b.StartLine)
			}
			if r.Summary.BlockCount == 0 {
				fmt.Println("  (no AST data available — run 'axons build --ast' to enable CFG)")
			}
		}
	}
	return nil
}