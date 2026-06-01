// Package algorithms provides code graph building and querying capabilities.
package algorithms

import (
	"math"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"

	"github.com/mengshi02/axons/pkg/types"
)

// GraphAdapter adapts our graph to gonum's graph.Directed interface.
type GraphAdapter struct {
	nodes      map[int64]*types.Node
	edges      map[int64][]*types.Edge
	gonumGraph *simple.DirectedGraph
	nodeMap    map[int64]graph.Node // maps our Node.ID to gonum node
	idMap      map[int64]int64      // maps gonum node ID to our Node.ID
}

// NodeWrapper wraps our Node to implement gonum's Node interface.
type NodeWrapper struct {
	id   int64
	node *types.Node
}

func (n *NodeWrapper) ID() int64 {
	return n.id
}

func (n *NodeWrapper) Node() *types.Node {
	return n.node
}

// NewGraphAdapter creates a new GraphAdapter from nodes and edges.
func NewGraphAdapter(nodes []*types.Node, edges []*types.Edge) *GraphAdapter {
	adapter := &GraphAdapter{
		nodes:      make(map[int64]*types.Node),
		edges:      make(map[int64][]*types.Edge),
		nodeMap:    make(map[int64]graph.Node),
		idMap:      make(map[int64]int64),
		gonumGraph: simple.NewDirectedGraph(),
	}

	// Add nodes
	for i, node := range nodes {
		adapter.nodes[node.ID] = node
		gonumID := int64(i + 1) // gonum uses positive IDs
		wrapper := &NodeWrapper{id: gonumID, node: node}
		adapter.gonumGraph.AddNode(wrapper)
		adapter.nodeMap[node.ID] = wrapper
		adapter.idMap[gonumID] = node.ID
	}

	// Add edges (skip self-loops: gonum simple.DirectedGraph does not allow them)
	for _, edge := range edges {
		adapter.edges[edge.SourceID] = append(adapter.edges[edge.SourceID], edge)
		if edge.SourceID == edge.TargetID {
			continue
		}
		if source, ok := adapter.nodeMap[edge.SourceID]; ok {
			if target, ok := adapter.nodeMap[edge.TargetID]; ok {
				adapter.gonumGraph.SetEdge(simple.Edge{F: source, T: target})
			}
		}
	}

	return adapter
}

// GonumGraph returns the underlying gonum graph.
func (ga *GraphAdapter) GonumGraph() graph.Directed {
	return ga.gonumGraph
}

// TarjanSCC uses gonum's Tarjan implementation to find strongly connected components.
func (ga *GraphAdapter) TarjanSCC() [][]int64 {
	sccs := topo.TarjanSCC(ga.gonumGraph)
	result := make([][]int64, len(sccs))

	for i, scc := range sccs {
		ids := make([]int64, len(scc))
		for j, node := range scc {
			gonumID := node.ID()
			if originalID, ok := ga.idMap[gonumID]; ok {
				ids[j] = originalID
			}
		}
		result[i] = ids
	}

	return result
}

// SCCInfo contains information about strongly connected components.
type SCCInfo struct {
	Components     [][]int64       // List of SCCs, each is a list of node IDs
	ComponentNodes [][]*types.Node // Same but with Node objects
	ComponentSizes []int           // Size of each component
	LargestSCC     int             // Index of largest component
	CyclicNodes    map[int64]bool  // Nodes that are part of cycles (SCCs > 1)
}

// GetSCCInfo returns detailed information about SCCs using gonum.
func (ga *GraphAdapter) GetSCCInfo() *SCCInfo {
	components := ga.TarjanSCC()

	info := &SCCInfo{
		Components:     components,
		ComponentNodes: make([][]*types.Node, len(components)),
		ComponentSizes: make([]int, len(components)),
		CyclicNodes:    make(map[int64]bool),
	}

	largestSize := 0
	largestIdx := 0

	for i, component := range components {
		info.ComponentSizes[i] = len(component)
		if len(component) > largestSize {
			largestSize = len(component)
			largestIdx = i
		}

		nodes := make([]*types.Node, len(component))
		for j, nodeID := range component {
			if node, ok := ga.nodes[nodeID]; ok {
				nodes[j] = node
			}
		}
		info.ComponentNodes[i] = nodes

		// Mark nodes in cycles (SCCs with more than one node)
		if len(component) > 1 {
			for _, nodeID := range component {
				info.CyclicNodes[nodeID] = true
			}
		}
	}

	info.LargestSCC = largestIdx
	return info
}

