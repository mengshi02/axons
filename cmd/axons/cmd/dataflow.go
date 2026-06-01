package cmd

import (
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var dataflowCmd = &cobra.Command{
	Use:   "dataflow <name>",
	Short: i18n.T("cmd.dataflow.short"),
	Long: `Show dataflow edges (flows_to, returns, mutates) for a function (no daemon required).

Examples:
  axons dataflow myFunction
  axons dataflow myFunction --file mypackage`,
	Args: cobra.ExactArgs(1),
	RunE: runDataflow,
}

var (
	dataflowJSON   bool
	dataflowFile   string
	dataflowDetail bool
)

func init() {
	rootCmd.AddCommand(dataflowCmd)
	dataflowCmd.Flags().BoolVar(&dataflowJSON, "json", false, "Output as JSON")
	dataflowCmd.Flags().StringVarP(&dataflowFile, "file", "f", "", "Filter by file path (partial match)")
	dataflowCmd.Flags().BoolVarP(&dataflowDetail, "detail", "d", false, "Show detailed dataflow edges")
}

func runDataflow(cmd *cobra.Command, args []string) error {
	name := args[0]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewDataflowService(repo)
	result, err := svc.Analyze(&core.DataflowOptions{
		Name: name,
		File: dataflowFile,
	})
	if err != nil {
		return err
	}

	fmt.Printf("\n=== Dataflow Analysis: %s ===\n", result.Node.Name)
	fmt.Printf("File: %s:%d\n\n", result.Node.File, result.Node.Line)

	if len(result.DataflowEdges) == 0 {
		fmt.Println("No dataflow edges found.")
		fmt.Println("Note: Run 'axons build --dataflow' to index dataflow data.")
		return nil
	}

	fmt.Printf("Dataflow edges (%d):\n", len(result.DataflowEdges))
	for _, e := range result.DataflowEdges {
		fmt.Printf("  %s --[%s]--> %s\n", e.From, e.EdgeType, e.To)
	}

	if !dataflowDetail {
		fmt.Println("\nUse --detail for more information.")
	}

	grouped := make(map[string][]string)
	for _, e := range result.DataflowEdges {
		grouped[e.EdgeType] = append(grouped[e.EdgeType], e.To)
	}
	if dataflowDetail && len(grouped) > 0 {
		fmt.Println("\nGrouped by type:")
		for kind, targets := range grouped {
			fmt.Printf("  %s: %s\n", kind, strings.Join(targets, ", "))
		}
	}

	return nil
}