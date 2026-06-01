package core

import (
	"fmt"

	"github.com/mengshi02/axons/internal/db/repository"
)

// CheckService runs CI-gate quality checks directly on the DB.
type CheckService struct {
	audit      *AuditService
	complexity *ComplexityService
}

// NewCheckService creates a new CheckService.
func NewCheckService(repo *repository.Repository) *CheckService {
	return &CheckService{
		audit:      NewAuditService(repo),
		complexity: NewComplexityService(repo),
	}
}

// CheckOptions holds check configuration.
type CheckOptions struct {
	MaxComplexity  int
	FailOnDeadCode bool
	FailOnComplex  bool
	NoNewCycles    bool
}

// CheckItem represents a single check result.
type CheckItem struct {
	Name       string
	Passed     bool
	Severity   string
	Message    string
	Details    []string
	Suggestion string
}

// CheckResult holds all check results.
type CheckResult struct {
	Passed       bool
	Checks       []CheckItem
	TotalChecks  int
	PassedChecks int
	FailedChecks int
	Summary      string
}

// Run executes all checks and returns the result.
func (s *CheckService) Run(opts *CheckOptions) *CheckResult {
	maxComplex := opts.MaxComplexity
	if maxComplex <= 0 {
		maxComplex = 15
	}

	result := &CheckResult{}

	// Check 1: cycles
	audit := s.audit.Audit(&AuditOptions{MaxCycles: 20, MaxComplexity: maxComplex})
	cycleCheck := CheckItem{Name: "No circular dependencies", Severity: "error"}
	if audit.Summary.CyclesFound == 0 {
		cycleCheck.Passed = true
		cycleCheck.Message = "No circular dependencies detected"
	} else {
		cycleCheck.Message = fmt.Sprintf("%d circular dependency cycle(s) found", audit.Summary.CyclesFound)
		cycleCheck.Suggestion = "Refactor to remove cycles using dependency injection or interfaces"
		for _, c := range audit.Cycles {
			cycleCheck.Details = append(cycleCheck.Details, fmt.Sprintf("Cycle: %v", c.Nodes))
		}
	}
	result.Checks = append(result.Checks, cycleCheck)

	// Check 2: complexity
	complexFuncs, _ := s.complexity.TopComplex(&ComplexityOptions{Threshold: maxComplex, Limit: 50})
	complexCheck := CheckItem{Name: fmt.Sprintf("Complexity <= %d", maxComplex), Severity: "warning"}
	if len(complexFuncs) == 0 {
		complexCheck.Passed = true
		complexCheck.Message = "All functions within complexity limits"
	} else {
		complexCheck.Message = fmt.Sprintf("%d function(s) exceed cyclomatic complexity %d", len(complexFuncs), maxComplex)
		complexCheck.Suggestion = "Break down complex functions into smaller units"
		for _, f := range complexFuncs {
			complexCheck.Details = append(complexCheck.Details,
				fmt.Sprintf("%s (%s:%d) cyclomatic=%d", f.Name, f.File, f.Line, f.Cyclomatic))
		}
	}
	result.Checks = append(result.Checks, complexCheck)

	// Check 3: dead code (optional)
	if opts.FailOnDeadCode {
		deadCheck := CheckItem{Name: "No dead code", Severity: "warning"}
		if audit.Summary.DeadCodeCount == 0 {
			deadCheck.Passed = true
			deadCheck.Message = "No dead code detected"
		} else {
			deadCheck.Message = fmt.Sprintf("%d dead code item(s) found", audit.Summary.DeadCodeCount)
			deadCheck.Suggestion = "Remove unused functions or add tests to confirm they are needed"
		}
		result.Checks = append(result.Checks, deadCheck)
	}

	// Aggregate
	result.TotalChecks = len(result.Checks)
	for _, c := range result.Checks {
		if c.Passed {
			result.PassedChecks++
		} else {
			result.FailedChecks++
		}
	}
	result.Passed = result.FailedChecks == 0 || (!opts.NoNewCycles && cycleCheck.Passed && (!opts.FailOnComplex || complexCheck.Passed))
	result.Passed = result.FailedChecks == 0

	if result.Passed {
		result.Summary = fmt.Sprintf("All %d checks passed", result.TotalChecks)
	} else {
		result.Summary = fmt.Sprintf("%d/%d checks failed", result.FailedChecks, result.TotalChecks)
	}

	return result
}