// CommunityResult represents a detected community.
type CommunityResult struct {
	ID         int64         // Community identifier
	Nodes      []*types.Node // Nodes in this community
	NodeIDs    []int64       // Node IDs in this community
	Size       int           // Number of nodes
	Density    float64       // Internal edge density
	Modularity float64       // Modularity contribution
}

// LouvainCommunities uses gonum's Modularize for community detection.
func (ga *GraphAdapter) LouvainCommunities(resolution float64) []*CommunityResult {
	// Create undirected version for community detection
	undirected := simple.NewUndirectedGraph()
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		undirected.AddNode(node)
	}
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		it := ga.gonumGraph.From(node.ID())
		for it.Next() {
			to := it.Node()
			undirected.SetEdge(simple.Edge{F: node, T: to})
		}
	}

	// Run modularization (similar to Louvain)
	reduced := community.Modularize(undirected, resolution, nil)

	// Extract communities from reduced graph
	communities := reduced.Communities()

	// Compute overall modularity ONCE (not per community)
	overallModularity := community.Q(undirected, communities, resolution)

	// Build result
	result := make([]*CommunityResult, 0, len(communities))
	for commIdx, commNodes := range communities {
		nodeIDs := make([]int64, 0, len(commNodes))
		nodes := make([]*types.Node, 0, len(commNodes))

		for _, commNode := range commNodes {
			if origID, ok := ga.idMap[commNode.ID()]; ok {
				nodeIDs = append(nodeIDs, origID)
				if n, exists := ga.nodes[origID]; exists {
					nodes = append(nodes, n)
				}
			}
		}

		if len(nodeIDs) > 0 {
			result = append(result, &CommunityResult{
				ID:         int64(commIdx),
				Nodes:      nodes,
				NodeIDs:    nodeIDs,
				Size:       len(nodeIDs),
				Density:    calculateDensity(ga, nodeIDs),
				Modularity: overallModularity,
			})
		}
	}

	return result
}

// calculateDensity calculates internal edge density for a directed graph.
// For directed graphs, max possible edges = n*(n-1), and we count all
// directed internal edges (source->target where both are in the community).
func calculateDensity(ga *GraphAdapter, nodeIDs []int64) float64 {
	if len(nodeIDs) < 2 {
		return 0.0
	}

	nodeSet := make(map[int64]bool)
	for _, id := range nodeIDs {
		nodeSet[id] = true
	}

	internalEdges := 0
	for _, id := range nodeIDs {
		for _, edge := range ga.edges[id] {
			if nodeSet[edge.TargetID] {
				internalEdges++
			}
		}
	}

	// For directed graph: max possible edges = n*(n-1)
	maxPossibleEdges := len(nodeIDs) * (len(nodeIDs) - 1)
	return float64(internalEdges) / float64(maxPossibleEdges)
}

// ShortestPathResult represents a shortest path result.
type ShortestPathResult struct {
	From   int64
	To     int64
	Path   []int64
	Length int
}

// ShortestPaths uses gonum's Dijkstra algorithm for shortest paths.
func (ga *GraphAdapter) ShortestPaths(fromID int64) (map[int64]float64, map[int64][]int64) {
	from, ok := ga.nodeMap[fromID]
	if !ok {
		return nil, nil
	}

	paths := path.DijkstraFrom(from, ga.gonumGraph)

	distances := make(map[int64]float64)
	pathsMap := make(map[int64][]int64)

	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		gonumID := node.ID()
		if originalID, ok := ga.idMap[gonumID]; ok {
			dist := paths.WeightTo(node.ID())
			distances[originalID] = dist

			pathNodes, _ := paths.To(node.ID())
			if len(pathNodes) > 0 {
				nodePath := make([]int64, len(pathNodes))
				for i, p := range pathNodes {
					if origID, exists := ga.idMap[p.ID()]; exists {
						nodePath[i] = origID
					}
				}
				pathsMap[originalID] = nodePath
			}
		}
	}

	return distances, pathsMap
}

