package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var cochangeCmd = &cobra.Command{
	Use:   "co-change [file]",
	Short: i18n.T("cmd.cochange.short"),
	Long: `Analyze git history for files that change together (no daemon required).

Uses git log to find temporal coupling between files and computes
Jaccard similarity coefficients.

Examples:
  axons co-change                    # Show top co-change pairs
  axons co-change src/api.go         # Show partners for a file
  axons co-change --analyze          # Scan git history and populate data
  axons co-change --analyze --since "6 months ago"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCoChange,
}

var (
	cochangeAnalyze    bool
	cochangeSince      string
	cochangeMinSupport int
	cochangeMinJaccard float64
	cochangeFull       bool
	cochangeLimit      int
	cochangeNoTests    bool
	cochangeJSON       bool
)

func init() {
	rootCmd.AddCommand(cochangeCmd)

	cochangeCmd.Flags().BoolVar(&cochangeAnalyze, "analyze", false, "Scan git history and populate co-change data")
	cochangeCmd.Flags().StringVar(&cochangeSince, "since", "1 year ago", "Git date for history window")
	cochangeCmd.Flags().IntVar(&cochangeMinSupport, "min-support", 3, "Minimum co-occurrence count")
	cochangeCmd.Flags().Float64Var(&cochangeMinJaccard, "min-jaccard", 0.3, "Minimum Jaccard similarity")
	cochangeCmd.Flags().BoolVar(&cochangeFull, "full", false, "Force full re-scan")
	cochangeCmd.Flags().IntVarP(&cochangeLimit, "limit", "n", 20, "Max results")
	cochangeCmd.Flags().BoolVarP(&cochangeNoTests, "no-tests", "T", false, "Exclude test/spec files")
	cochangeCmd.Flags().BoolVarP(&cochangeJSON, "json", "j", false, "Output as JSON")
}

func runCoChange(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	var file string
	if len(args) > 0 {
		file = args[0]
	}

	rootDir, _ := filepath.Abs(".")
	svc := core.NewCoChangeService(repo)
	opts := &core.CoChangeOptions{
		File:       file,
		Since:      cochangeSince,
		MinSupport: cochangeMinSupport,
		MinJaccard: cochangeMinJaccard,
		Full:       cochangeFull,
		Limit:      cochangeLimit,
		NoTests:    cochangeNoTests,
	}
	if cochangeAnalyze {
		opts.RootDir = rootDir
	}

	pairs, err := svc.Analyze(opts)
	if err != nil {
		return fmt.Errorf("co-change analysis failed: %w", err)
	}

	if cochangeJSON {
		data, _ := json.MarshalIndent(pairs, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "No co-change pairs found.")
		return nil
	}

	if file != "" {
		fmt.Printf("\nCo-change partners for %s:\n\n", file)
		for _, p := range pairs {
			partner := p.FileB
			if p.FileA != file {
				partner = p.FileA
			}
			fmt.Printf("  %-40s  count: %3d  jaccard: %.3f\n", truncatePath(partner, 40), p.CommitCount, p.Jaccard)
		}
	} else {
		fmt.Printf("\nTop co-change pairs (since %s):\n\n", cochangeSince)
		for _, p := range pairs {
			fmt.Printf("  %-40s  %-40s  count: %3d  jaccard: %.3f\n",
				truncatePath(p.FileA, 40), truncatePath(p.FileB, 40), p.CommitCount, p.Jaccard)
		}
	}
	return nil
}