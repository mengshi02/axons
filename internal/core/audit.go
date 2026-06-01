package core

import (
	"database/sql"

	"github.com/mengshi02/axons/internal/db/repository"
)

// AuditService performs comprehensive code audits directly on the DB.
type AuditService struct {
	repo *repository.Repository
	db   *sql.DB
}

// NewAuditService creates a new AuditService.
func NewAuditService(repo *repository.Repository) *AuditService {
	return &AuditService{repo: repo, db: repo.DB()}
}

// AuditOptions holds audit parameters.
type AuditOptions struct {
	MaxCycles     int
	MaxComplexity int
}

// AuditResult holds the audit output.
type AuditResult struct {
	Summary        AuditSummary
	Cycles         []CycleInfo
	DeadCode       []DeadCodeItem
	HighComplexity []ComplexItem
	EntryPoints    []string
	Issues         int
}

// AuditSummary holds summary counts.
type AuditSummary struct {
	TotalNodes      int
	TotalEdges      int
	TotalFunctions  int
	TotalClasses    int
	CyclesFound     int
	DeadCodeCount   int
	ComplexWarnings int
	EntryPoints     int
}

// CycleInfo represents a detected dependency cycle.
type CycleInfo struct {
	Nodes  []string
	Length int
}

// DeadCodeItem represents an unreferenced function.
type DeadCodeItem struct {
	Name string
	Kind string
	File string
	Line int
}

// ComplexItem represents a high-complexity function.
type ComplexItem struct {
	Name       string
	File       string
	Line       int
	Cyclomatic int
	Cognitive  int
}

// Audit runs a full code audit and returns the result.
func (s *AuditService) Audit(opts *AuditOptions) *AuditResult {
	maxCycles := opts.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 10
	}
	maxComplex := opts.MaxComplexity
	if maxComplex <= 0 {
		maxComplex = 15
	}

	result := &AuditResult{}

	// Summary counts
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&result.Summary.TotalNodes)
	s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&result.Summary.TotalEdges)
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind IN ('function', 'method')`).Scan(&result.Summary.TotalFunctions)
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'class'`).Scan(&result.Summary.TotalClasses)

	// Cycles via DFS on call edges
	result.Cycles = s.detectCycles(maxCycles)
	result.Summary.CyclesFound = len(result.Cycles)

	// Dead code
	result.DeadCode = s.findDeadCode()
	result.Summary.DeadCodeCount = len(result.DeadCode)

	// High complexity
	result.HighComplexity = s.findHighComplexity(maxComplex)
	result.Summary.ComplexWarnings = len(result.HighComplexity)

	// Entry points
	result.EntryPoints = s.findEntryPoints()
	result.Summary.EntryPoints = len(result.EntryPoints)

	result.Issues = result.Summary.CyclesFound + result.Summary.DeadCodeCount + result.Summary.ComplexWarnings
	return result
}

func (s *AuditService) detectCycles(max int) []CycleInfo {
	// Reuse the same DFS logic as handlers.go — delegate to algorithms
	return nil // placeholder; real logic lives in algorithms package, called from CLI
}

func (s *AuditService) findDeadCode() []DeadCodeItem {
	rows, err := s.db.Query(`
		SELECT n.name, n.kind, n.file, n.line
		FROM nodes n
		WHERE n.kind IN ('function','method') AND n.exported = 0
		  AND NOT EXISTS (SELECT 1 FROM edges e WHERE e.target_id = n.id AND e.kind = 'calls')
		LIMIT 200
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []DeadCodeItem
	for rows.Next() {
		var it DeadCodeItem
		rows.Scan(&it.Name, &it.Kind, &it.File, &it.Line)
		items = append(items, it)
	}
	return items
}

func (s *AuditService) findHighComplexity(threshold int) []ComplexItem {
	rows, err := s.db.Query(`
		SELECT n.name, n.file, n.line, fc.cyclomatic, fc.cognitive
		FROM function_complexity fc
		JOIN nodes n ON fc.node_id = n.id
		WHERE fc.cyclomatic >= ?
		ORDER BY fc.cyclomatic DESC
		LIMIT 100
	`, threshold)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []ComplexItem
	for rows.Next() {
		var it ComplexItem
		rows.Scan(&it.Name, &it.File, &it.Line, &it.Cyclomatic, &it.Cognitive)
		items = append(items, it)
	}
	return items
}

func (s *AuditService) findEntryPoints() []string {
	rows, err := s.db.Query(`
		SELECT DISTINCT n.qualified_name
		FROM nodes n
		WHERE n.name IN ('main', 'Main', 'init')
		   OR n.role = 'entry_point'
		LIMIT 50
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var eps []string
	for rows.Next() {
		var ep string
		rows.Scan(&ep)
		eps = append(eps, ep)
	}
	return eps
}