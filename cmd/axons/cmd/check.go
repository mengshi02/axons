package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: i18n.T("cmd.check.short"),
	Long: `Run CI gate checks to enforce code quality standards (no daemon required).

Exit codes:
  0: All checks passed
  1: Checks failed

Examples:
  axons check
  axons check --fail-on-dead-code
  axons check --max-complexity 10`,
	Args: cobra.NoArgs,
	Run:  runCheck,
}

var (
	checkFailOnDeadCode bool
	checkFailOnComplex  bool
	checkMaxComplexity  int
	checkBase           string
	checkOutput         string
	checkNoNewCycles    bool
	checkMaxCycleLength int
)

func init() {
	rootCmd.AddCommand(checkCmd)
	checkCmd.Flags().BoolVar(&checkFailOnDeadCode, "fail-on-dead-code", false, "Fail if dead code detected")
	checkCmd.Flags().BoolVar(&checkFailOnComplex, "fail-on-complex", false, "Fail if high complexity functions found")
	checkCmd.Flags().IntVar(&checkMaxComplexity, "max-complexity", 15, "Maximum allowed cyclomatic complexity")
	checkCmd.Flags().StringVar(&checkBase, "base", "", "Base branch to compare (default: HEAD)")
	checkCmd.Flags().StringVarP(&checkOutput, "output", "o", "text", "Output format: text, json")
	checkCmd.Flags().BoolVar(&checkNoNewCycles, "no-new-cycles", true, "Fail if new cycles introduced")
	checkCmd.Flags().IntVar(&checkMaxCycleLength, "max-cycle-length", 0, "Maximum allowed cycle length (0 = no limit)")
}

func runCheck(cmd *cobra.Command, args []string) {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer closeDB()

	svc := core.NewCheckService(repo)
	result := svc.Run(&core.CheckOptions{
		MaxComplexity:  checkMaxComplexity,
		FailOnDeadCode: checkFailOnDeadCode,
		FailOnComplex:  checkFailOnComplex,
		NoNewCycles:    checkNoNewCycles,
	})

	if checkOutput == "json" {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Println("\n=== CI Check Results ===")
		for _, c := range result.Checks {
			status := "[PASS]"
			if !c.Passed {
				status = "[FAIL]"
			}
			fmt.Printf("\n%s [%s] %s\n   %s\n", status, strings.ToUpper(c.Severity), c.Name, c.Message)
			for _, d := range c.Details {
				fmt.Printf("   - %s\n", d)
			}
			if c.Suggestion != "" && !c.Passed {
				fmt.Printf("   Suggestion: %s\n", c.Suggestion)
			}
		}
		fmt.Printf("\n---------------------------------\n")
		if result.Passed {
			fmt.Printf("[PASS] %s\n", result.Summary)
		} else {
			fmt.Printf("[FAIL] %s\n", result.Summary)
		}
	}

	if !result.Passed {
		os.Exit(1)
	}
}