// AllShortestPaths computes all-pairs shortest paths.
func (ga *GraphAdapter) AllShortestPaths() (map[int64]map[int64]float64, error) {
	allPaths, ok := path.FloydWarshall(ga.gonumGraph)
	if !ok {
		return nil, nil
	}

	distances := make(map[int64]map[int64]float64)

	for _, from := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		fromID := from.ID()
		fromOrigID, fromOK := ga.idMap[fromID]
		if !fromOK {
			continue
		}

		distances[fromOrigID] = make(map[int64]float64)

		for _, to := range graph.NodesOf(ga.gonumGraph.Nodes()) {
			toID := to.ID()
			toOrigID, toOK := ga.idMap[toID]
			if !toOK {
				continue
			}

			dist := allPaths.Weight(fromID, toID)
			distances[fromOrigID][toOrigID] = dist
		}
	}

	return distances, nil
}

// CycleResult represents a detected cycle.
type CycleResult struct {
	Nodes []*types.Node
	Path  []int64
}

// FindCycles detects all cycles in the graph using gonum.
func (ga *GraphAdapter) FindCycles() []*CycleResult {
	// Use Tarjan SCC to find cycles
	sccs := topo.TarjanSCC(ga.gonumGraph)

	cycles := make([]*CycleResult, 0)
	for _, scc := range sccs {
		if len(scc) > 1 {
			// SCC with more than one node contains cycles
			ids := make([]int64, len(scc))
			nodes := make([]*types.Node, len(scc))
			for i, node := range scc {
				gonumID := node.ID()
				if originalID, ok := ga.idMap[gonumID]; ok {
					ids[i] = originalID
					if n, exists := ga.nodes[originalID]; exists {
						nodes[i] = n
					}
				}
			}
			cycles = append(cycles, &CycleResult{
				Nodes: nodes,
				Path:  ids,
			})
		}
	}

	return cycles
}

// TopologicalOrder returns a topological ordering of the graph.
// Returns nil if the graph contains cycles.
func (ga *GraphAdapter) TopologicalOrder() []int64 {
	order, err := topo.Sort(ga.gonumGraph)
	if err != nil {
		return nil // Graph has cycles
	}

	result := make([]int64, len(order))
	for i, node := range order {
		gonumID := node.ID()
		if originalID, ok := ga.idMap[gonumID]; ok {
			result[i] = originalID
		}
	}

	return result
}

// ConnectedComponents returns weakly connected components.
// For directed graphs, we convert to undirected for component analysis.
func (ga *GraphAdapter) ConnectedComponents() [][]int64 {
	// Create undirected version for connected components
	undirected := simple.NewUndirectedGraph()
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		undirected.AddNode(node)
	}
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		it := ga.gonumGraph.From(node.ID())
		for it.Next() {
			to := it.Node()
			undirected.SetEdge(simple.Edge{F: node, T: to})
		}
	}

	components := topo.ConnectedComponents(undirected)

	result := make([][]int64, len(components))
	for i, component := range components {
		ids := make([]int64, len(component))
		for j, node := range component {
			gonumID := node.ID()
			if originalID, ok := ga.idMap[gonumID]; ok {
				ids[j] = originalID
			}
		}
		result[i] = ids
	}

	return result
}

// IsDAG checks if the graph is a directed acyclic graph.
func (ga *GraphAdapter) IsDAG() bool {
	_, err := topo.Sort(ga.gonumGraph)
	return err == nil
}

