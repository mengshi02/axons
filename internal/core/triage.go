package core

import (
	"context"
	"os/exec"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// TriageService ranks changed symbols by risk and impact.
type TriageService struct {
	repo *repository.Repository
}

// NewTriageService creates a new TriageService.
func NewTriageService(repo *repository.Repository) *TriageService {
	return &TriageService{repo: repo}
}

// TriageOptions holds triage options.
type TriageOptions struct {
	Files   []string
	Base    string // git base branch; empty = uncommitted
	Top     int
	SortBy  string // risk, impact, complexity, changes
	RootDir string
}

// TriageItem is a single prioritized symbol.
type TriageItem struct {
	Name        string
	Kind        types.SymbolKind
	File        string
	Line        int
	RiskScore   float64
	ImpactScore float64
	Complexity  int
	Callers     int
	Role        types.Role
	Reason      string
}

// TriageResult holds triage output.
type TriageResult struct {
	Items        []TriageItem
	TotalFiles   int
	TotalSymbols int
}

// Analyze runs triage analysis.
func (s *TriageService) Analyze(ctx context.Context, opts *TriageOptions) (*TriageResult, error) {
	top := opts.Top
	if top <= 0 {
		top = 20
	}

	// Get changed files
	changedFiles := opts.Files
	if len(changedFiles) == 0 {
		changedFiles, _ = getGitChangedFiles(opts.RootDir, opts.Base)
	}

	result := &TriageResult{TotalFiles: len(changedFiles)}

	// Collect all nodes in changed files
	var allNodes []*types.Node
	for _, f := range changedFiles {
		nodes, err := s.repo.FindNodesByFile(f)
		if err != nil {
			continue
		}
		allNodes = append(allNodes, nodes...)
	}
	result.TotalSymbols = len(allNodes)

	// Score each node
	items := make([]TriageItem, 0, len(allNodes))
	for _, n := range allNodes {
		if n.Kind != types.SymbolKindFunction && n.Kind != types.SymbolKindMethod {
			continue
		}
		callers, _ := s.repo.FindCallers(n.ID)
		fanIn := len(callers)

		// Simple risk = fanIn * 0.5 + (entry/core role bonus)
		impact := float64(fanIn) / 20.0
		if impact > 1.0 {
			impact = 1.0
		}
		risk := impact
		if n.Role == types.RoleEntry {
			risk = min64(risk+0.3, 1.0)
		}

		reason := "changed symbol"
		if fanIn > 5 {
			reason = "high fan-in function"
		}
		if n.Role == types.RoleEntry {
			reason = "entry point"
		}

		items = append(items, TriageItem{
			Name:        n.Name,
			Kind:        n.Kind,
			File:        n.File,
			Line:        n.Line,
			RiskScore:   risk,
			ImpactScore: impact,
			Callers:     fanIn,
			Role:        n.Role,
			Reason:      reason,
		})
	}

	// Sort by risk descending
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].RiskScore > items[j-1].RiskScore; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	if len(items) > top {
		items = items[:top]
	}
	result.Items = items
	return result, nil
}

func getChangedFilesFromGit(rootDir, base string) []string {
	files, _ := getGitChangedFiles(rootDir, base)
	return files
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// gitChangedInDir returns files changed vs base in the given directory.
func gitChangedInDir(rootDir, base string) ([]string, error) {
	var args []string
	if base != "" {
		args = []string{"diff", "--name-only", base + "...HEAD"}
	} else {
		args = []string{"diff", "--name-only", "HEAD"}
	}
	cmd := exec.Command("git", args...)
	if rootDir != "" {
		cmd.Dir = rootDir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}