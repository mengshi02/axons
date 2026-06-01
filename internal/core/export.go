package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// ExportService exports the code graph to various formats.
type ExportService struct {
	repo *repository.Repository
}

// NewExportService creates a new ExportService.
func NewExportService(repo *repository.Repository) *ExportService {
	return &ExportService{repo: repo}
}

// ExportOptions holds export options.
type ExportOptions struct {
	Format      string // json, dot, mermaid, csv
	EdgeFilter  string
	Limit       int
}

// ExportResult holds the exported data.
type ExportResult struct {
	Nodes  []*types.Node
	Edges  []*types.Edge
	Raw    string // for dot/mermaid/csv formats
	Format string
}

// Export exports the graph in the requested format.
func (s *ExportService) Export(ctx context.Context, opts *ExportOptions) (*ExportResult, error) {
	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	edges, err := s.repo.ListAllEdges()
	if err != nil {
		return nil, fmt.Errorf("list edges: %w", err)
	}

	// Apply limit
	if opts.Limit > 0 && len(nodes) > opts.Limit {
		nodes = nodes[:opts.Limit]
	}

	// Apply edge filter
	if opts.EdgeFilter != "" {
		var filtered []*types.Edge
		for _, e := range edges {
			if string(e.Kind) == opts.EdgeFilter {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}

	result := &ExportResult{Nodes: nodes, Edges: edges, Format: opts.Format}

	switch opts.Format {
	case "dot":
		result.Raw = exportDOT(nodes, edges)
	case "mermaid":
		result.Raw = exportMermaid(nodes, edges, opts.Limit)
	case "csv":
		result.Raw = exportCSV(nodes)
	}

	return result, nil
}

func exportDOT(nodes []*types.Node, edges []*types.Edge) string {
	var sb strings.Builder
	sb.WriteString("digraph axons {\n  rankdir=LR;\n  node [shape=box];\n\n")
	for _, n := range nodes {
		label := fmt.Sprintf("%s\\n%s", n.Name, n.File)
		sb.WriteString(fmt.Sprintf("  n%d [label=\"%s\"];\n", n.ID, label))
	}
	sb.WriteString("\n")
	for _, e := range edges {
		sb.WriteString(fmt.Sprintf("  n%d -> n%d [label=\"%s\"];\n", e.SourceID, e.TargetID, e.Kind))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func exportMermaid(nodes []*types.Node, edges []*types.Edge, limit int) string {
	var sb strings.Builder
	sb.WriteString("graph LR\n")
	shown := 0
	maxNodes := limit
	if maxNodes <= 0 || maxNodes > 200 {
		maxNodes = 200
	}
	for _, n := range nodes {
		if shown >= maxNodes {
			break
		}
		sb.WriteString(fmt.Sprintf("  n%d[\"%s\"]\n", n.ID, n.Name))
		shown++
	}
	for _, e := range edges {
		sb.WriteString(fmt.Sprintf("  n%d --> n%d\n", e.SourceID, e.TargetID))
	}
	return sb.String()
}

func exportCSV(nodes []*types.Node) string {
	var sb strings.Builder
	sb.WriteString("id,name,kind,file,line,exported\n")
	for _, n := range nodes {
		exported := "false"
		if n.Exported {
			exported = "true"
		}
		sb.WriteString(fmt.Sprintf("%d,%q,%s,%s,%d,%s\n",
			n.ID, n.Name, n.Kind, n.File, n.Line, exported))
	}
	return sb.String()
}