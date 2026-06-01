// Package algorithms provides code graph building and querying capabilities.
package algorithms

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestNewGraphAdapter(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "main.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	if adapter == nil {
		t.Fatal("NewGraphAdapter returned nil")
	}

	if len(adapter.nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(adapter.nodes))
	}

	if len(adapter.nodeMap) != 2 {
		t.Errorf("Expected 2 nodes in nodeMap, got %d", len(adapter.nodeMap))
	}
}

func TestGraphAdapter_EmptyGraph(t *testing.T) {
	adapter := NewGraphAdapter(nil, nil)
	if adapter == nil {
		t.Fatal("NewGraphAdapter returned nil for empty graph")
	}

	// Empty graph should still work
	sccs := adapter.TarjanSCC()
	if len(sccs) != 0 {
		t.Errorf("Expected 0 SCCs for empty graph, got %d", len(sccs))
	}
}

func TestGraphAdapter_SingleNode(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
	}

	adapter := NewGraphAdapter(nodes, nil)

	// Single node should have one SCC
	sccs := adapter.TarjanSCC()
	if len(sccs) != 1 {
		t.Errorf("Expected 1 SCC for single node, got %d", len(sccs))
	}

	if len(sccs[0]) != 1 {
		t.Errorf("Expected SCC of size 1, got %d", len(sccs[0]))
	}
}

func TestGraphAdapter_SimpleCallGraph(t *testing.T) {
	// main -> helper -> util
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)

	// No cycles, so each node is its own SCC
	sccs := adapter.TarjanSCC()
	if len(sccs) != 3 {
		t.Errorf("Expected 3 SCCs for DAG, got %d", len(sccs))
	}
}