// GraphMetrics contains various graph metrics.
type GraphMetrics struct {
	TotalNodes     int     `json:"total_nodes"`
	TotalEdges     int     `json:"total_edges"`
	AvgInDegree    float64 `json:"avg_in_degree"`
	AvgOutDegree   float64 `json:"avg_out_degree"`
	MaxInDegree    int     `json:"max_in_degree"`
	MaxOutDegree   int     `json:"max_out_degree"`
	Density        float64 `json:"density"`
	NumSCCs        int     `json:"num_sccs"`
	LargestSCCSize int     `json:"largest_scc_size"`
	NumCommunities int     `json:"num_communities"`
	Modularity     float64 `json:"modularity"`
	IsDAG          bool    `json:"is_dag"`
	AvgClustering  float64 `json:"avg_clustering"`
	Diameter       int     `json:"diameter"`
	AvgPathLength  float64 `json:"avg_path_length"`
}

// CalculateMetrics calculates comprehensive graph metrics using gonum.
func (ga *GraphAdapter) CalculateMetrics() *GraphMetrics {
	metrics := &GraphMetrics{
		TotalNodes: len(ga.nodes),
	}

	// Count edges
	totalEdges := 0
	inDegree := make(map[int64]int)
	outDegree := make(map[int64]int)

	for sourceID, edges := range ga.edges {
		outDegree[sourceID] = len(edges)
		totalEdges += len(edges)
		for _, edge := range edges {
			inDegree[edge.TargetID]++
		}
	}

	metrics.TotalEdges = totalEdges

	// Calculate average degree
	if metrics.TotalNodes > 0 {
		metrics.AvgInDegree = float64(totalEdges) / float64(metrics.TotalNodes)
		metrics.AvgOutDegree = float64(totalEdges) / float64(metrics.TotalNodes)
	}

	// Find max degrees
	for _, degree := range inDegree {
		if degree > metrics.MaxInDegree {
			metrics.MaxInDegree = degree
		}
	}
	for _, degree := range outDegree {
		if degree > metrics.MaxOutDegree {
			metrics.MaxOutDegree = degree
		}
	}

	// Calculate density
	maxPossibleEdges := metrics.TotalNodes * (metrics.TotalNodes - 1)
	if maxPossibleEdges > 0 {
		metrics.Density = float64(totalEdges) / float64(maxPossibleEdges)
	}

	// SCC analysis
	sccs := ga.TarjanSCC()
	metrics.NumSCCs = len(sccs)
	for _, scc := range sccs {
		if len(scc) > metrics.LargestSCCSize {
			metrics.LargestSCCSize = len(scc)
		}
	}

	// Community detection
	communities := ga.LouvainCommunities(1.0)
	metrics.NumCommunities = len(communities)

	// Calculate modularity using undirected graph
	undirected := simple.NewUndirectedGraph()
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		undirected.AddNode(node)
	}
	for _, node := range graph.NodesOf(ga.gonumGraph.Nodes()) {
		it := ga.gonumGraph.From(node.ID())
		for it.Next() {
			to := it.Node()
			undirected.SetEdge(simple.Edge{F: node, T: to})
		}
	}
	// Build communities for modularity calculation
	communitiesForQ := make([][]graph.Node, 0, len(communities))
	for _, comm := range communities {
		commNodes := make([]graph.Node, 0, len(comm.NodeIDs))
		for _, nodeID := range comm.NodeIDs {
			if gn, ok := ga.nodeMap[nodeID]; ok {
				commNodes = append(commNodes, gn)
			}
		}
		if len(commNodes) > 0 {
			communitiesForQ = append(communitiesForQ, commNodes)
		}
	}
	metrics.Modularity = community.Q(undirected, communitiesForQ, 1.0)

	// Check if DAG
	metrics.IsDAG = ga.IsDAG()

	return metrics
}

// PageRankResult represents PageRank score for a node.
type PageRankResult struct {
	NodeID   int64
	Node     *types.Node
	PageRank float64
}

