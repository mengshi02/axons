package core

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// OwnersService analyzes CODEOWNERS ownership.
type OwnersService struct {
	repo *repository.Repository
}

// NewOwnersService creates a new OwnersService.
func NewOwnersService(repo *repository.Repository) *OwnersService {
	return &OwnersService{repo: repo}
}

// OwnersOptions holds options.
type OwnersOptions struct {
	RootDir    string
	Owner      string
	Boundary   bool
	FileFilter []string
	NoTests    bool
}

// FileOwnership maps a file to its owners.
type FileOwnership struct {
	File   string
	Owners []string
}

// BoundaryEdge represents a call crossing owner boundaries.
type BoundaryEdge struct {
	From FileOwnership
	To   FileOwnership
}

// OwnersSummary contains aggregate ownership stats.
type OwnersSummary struct {
	TotalFiles     int
	TotalSymbols   int
	UnownedFiles   int
	UnownedSymbols int
	ByOwner        map[string]int
}

// OwnersResult holds the full ownership analysis.
type OwnersResult struct {
	CodeownersFile string
	Summary        OwnersSummary
	Files          []FileOwnership
	Boundaries     []BoundaryEdge
}

// Analyze performs ownership analysis.
func (s *OwnersService) Analyze(opts *OwnersOptions) (*OwnersResult, error) {
	ownerMap, codeownersFile := parseCodeowners(opts.RootDir)

	files, err := s.repo.GetAllFiles()
	if err != nil {
		return nil, err
	}

	result := &OwnersResult{
		CodeownersFile: codeownersFile,
		Summary:        OwnersSummary{ByOwner: make(map[string]int)},
	}

	fileOwnership := make(map[string][]string)
	for _, f := range files {
		owners := matchOwners(f, ownerMap)
		fileOwnership[f] = owners

		if opts.Owner == "" || containsOwner(owners, opts.Owner) {
			fo := FileOwnership{File: f, Owners: owners}
			result.Files = append(result.Files, fo)
		}

		if len(owners) == 0 {
			result.Summary.UnownedFiles++
		}
		for _, o := range owners {
			result.Summary.ByOwner[o]++
		}
	}
	result.Summary.TotalFiles = len(files)

	// Boundary edges
	if opts.Boundary {
		edges, _ := s.repo.ListAllEdges()
		nodeCache := make(map[int64]*types.Node)
		for _, e := range edges {
			src := cachedNode(s.repo, nodeCache, e.SourceID)
			tgt := cachedNode(s.repo, nodeCache, e.TargetID)
			if src == nil || tgt == nil || src.File == tgt.File {
				continue
			}
			srcOwners := fileOwnership[src.File]
			tgtOwners := fileOwnership[tgt.File]
			if !sameOwners(srcOwners, tgtOwners) {
				result.Boundaries = append(result.Boundaries, BoundaryEdge{
					From: FileOwnership{File: src.File, Owners: srcOwners},
					To:   FileOwnership{File: tgt.File, Owners: tgtOwners},
				})
			}
		}
	}

	return result, nil
}

// parseCodeowners reads the CODEOWNERS file and returns pattern→owners map.
func parseCodeowners(rootDir string) (map[string][]string, string) {
	candidates := []string{
		filepath.Join(rootDir, ".github", "CODEOWNERS"),
		filepath.Join(rootDir, "CODEOWNERS"),
		filepath.Join(rootDir, "docs", "CODEOWNERS"),
	}
	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()
		m := make(map[string][]string)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				m[parts[0]] = parts[1:]
			}
		}
		return m, path
	}
	return nil, ""
}

func matchOwners(file string, ownerMap map[string][]string) []string {
	// Reverse order to find the last matching pattern (CODEOWNERS semantics)
	var matched []string
	for pattern, owners := range ownerMap {
		if globMatch(pattern, file) {
			matched = owners
		}
	}
	return matched
}

func globMatch(pattern, file string) bool {
	if strings.HasPrefix(pattern, "/") {
		pattern = pattern[1:]
	}
	matched, _ := filepath.Match(pattern, file)
	if matched {
		return true
	}
	return strings.HasPrefix(file, strings.TrimSuffix(pattern, "/"))
}

func containsOwner(owners []string, target string) bool {
	for _, o := range owners {
		if o == target {
			return true
		}
	}
	return false
}

func sameOwners(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool)
	for _, o := range a {
		set[o] = true
	}
	for _, o := range b {
		if !set[o] {
			return false
		}
	}
	return true
}

func cachedNode(repo *repository.Repository, cache map[int64]*types.Node, id int64) *types.Node {
	if n, ok := cache[id]; ok {
		return n
	}
	n, _ := repo.FindNodeByID(id)
	if n != nil {
		cache[id] = n
	}
	return n
}