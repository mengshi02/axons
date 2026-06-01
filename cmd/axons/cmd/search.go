// Package cmd provides CLI commands.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: i18n.T("cmd.search.short"),
	Long: `Search for code symbols using various search modes (no daemon required).

Modes:
  - hybrid (default): Combines BM25 keyword search with semantic vector search
  - semantic: Pure vector similarity search using embeddings
  - keyword: Simple LIKE-based pattern matching

Examples:
  axons search "authentication function"
  axons search "handleError" --mode keyword
  axons search "database connection" --mode hybrid --limit 20`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var (
	searchMode     string
	searchLimit    int
	searchMinScore float64
	searchKind     string
	searchFile     string
	searchNoTests  bool
	searchJSON     bool
	searchTimeout  time.Duration
	searchProvider string
	searchModel    string
)

func init() {
	AddCommand(searchCmd)
	searchCmd.Flags().StringVarP(&searchMode, "mode", "m", "hybrid", "Search mode: hybrid, semantic, keyword")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 15, "Maximum number of results")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0.2, "Minimum similarity score (0-1)")
	searchCmd.Flags().StringVarP(&searchKind, "kind", "k", "", "Filter by symbol kind")
	searchCmd.Flags().StringVarP(&searchFile, "file", "f", "", "Filter by file path pattern")
	searchCmd.Flags().BoolVarP(&searchNoTests, "no-tests", "T", false, "Exclude test files")
	searchCmd.Flags().BoolVarP(&searchJSON, "json", "j", false, "Output as JSON")
	searchCmd.Flags().DurationVar(&searchTimeout, "timeout", 30*time.Second, "Search timeout")
	searchCmd.Flags().StringVarP(&searchProvider, "provider", "p", "ollama", "Embedding provider (for semantic search)")
	searchCmd.Flags().StringVarP(&searchModel, "model", "M", "", "Embedding model (for semantic search)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	validModes := map[string]bool{"hybrid": true, "semantic": true, "keyword": true}
	if !validModes[searchMode] {
		return fmt.Errorf("invalid mode %q: must be one of hybrid, semantic, keyword", searchMode)
	}

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewSearchService(repo, nil)

	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	req := &service.Request{
		Query:    query,
		Mode:     service.Mode(searchMode),
		Limit:    searchLimit,
		MinScore: float32(searchMinScore),
		Kind:     searchKind,
		File:     searchFile,
		NoTests:  searchNoTests,
	}

	resp, err := svc.Search(ctx, req)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if searchJSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(resp.Results) == 0 {
		fmt.Printf("No results found for query: %q\n", query)
		return nil
	}

	fmt.Printf("\n%s search: %q\n\n", strings.Title(searchMode), query)

	kindIcon := map[string]string{
		"function": "f", "method": "m", "class": "c",
		"interface": "i", "variable": "v",
	}

	for i, r := range resp.Results {
		icon := kindIcon[r.Kind]
		if icon == "" {
			icon = "o"
		}
		if r.RRFScore > 0 {
			fmt.Printf("  %d. RRF %.4f  %s %s -- %s:%d\n", i+1, r.RRFScore, icon, r.Name, r.File, r.Line)
		} else {
			bar := strings.Repeat("#", int(r.Score*20))
			fmt.Printf("  %d. %.1f%% %s  %s %s -- %s:%d\n", i+1, r.Score*100, bar, icon, r.Name, r.File, r.Line)
		}
		if r.QualifiedName != "" && r.QualifiedName != r.Name {
			fmt.Printf("      %s\n", r.QualifiedName)
		}
	}

	fmt.Printf("\n  %d results shown\n\n", len(resp.Results))
	if resp.Message != "" {
		fmt.Printf("  Note: %s\n", resp.Message)
	}

	return nil
}