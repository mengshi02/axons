// Package stages provides pipeline stages for graph building.
package stages

import (
	"time"

	"github.com/mengshi02/axons/internal/algorithms"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// RunAnalyses runs optional analyses on the graph.
func RunAnalyses(ctx *PipelineContext) error {
	if ctx.EarlyExit {
		return nil
	}

	start := time.Now()
	defer func() {
		ctx.RecordTiming("analyses", time.Since(start))
	}()

	// Run Go interface implementation inference
	inferredCount := inferGoImplementations(ctx)
	if inferredCount > 0 {
		logger.Info("Inferred Go interface implementations",
			zap.Int("count", inferredCount),
		)
	}

	// Run community detection (Louvain) and store results to DB
	communities := runCommunityDetection(ctx)
	if communities != nil {
		logger.Info("Community detection completed",
			zap.Int("communityCount", len(communities)),
		)
	}

	logger.Info("Analyses completed",
		zap.Int("nodeCount", len(ctx.AllNodes)),
		zap.Int("parseResultCount", len(ctx.ParseResults)),
	)

	return nil
}

// runCommunityDetection runs Louvain community detection and stores results.
func runCommunityDetection(ctx *PipelineContext) []*algorithms.CommunityResult {
	// Need at least 2 nodes to detect communities
	if len(ctx.AllNodes) < 2 {
		return nil
	}

	// Need edges from DB — build adapter from AllNodes + DB edges
	// For incremental builds, we need all edges, not just new ones
	var allEdges []*types.Edge
	if ctx.Repo != nil {
		var err error
		allEdges, err = ctx.Repo.ListAllEdges()
		if err != nil {
			logger.Warn("Failed to list edges for community detection", zap.Error(err))
			return nil
		}
	}

	adapter := algorithms.NewGraphAdapter(ctx.AllNodes, allEdges)
	communities := adapter.LouvainCommunities(1.0)

	// Store community_id for each node in DB (batch update in a single transaction)
	if ctx.Repo != nil {
		nodeCommunityIDs := make(map[int64]int64, len(communities)*4) // estimate
		for _, comm := range communities {
			for _, nodeID := range comm.NodeIDs {
				nodeCommunityIDs[nodeID] = comm.ID
			}
		}
		if err := ctx.Repo.BatchSetNodeCommunity(nodeCommunityIDs); err != nil {
			logger.Debug("Failed to batch set node communities", zap.Error(err))
		}
	}

	return communities
}

// inferGoImplementations detects implicit Go interface implementations.
// In Go, a struct implicitly implements an interface if it has all the methods
// defined by that interface. This cross-file analysis cannot be done in the
// extractor (which is single-file), so we do it here.
func inferGoImplementations(ctx *PipelineContext) int {
	// Collect interfaces and their method sets
	type interfaceInfo struct {
		node    *types.Node
		methods map[string]bool // method name -> true
	}

	interfaces := make(map[string]*interfaceInfo) // qualified name -> info

	// Collect structs and their method sets
	structMethods := make(map[string]map[string]bool) // struct name -> method names

	// Build method index by qualified name prefix for O(1) lookup
	// instead of scanning all nodes for each interface
	methodByPrefix := make(map[string]map[string]bool) // prefix (e.g. "StructName") -> method names
	for _, node := range ctx.AllNodes {
		if node.Kind == types.SymbolKindMethod && node.QualifiedName != "" {
			// Extract prefix from QualifiedName: "ParentName.MethodName"
			for idx, ch := range node.QualifiedName {
				if ch == '.' {
					parentName := node.QualifiedName[:idx]
					if _, ok := methodByPrefix[parentName]; !ok {
						methodByPrefix[parentName] = make(map[string]bool)
					}
					methodByPrefix[parentName][node.Name] = true
					break
				}
			}
		}
	}

	// Build interface method sets using the index
	for _, node := range ctx.AllNodes {
		if node.Kind == types.SymbolKindInterface {
			methods := make(map[string]bool)
			// Use the prefix index for O(1) method lookup
			if m, ok := methodByPrefix[node.Name]; ok {
				for name := range m {
					methods[name] = true
				}
			}
			// Also check definitions with Parent info from extractor output
			for _, pr := range ctx.ParseResults {
				if pr.Output == nil {
					continue
				}
				for _, def := range pr.Output.Definitions {
					if def.Parent == node.Name && def.Kind == types.SymbolKindMethod {
						methods[def.Name] = true
					}
				}
			}
			if len(methods) > 0 {
				qname := node.QualifiedName
				if qname == "" {
					qname = node.Name
				}
				interfaces[qname] = &interfaceInfo{
					node:    node,
					methods: methods,
				}
			}
		}

		if node.Kind == types.SymbolKindStruct {
			if _, ok := structMethods[node.Name]; !ok {
				structMethods[node.Name] = methodByPrefix[node.Name]
			}
		}
	}

	if len(interfaces) == 0 {
		return 0
	}

	// For each struct, check if it satisfies any interface
	// Collect edges for batch insert instead of individual CreateEdge calls
	var implEdges []*types.Edge
	for structName, methods := range structMethods {
		if len(methods) == 0 {
			continue
		}
		for ifaceQName, iface := range interfaces {
			if structName == ifaceQName {
				continue // Skip self
			}

			// Check if struct implements all interface methods
			implements := true
			for method := range iface.methods {
				if !methods[method] {
					implements = false
					break
				}
			}

			if implements && len(iface.methods) > 0 {
				// Find struct node
				structNodes := ctx.NodesByName[structName]
				for _, sn := range structNodes {
					if sn.Kind != types.SymbolKindStruct {
						continue
					}
					implEdges = append(implEdges, &types.Edge{
						SourceID:   sn.ID,
						TargetID:   iface.node.ID,
						Kind:       types.EdgeKindImplements,
						Confidence: 0.8, // Inferred, not explicit
					})
				}
			}
		}
	}

	// Batch insert inferred implementation edges
	count := 0
	if len(implEdges) > 0 {
		inserted, err := ctx.Repo.BatchInsertEdges(implEdges)
		if err != nil {
			// Fallback to individual inserts
			for _, edge := range implEdges {
				if inserted, err := ctx.Repo.CreateEdge(edge); err == nil && inserted {
					count++
				}
			}
		} else {
			count = inserted
		}
	}

	return count
}