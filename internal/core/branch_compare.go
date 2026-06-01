package core

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// BranchCompareService compares symbol-level changes between two git refs.
type BranchCompareService struct {
	repo *repository.Repository
}

// NewBranchCompareService creates a new BranchCompareService.
func NewBranchCompareService(repo *repository.Repository) *BranchCompareService {
	return &BranchCompareService{repo: repo}
}

// BranchCompareOptions holds comparison options.
type BranchCompareOptions struct {
	BaseRef   string
	TargetRef string
	RootDir   string
	Depth     int
	NoTests   bool
}

// SymbolChange represents a changed symbol.
type SymbolChange struct {
	Name string
	Kind types.SymbolKind
	File string
	Line int
}

// BranchCompareResult holds the comparison result.
type BranchCompareResult struct {
	BaseRef      string
	TargetRef    string
	ChangedFiles []string
	Added        []SymbolChange
	Removed      []SymbolChange
	Changed      []SymbolChange
	TotalImpacted int
}

// Compare performs a symbol-level diff between two git refs.
// Since we work from the current DB (single-branch), we approximate by:
// 1. Getting the list of changed files between the two refs via git
// 2. Returning all symbols in those files as "changed"
func (s *BranchCompareService) Compare(ctx context.Context, opts *BranchCompareOptions) (*BranchCompareResult, error) {
	changedFiles, baseSHA, targetSHA, err := gitDiffFiles(opts.RootDir, opts.BaseRef, opts.TargetRef)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	_ = baseSHA
	_ = targetSHA

	result := &BranchCompareResult{
		BaseRef:      opts.BaseRef,
		TargetRef:    opts.TargetRef,
		ChangedFiles: changedFiles,
	}

	for _, f := range changedFiles {
		if opts.NoTests && strings.Contains(f, "_test.") {
			continue
		}
		nodes, _ := s.repo.FindNodesByFile(f)
		for _, n := range nodes {
			result.Changed = append(result.Changed, SymbolChange{
				Name: n.Name,
				Kind: n.Kind,
				File: n.File,
				Line: n.Line,
			})
		}
	}
	result.TotalImpacted = len(result.Changed)
	return result, nil
}

func gitDiffFiles(rootDir, baseRef, targetRef string) ([]string, string, string, error) {
	rangeArg := baseRef + "..." + targetRef
	cmd := exec.Command("git", "diff", "--name-only", rangeArg)
	if rootDir != "" {
		cmd.Dir = rootDir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, "", "", err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}

	// Get SHAs
	baseSHA, _ := gitRevParse(rootDir, baseRef)
	targetSHA, _ := gitRevParse(rootDir, targetRef)
	return files, baseSHA, targetSHA, nil
}

func gitRevParse(rootDir, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", ref)
	if rootDir != "" {
		cmd.Dir = rootDir
	}
	out, err := cmd.Output()
	if err != nil {
		return ref, nil
	}
	return strings.TrimSpace(string(out)), nil
}