// CalculatePageRank calculates PageRank using random walk.
func (ga *GraphAdapter) CalculatePageRank(dampingFactor float64, iterations int) []*PageRankResult {
	n := len(ga.nodes)
	if n == 0 {
		return []*PageRankResult{}
	}

	// Initialize PageRank
	pageRank := make(map[int64]float64)
	for nodeID := range ga.nodes {
		pageRank[nodeID] = 1.0 / float64(n)
	}

	// Build in-edge map and out-degree
	inEdges := make(map[int64][]int64)
	outDegree := make(map[int64]int)

	for sourceID, edges := range ga.edges {
		outDegree[sourceID] = len(edges)
		for _, edge := range edges {
			inEdges[edge.TargetID] = append(inEdges[edge.TargetID], sourceID)
		}
	}

	// Iterate
	for i := 0; i < iterations; i++ {
		newRank := make(map[int64]float64)
		danglingSum := 0.0

		// Calculate dangling node contribution
		for nodeID := range ga.nodes {
			if outDegree[nodeID] == 0 {
				danglingSum += pageRank[nodeID]
			}
		}

		for nodeID := range ga.nodes {
			rank := (1.0 - dampingFactor) / float64(n)

			// Contribution from incoming edges
			for _, sourceID := range inEdges[nodeID] {
				if outDegree[sourceID] > 0 {
					rank += dampingFactor * pageRank[sourceID] / float64(outDegree[sourceID])
				}
			}

			// Contribution from dangling nodes
			rank += dampingFactor * danglingSum / float64(n)

			newRank[nodeID] = rank
		}

		pageRank = newRank
	}

	results := make([]*PageRankResult, 0, n)
	for nodeID, node := range ga.nodes {
		results = append(results, &PageRankResult{
			NodeID:   nodeID,
			Node:     node,
			PageRank: pageRank[nodeID],
		})
	}

	return results
}

// CentralityResult represents centrality metrics for a node.
type CentralityResult struct {
	NodeID                int64
	Node                  *types.Node
	DegreeCentrality      float64
	BetweennessCentrality float64
	ClosenessCentrality   float64
}

// CalculateDegreeCentrality calculates degree centrality for all nodes.
func (ga *GraphAdapter) CalculateDegreeCentrality() []*CentralityResult {
	results := make([]*CentralityResult, 0, len(ga.nodes))
	n := float64(len(ga.nodes))

	// Count in-degree and out-degree
	inDegree := make(map[int64]int)
	outDegree := make(map[int64]int)

	for sourceID, edges := range ga.edges {
		outDegree[sourceID] = len(edges)
		for _, edge := range edges {
			inDegree[edge.TargetID]++
		}
	}

	for nodeID, node := range ga.nodes {
		total := inDegree[nodeID] + outDegree[nodeID]
		results = append(results, &CentralityResult{
			NodeID:           nodeID,
			Node:             node,
			DegreeCentrality: float64(total) / (n - 1),
		})
	}

	return results
}

// BetweennessCentrality calculates betweenness centrality using Brandes' algorithm.
// For each node v, betweenness is defined as the sum over all source-target pairs (s,t)
// of the fraction of shortest paths from s to t that pass through v.
func (ga *GraphAdapter) BetweennessCentrality() map[int64]float64 {
	betweenness := make(map[int64]float64)
	for nodeID := range ga.nodes {
		betweenness[nodeID] = 0.0
	}

	// Brandes' algorithm
	for sourceID := range ga.nodes {
		_, ok := ga.nodeMap[sourceID]
		if !ok {
			continue
		}

		// BFS from source
		stack := make([]int64, 0)          // vertices in non-increasing order of distance
		predecessors := make(map[int64][]int64) // predecessors on shortest paths
		sigma := make(map[int64]float64)       // number of shortest paths
		dist := make(map[int64]float64)        // distance from source

		for nodeID := range ga.nodes {
			sigma[nodeID] = 0
			dist[nodeID] = -1
			predecessors[nodeID] = nil
		}
		sigma[sourceID] = 1
		dist[sourceID] = 0

		queue := []int64{sourceID}

		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)

			// Iterate over neighbors of v (outgoing edges in directed graph)
			for _, edge := range ga.edges[v] {
				w := edge.TargetID
				// First visit?
				if dist[w] < 0 {
					queue = append(queue, w)
					dist[w] = dist[v] + 1
				}
				// Shortest path to w via v?
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					predecessors[w] = append(predecessors[w], v)
				}
			}
		}

		// Back-propagation of dependencies
		delta := make(map[int64]float64)
		for nodeID := range ga.nodes {
			delta[nodeID] = 0
		}

		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range predecessors[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != sourceID {
				betweenness[w] += delta[w]
			}
		}
	}

	// Normalize for directed graph
	n := float64(len(ga.nodes))
	normalization := (n - 1) * (n - 2)
	if normalization <= 0 {
		normalization = 1
	}

	for id := range betweenness {
		betweenness[id] /= normalization
	}

	return betweenness
}