func TestGraphAdapter_CyclicGraph(t *testing.T) {
	// A -> B -> A (cycle)
	nodes := []*types.Node{
		{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
		{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 1, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)

	// Should detect one SCC containing both nodes
	sccs := adapter.TarjanSCC()

	// Find the SCC with more than one node
	largestSCC := 0
	for _, scc := range sccs {
		if len(scc) > largestSCC {
			largestSCC = len(scc)
		}
	}

	if largestSCC != 2 {
		t.Errorf("Expected SCC of size 2 for cycle, got largest SCC size %d", largestSCC)
	}
}

func TestGraphAdapter_GetSCCInfo(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	info := adapter.GetSCCInfo()

	if info == nil {
		t.Fatal("GetSCCInfo returned nil")
	}

	if len(info.Components) != 3 {
		t.Errorf("Expected 3 components, got %d", len(info.Components))
	}

	if len(info.ComponentSizes) != 3 {
		t.Errorf("Expected 3 component sizes, got %d", len(info.ComponentSizes))
	}
}

func TestGraphAdapter_IsDAG(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []*types.Node
		edges    []*types.Edge
		expected bool
	}{
		{
			name:     "empty graph",
			nodes:    nil,
			edges:    nil,
			expected: true,
		},
		{
			name: "single node",
			nodes: []*types.Node{
				{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
			},
			edges:    nil,
			expected: true,
		},
		{
			name: "simple DAG",
			nodes: []*types.Node{
				{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
				{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
			},
			edges: []*types.Edge{
				{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
			},
			expected: true,
		},
		{
			name: "graph with cycle",
			nodes: []*types.Node{
				{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
				{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
			},
			edges: []*types.Edge{
				{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
				{ID: 2, SourceID: 2, TargetID: 1, Kind: types.EdgeKindCalls},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewGraphAdapter(tt.nodes, tt.edges)
			got := adapter.IsDAG()
			if got != tt.expected {
				t.Errorf("IsDAG() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGraphAdapter_TopologicalOrder(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	order := adapter.TopologicalOrder()

	if len(order) != 3 {
		t.Errorf("Expected 3 nodes in topological order, got %d", len(order))
	}
}

func TestGraphAdapter_ConnectedComponents(t *testing.T) {
	// Two disconnected subgraphs
	nodes := []*types.Node{
		{ID: 1, Name: "A1", Kind: types.SymbolKindFunction, File: "a.go"},
		{ID: 2, Name: "A2", Kind: types.SymbolKindFunction, File: "a.go"},
		{ID: 3, Name: "B1", Kind: types.SymbolKindFunction, File: "b.go"},
		{ID: 4, Name: "B2", Kind: types.SymbolKindFunction, File: "b.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 3, TargetID: 4, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	components := adapter.ConnectedComponents()

	if len(components) < 1 {
		t.Error("Expected at least one connected component")
	}
}

func TestGraphAdapter_GraphMetrics(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	metrics := adapter.CalculateMetrics()

	if metrics == nil {
		t.Fatal("CalculateMetrics returned nil")
	}

	if metrics.TotalNodes != 3 {
		t.Errorf("TotalNodes = %d, want 3", metrics.TotalNodes)
	}

	if metrics.TotalEdges != 2 {
		t.Errorf("TotalEdges = %d, want 2", metrics.TotalEdges)
	}
}

func TestGraphAdapter_ShortestPaths(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
		{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
		{ID: 3, Name: "C", Kind: types.SymbolKindFunction, File: "c.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)

	// Find shortest paths from node A (ID 1)
	var gonumID int64
	for gID, origID := range adapter.idMap {
		if origID == 1 {
			gonumID = gID
			break
		}
	}

	distances, paths := adapter.ShortestPaths(gonumID)

	if distances == nil {
		t.Error("ShortestPaths distances is nil")
	}

	if paths == nil {
		t.Error("ShortestPaths paths is nil")
	}
}

func TestGraphAdapter_PageRank(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	results := adapter.CalculatePageRank(0.85, 10)

	if len(results) != 3 {
		t.Errorf("Expected 3 PageRank results, got %d", len(results))
	}

	// All PageRank values should be positive
	for _, result := range results {
		if result.PageRank < 0 {
			t.Errorf("PageRank score should not be negative: %f", result.PageRank)
		}
	}
}

func TestGraphAdapter_DegreeCentrality(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "main", Kind: types.SymbolKindFunction, File: "main.go"},
		{ID: 2, Name: "helper", Kind: types.SymbolKindFunction, File: "helper.go"},
		{ID: 3, Name: "util", Kind: types.SymbolKindFunction, File: "util.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 1, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)
	results := adapter.CalculateDegreeCentrality()

	if len(results) != 3 {
		t.Errorf("Expected 3 centrality results, got %d", len(results))
	}
}

func TestGraphAdapter_ClusteringCoefficient(t *testing.T) {
	nodes := []*types.Node{
		{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
		{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
		{ID: 3, Name: "C", Kind: types.SymbolKindFunction, File: "c.go"},
	}
	edges := []*types.Edge{
		{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
		{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
	}

	adapter := NewGraphAdapter(nodes, edges)

	// Get clustering coefficient for each node
	for _, node := range nodes {
		var gonumID int64
		for gID, origID := range adapter.idMap {
			if origID == node.ID {
				gonumID = gID
				break
			}
		}

		coef := adapter.ClusteringCoefficient(gonumID)
		// Coefficient should be between 0 and 1
		if coef < 0 || coef > 1 {
			t.Errorf("Clustering coefficient should be between 0 and 1, got %f", coef)
		}
	}
}

func TestNodeWrapper(t *testing.T) {
	node := &types.Node{
		ID:   1,
		Name: "test",
		Kind: types.SymbolKindFunction,
		File: "test.go",
	}

	wrapper := &NodeWrapper{id: 42, node: node}

	if wrapper.ID() != 42 {
		t.Errorf("NodeWrapper.ID() = %d, want 42", wrapper.ID())
	}

	if wrapper.Node() != node {
		t.Error("NodeWrapper.Node() returned wrong node")
	}
}

func TestGraphAdapter_FindCycles(t *testing.T) {
	tests := []struct {
		name         string
		nodes        []*types.Node
		edges        []*types.Edge
		expectCycles bool
	}{
		{
			name: "no cycle",
			nodes: []*types.Node{
				{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
				{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
			},
			edges: []*types.Edge{
				{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
			},
			expectCycles: false,
		},
		{
			name: "simple cycle",
			nodes: []*types.Node{
				{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
				{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
			},
			edges: []*types.Edge{
				{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
				{ID: 2, SourceID: 2, TargetID: 1, Kind: types.EdgeKindCalls},
			},
			expectCycles: true,
		},
		{
			name: "three node cycle",
			nodes: []*types.Node{
				{ID: 1, Name: "A", Kind: types.SymbolKindFunction, File: "a.go"},
				{ID: 2, Name: "B", Kind: types.SymbolKindFunction, File: "b.go"},
				{ID: 3, Name: "C", Kind: types.SymbolKindFunction, File: "c.go"},
			},
			edges: []*types.Edge{
				{ID: 1, SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls},
				{ID: 2, SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls},
				{ID: 3, SourceID: 3, TargetID: 1, Kind: types.EdgeKindCalls},
			},
			expectCycles: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewGraphAdapter(tt.nodes, tt.edges)
			cycles := adapter.FindCycles()

			hasCycles := len(cycles) > 0
			if hasCycles != tt.expectCycles {
				t.Errorf("FindCycles() found %d cycles, expectCycles = %v", len(cycles), tt.expectCycles)
			}
		})
	}
}
