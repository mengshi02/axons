package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: i18n.T("cmd.audit.short"),
	Long: `Run a comprehensive audit of the codebase (no daemon required).

Analyzes:
- Circular dependencies (cycles)
- Dead code (unreferenced functions)
- High complexity functions
- Structure issues

Examples:
  axons audit
  axons audit --json
  axons audit --fail-on-cycle`,
	Args: cobra.NoArgs,
	RunE: runAudit,
}

var (
	auditJSON        bool
	auditFailOnCycle bool
	auditMaxComplex  int
	auditMaxCycles   int
)

func init() {
	rootCmd.AddCommand(auditCmd)

	auditCmd.Flags().BoolVar(&auditJSON, "json", false, "Output as JSON")
	auditCmd.Flags().BoolVar(&auditFailOnCycle, "fail-on-cycle", false, "Exit with error if cycles found")
	auditCmd.Flags().IntVar(&auditMaxComplex, "max-complexity", 15, "Complexity threshold for warnings")
	auditCmd.Flags().IntVar(&auditMaxCycles, "max-cycles", 10, "Maximum cycles to report")
}

func runAudit(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewAuditService(repo)
	result := svc.Audit(&core.AuditOptions{
		MaxCycles:     auditMaxCycles,
		MaxComplexity: auditMaxComplex,
	})

	if auditJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	} else {
		printCoreAuditResult(result)
	}

	if auditFailOnCycle && result.Summary.CyclesFound > 0 {
		os.Exit(1)
	}
	return nil
}

func printCoreAuditResult(result *core.AuditResult) {
	fmt.Println("\n=== Code Audit Report ===")
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Nodes: %d  Edges: %d\n", result.Summary.TotalNodes, result.Summary.TotalEdges)
	fmt.Printf("  Functions: %d  Classes: %d\n", result.Summary.TotalFunctions, result.Summary.TotalClasses)
	fmt.Printf("  Entry Points: %d\n", result.Summary.EntryPoints)

	fmt.Printf("\nIssues:\n")
	fmt.Printf("  Cycles: %d\n", result.Summary.CyclesFound)
	fmt.Printf("  Dead Code: %d\n", result.Summary.DeadCodeCount)
	fmt.Printf("  High Complexity: %d\n", result.Summary.ComplexWarnings)
	fmt.Printf("  Total Issues: %d\n", result.Issues)

	// Cycles
	if len(result.Cycles) > 0 {
		fmt.Printf("\nCircular Dependencies (%d):\n", len(result.Cycles))
		for i, cycle := range result.Cycles {
			fmt.Printf("  %d. %s (length: %d)\n", i+1, strings.Join(cycle.Nodes, " -> "), cycle.Length)
		}
	}

	// Dead code
	if len(result.DeadCode) > 0 {
		fmt.Printf("\nDead Code (%d):\n", len(result.DeadCode))
		for _, dc := range result.DeadCode {
			fmt.Printf("  * [%s] %s (%s:%d)\n", dc.Kind, dc.Name, dc.File, dc.Line)
		}
	}

	// High complexity
	if len(result.HighComplexity) > 0 {
		fmt.Printf("\nHigh Complexity (%d):\n", len(result.HighComplexity))
		for _, hc := range result.HighComplexity {
			fmt.Printf("  * %s (%s:%d) - cyclomatic: %d, cognitive: %d\n",
				hc.Name, hc.File, hc.Line, hc.Cyclomatic, hc.Cognitive)
		}
	}

	// Entry points
	if len(result.EntryPoints) > 0 && len(result.EntryPoints) <= 20 {
		fmt.Printf("\nEntry Points (%d):\n", len(result.EntryPoints))
		sort.Strings(result.EntryPoints)
		for _, ep := range result.EntryPoints {
			fmt.Printf("  * %s\n", ep)
		}
	}
}

// AuditResult represents the audit output (kept for JSON compatibility with old code)
type AuditResult struct {
	Summary        AuditSummary   `json:"summary"`
	Cycles         []CycleInfo    `json:"cycles,omitempty"`
	DeadCode       []DeadCodeInfo `json:"dead_code,omitempty"`
	HighComplexity []ComplexInfo  `json:"high_complexity,omitempty"`
	UnexportInfo   []UnexportInfo `json:"unexported,omitempty"`
	EntryPoints    []string       `json:"entry_points,omitempty"`
	Issues         int            `json:"issues"`
}

// AuditSummary summarizes the audit
type AuditSummary struct {
	TotalNodes      int `json:"total_nodes"`
	TotalEdges      int `json:"total_edges"`
	TotalFunctions  int `json:"total_functions"`
	TotalClasses    int `json:"total_classes"`
	CyclesFound     int `json:"cycles_found"`
	DeadCodeCount   int `json:"dead_code_count"`
	ComplexWarnings int `json:"complex_warnings"`
	EntryPoints     int `json:"entry_points"`
}

// CycleInfo represents a detected cycle
type CycleInfo struct {
	Nodes  []string `json:"nodes"`
	Length int      `json:"length"`
}

// DeadCodeInfo represents dead code
type DeadCodeInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// ComplexInfo represents a high complexity function
type ComplexInfo struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
}

// UnexportInfo represents an unexported function issue
type UnexportInfo struct {
	Name    string `json:"name"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Callers int    `json:"callers"`
}