// ClusteringCoefficient calculates the clustering coefficient for a node
// in a directed graph. For directed graphs, we treat the neighborhood as
// undirected and count all directed edges between neighbors. The denominator
// for a directed graph is k*(k-1)*2 because each pair can have two directed edges.
func (ga *GraphAdapter) ClusteringCoefficient(nodeID int64) float64 {
	_, ok := ga.nodeMap[nodeID]
	if !ok {
		return 0.0
	}

	neighborSet := make(map[int64]bool)

	// Get neighbors (both outgoing and incoming for undirected neighborhood)
	for _, edge := range ga.edges[nodeID] {
		neighborSet[edge.TargetID] = true
	}

	// Add incoming neighbors
	for sourceID, edges := range ga.edges {
		if sourceID == nodeID {
			continue
		}
		for _, edge := range edges {
			if edge.TargetID == nodeID {
				neighborSet[sourceID] = true
			}
		}
	}

	k := len(neighborSet)
	if k < 2 {
		return 0.0
	}

	// Count directed edges between neighbors (both directions count)
	edgesBetweenNeighbors := 0
	for n1 := range neighborSet {
		for n2 := range neighborSet {
			if n1 >= n2 {
				continue
			}
			// Check if there's an edge between n1 and n2
			for _, edge := range ga.edges[n1] {
				if edge.TargetID == n2 {
					edgesBetweenNeighbors++
				}
			}
			for _, edge := range ga.edges[n2] {
				if edge.TargetID == n1 {
					edgesBetweenNeighbors++
				}
			}
		}
	}

	// For directed graph: each pair (n1, n2) can have up to 2 directed edges,
	// so the maximum is k*(k-1)*2 (not k*(k-1) which is for undirected graphs)
	return float64(edgesBetweenNeighbors) / float64(k*(k-1)*2)
}

// AverageClusteringCoefficient calculates the average clustering coefficient.
func (ga *GraphAdapter) AverageClusteringCoefficient() float64 {
	if len(ga.nodes) == 0 {
		return 0.0
	}

	total := 0.0
	count := 0

	for nodeID := range ga.nodes {
		total += ga.ClusteringCoefficient(nodeID)
		count++
	}

	if count == 0 {
		return 0.0
	}
	return total / float64(count)
}

// GraphDiameter calculates the diameter of the graph.
func (ga *GraphAdapter) GraphDiameter() int {
	// Use Floyd-Warshall for all-pairs shortest paths
	distances, err := ga.AllShortestPaths()
	if err != nil {
		return 0
	}

	maxDist := 0.0
	for _, toMap := range distances {
		for _, dist := range toMap {
			if dist > 0 && dist > maxDist && !math.IsInf(dist, 1) {
				maxDist = dist
			}
		}
	}

	return int(math.Ceil(maxDist))
}

// AveragePathLength calculates the average shortest path length.
func (ga *GraphAdapter) AveragePathLength() float64 {
	distances, err := ga.AllShortestPaths()
	if err != nil {
		return 0.0
	}

	totalDist := 0.0
	pathCount := 0

	for _, toMap := range distances {
		for _, dist := range toMap {
			if dist > 0 && !math.IsInf(dist, 1) {
				totalDist += dist
				pathCount++
			}
		}
	}

	if pathCount == 0 {
		return 0.0
	}
	return float64(totalDist) / float64(pathCount)
}
