// Package cmd provides CLI commands.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export [output]",
	Short: i18n.T("cmd.export.short"),
	Long: `Export the code graph to various formats (no daemon required).

Examples:
  axons export graph.json              # Export to JSON
  axons export --format dot graph.dot  # Export to DOT format
  axons export --format mermaid        # Export to Mermaid
  axons export --format csv graph.csv  # Export nodes to CSV`,
	Args: cobra.MaximumNArgs(1),
	RunE: runExport,
}

var (
	exportFormat string
	exportFilter string
	exportLimit  int
)

func init() {
	AddCommand(exportCmd)
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "json", "Output format (json, dot, mermaid, csv)")
	exportCmd.Flags().StringVarP(&exportFilter, "filter", "t", "", "Filter by edge type (calls, imports, etc.)")
	exportCmd.Flags().IntVarP(&exportLimit, "limit", "l", 0, "Limit number of nodes (0 = no limit)")
}

func runExport(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewExportService(repo)
	result, err := svc.Export(context.Background(), &core.ExportOptions{
		Format:     exportFormat,
		EdgeFilter: exportFilter,
		Limit:      exportLimit,
	})
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	outputPath := "graph.json"
	if len(args) > 0 {
		outputPath = args[0]
	}

	if result.Raw != "" {
		if err := os.WriteFile(outputPath, []byte(result.Raw), 0644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Printf("Exported to %s\n", outputPath)
		return nil
	}

	data := map[string]interface{}{
		"nodes": result.Nodes,
		"edges": result.Edges,
	}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	fmt.Printf("Exported to %s (%d nodes, %d edges)\n", outputPath, len(result.Nodes), len(result.Edges))
	return nil
}