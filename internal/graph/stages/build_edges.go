// Package stages provides pipeline stages for graph building.
package stages

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// BuildEdges builds edges between nodes.
func BuildEdges(ctx *PipelineContext) error {
	if ctx.EarlyExit {
		return nil
	}

	start := time.Now()
	defer func() {
		ctx.RecordTiming("edges", time.Since(start))
	}()

	totalImports := 0
	totalCalls := 0
	totalClasses := 0
	totalDefinitions := 0

	// Collect all edges across files before batch-inserting.
	// This replaces individual CreateEdge calls with a single transaction,
	// reducing 25000+ fsyncs to just 1 — the primary performance bottleneck.
	var allEdges []*types.Edge

	// Build edges for changed/new files
	for _, pr := range ctx.ParseResults {
		if pr.Err != nil || pr.Output == nil {
			continue
		}
		totalImports += len(pr.Output.Imports)
		totalCalls += len(pr.Output.Calls)
		totalClasses += len(pr.Output.Classes)
		totalDefinitions += len(pr.Output.Definitions)
		edges := buildEdgesForFile(ctx, &pr)
		for _, edge := range edges {
			if edge.SourceID == 0 || edge.TargetID == 0 {
				logger.Debug("Skipping edge with zero ID",
					zap.Int64("source", edge.SourceID),
					zap.Int64("target", edge.TargetID),
					zap.String("kind", string(edge.Kind)),
				)
				continue
			}
			allEdges = append(allEdges, edge)
		}
		logger.Debug("Edges for changed file",
			zap.String("file", ToRelativePath(pr.FilePath, ctx.Opts.RootDir)),
			zap.Int("totalEdges", len(edges)),
		)
	}

	// For incremental builds, also rebuild edges for existing files that reference
	// nodes from changed files. When a file changes, other files that call functions
	// defined in the changed file need their edges rebuilt.
	if !ctx.IsFullBuild {
		affectedEdges := rebuildEdgesForAffectedFiles(ctx)
		allEdges = append(allEdges, affectedEdges...)
	}

	// Batch insert all edges in a single transaction
	edgeCount := 0
	ignoredEdgeCount := 0
	if len(allEdges) > 0 {
		inserted, err := ctx.Repo.BatchInsertEdges(allEdges)
		if err != nil {
			logger.Warn("Batch edge insert failed, falling back to individual inserts",
				zap.Error(err),
			)
			// Fallback: insert edges one by one
			for _, edge := range allEdges {
				if inserted, err := ctx.Repo.CreateEdge(edge); err != nil {
					logger.Debug("Failed to create edge",
						zap.Int64("source", edge.SourceID),
						zap.Int64("target", edge.TargetID),
						zap.Error(err),
					)
				} else if inserted {
					edgeCount++
					ctx.AllEdges = append(ctx.AllEdges, edge)
				} else {
					ignoredEdgeCount++
				}
			}
		} else {
			edgeCount = inserted
			ignoredEdgeCount = len(allEdges) - inserted
			ctx.AllEdges = append(ctx.AllEdges, allEdges...)
		}
	}

	logger.Info("Built edges",
		zap.Int("count", edgeCount),
		zap.Int("ignoredAlreadyExists", ignoredEdgeCount),
		zap.Int("totalImports", totalImports),
		zap.Int("totalCalls", totalCalls),
		zap.Int("totalClasses", totalClasses),
		zap.Int("totalDefinitions", totalDefinitions),
		zap.Int("totalNodes", len(ctx.AllNodes)),
		zap.Int("totalFiles", len(ctx.ParseResults)),
		zap.Int("filesWithNodes", len(ctx.NodesByFile)),
	)
	return nil
}

// buildEdgesForFile builds edges for a single file.
func buildEdgesForFile(ctx *PipelineContext, pr *ParseResult) []*types.Edge {
	var edges []*types.Edge

	if pr.Output == nil {
		return edges
	}

	relativePath := ToRelativePath(pr.FilePath, ctx.Opts.RootDir)
	output := pr.Output
	nodesInFile := ctx.NodesByFile[relativePath]
	
	if len(nodesInFile) == 0 {
		// No nodes found for this file, skip edge building
		logger.Debug("No nodes found for file",
			zap.String("file", relativePath),
			zap.String("originalPath", pr.FilePath),
			zap.Int("availableFiles", len(ctx.NodesByFile)),
		)
		// Debug: try to find the file with different path formats
		for key := range ctx.NodesByFile {
			if strings.HasSuffix(key, filepath.Base(relativePath)) {
				logger.Debug("Possible path mismatch - found similar file",
					zap.String("expected", relativePath),
					zap.String("found", key),
				)
			}
		}
		return edges
	}

	// Build call edges
	for _, call := range output.Calls {
		// Find caller node: look for the function/method that contains this call line
		var callerID int64
		for _, node := range nodesInFile {
			if node.Line <= call.Line && (node.EndLine == 0 || node.EndLine >= call.Line) {
				if node.Kind == types.SymbolKindFunction || node.Kind == types.SymbolKindMethod {
					callerID = node.ID
					break
				}
			}
		}

		// Fallback: if no function/method encloses the call, try class/struct/interface as caller
		if callerID == 0 {
			for _, node := range nodesInFile {
				if node.Line <= call.Line && (node.EndLine == 0 || node.EndLine >= call.Line) {
					if node.Kind == types.SymbolKindClass || node.Kind == types.SymbolKindStruct || node.Kind == types.SymbolKindInterface {
						callerID = node.ID
						break
					}
				}
			}
		}

		// Resolve target
		targets := resolveCallTarget(call, relativePath, nodesInFile, ctx.NodesByName, ctx.NodesByQualified)
		if len(targets) == 0 {
			continue
		}

		// Create edges to all targets
		for _, target := range targets {
			edge := &types.Edge{
				SourceID:   callerID,
				TargetID:   target.ID,
				Kind:       types.EdgeKindCalls,
				Confidence: 1.0,
				Dynamic:    call.IsDynamic,
			}
			edges = append(edges, edge)
		}
	}

	// Build class hierarchy edges
	for _, class := range output.Classes {
		classNodes := ctx.NodesByName[class.ClassName]
		var classID int64
		for _, cn := range classNodes {
			if cn.File == relativePath && (cn.Kind == types.SymbolKindClass || cn.Kind == types.SymbolKindInterface || cn.Kind == types.SymbolKindStruct) {
				classID = cn.ID
				break
			}
		}

		if classID == 0 {
			continue
		}

		// Create extends edge
		if class.Extends != "" {
			parentNodes := ctx.NodesByName[class.Extends]
			if best := findBestMatchByFile(parentNodes, relativePath, typeFilterClassish); best != nil {
				edges = append(edges, &types.Edge{
					SourceID:   classID,
					TargetID:   best.ID,
					Kind:       types.EdgeKindExtends,
					Confidence: 1.0,
				})
			}
		}

		// Create implements edges
		for _, iface := range class.Implements {
			ifaceNodes := ctx.NodesByName[iface]
			if best := findBestMatchByFile(ifaceNodes, relativePath, typeFilterInterface); best != nil {
				edges = append(edges, &types.Edge{
					SourceID:   classID,
					TargetID:   best.ID,
					Kind:       types.EdgeKindImplements,
					Confidence: 1.0,
				})
			}
		}
	}

	// Build contains edges (parent -> child relationships)
	for _, def := range output.Definitions {
		if def.Parent == "" {
			continue
		}
		var childID int64
		for _, node := range nodesInFile {
			if node.Name == def.Name && node.Line == def.Line {
				childID = node.ID
				break
			}
		}
		if childID == 0 {
			continue
		}

		parentNodes := ctx.NodesByName[def.Parent]
		for _, parentNode := range parentNodes {
			if parentNode.File == relativePath {
				edges = append(edges, &types.Edge{
					SourceID:   parentNode.ID,
					TargetID:   childID,
					Kind:       types.EdgeKindContains,
					Confidence: 1.0,
				})
				break
			}
		}
	}

	// Build imports edges
	importEdgeCount := 0
	for _, imp := range output.Imports {
		targetPath := resolveImportPath(ctx, imp.Source, relativePath)
		if targetPath == "" {
			continue
		}

		targetNodes := ctx.NodesByFile[targetPath]
		if len(targetNodes) == 0 {
			targetNodes = ctx.NodesByDir[targetPath]
		}
		if len(targetNodes) == 0 {
			continue
		}

		edgeKind := types.EdgeKindImports
		if imp.IsType {
			edgeKind = types.EdgeKindImportsType
		}

		if len(imp.Symbols) > 0 {
			// Named imports: create edges only for explicitly imported symbols.
			// For each symbol, find callers in this file that actually reference it
			// to avoid creating edges for every function in the file.
			for _, symbol := range imp.Symbols {
				for _, targetNode := range targetNodes {
					if targetNode.Name == symbol && targetNode.Exported {
						// Find the most specific caller that references this symbol
						importerID := findBestImporterForSymbol(nodesInFile, symbol, output)
						if importerID != 0 {
							edges = append(edges, &types.Edge{
								SourceID:   importerID,
								TargetID:   targetNode.ID,
								Kind:       edgeKind,
								Confidence: 0.9,
							})
							importEdgeCount++
						}
						break
					}
				}
			}
		} else {
			// Wildcard/star imports: only create edges for symbols that are actually
			// used (referenced via calls) in this file, instead of creating N×M edges
			// for every exported symbol × every function.
			usedSymbols := collectUsedSymbolNames(output)
			for _, targetNode := range targetNodes {
				if targetNode.Exported && usedSymbols[targetNode.Name] {
					importerID := findBestImporterForSymbol(nodesInFile, targetNode.Name, output)
					if importerID != 0 {
						edges = append(edges, &types.Edge{
							SourceID:   importerID,
							TargetID:   targetNode.ID,
							Kind:       edgeKind,
							Confidence: 0.7,
						})
						importEdgeCount++
					}
				}
			}
		}
	}

	if importEdgeCount > 0 {
		logger.Debug("Import edges created",
			zap.String("file", relativePath),
			zap.Int("count", importEdgeCount),
		)
	}

	return edges
}

// resolveCallTarget resolves a call to target nodes.
// It prefers same-file and same-package matches over distant matches,
// and limits the number of returned targets to prevent edge explosion.
func resolveCallTarget(call types.Call, filePath string, nodesInFile []*types.Node, nodesByName map[string][]*types.Node, nodesByQualified map[string][]*types.Node) []*types.Node {
	const maxTargets = 3 // Limit targets to prevent edge explosion from common names

	// 1. Check same file definitions first (highest confidence)
	for _, node := range nodesInFile {
		if node.Name == call.Name {
			return []*types.Node{node}
		}
	}

	// 2. Check by receiver for method calls (exact qualified match)
	if call.IsMethod && call.Receiver != "" {
		expectedQualified := call.Receiver + "." + call.Name
		if nodes, ok := nodesByQualified[expectedQualified]; ok && len(nodes) > 0 {
			return []*types.Node{nodes[0]}
		}
	}

	// 3. Global search — prefer same-package, then exported, with limit
	nodes := nodesByName[call.Name]
	if len(nodes) == 0 {
		return nil
	}

	sourceDir := filepath.Dir(filePath)

	// Tier A: Same directory (same package) matches — highest priority
	var sameDirNodes []*types.Node
	// Tier B: Exported/public nodes from other packages
	var exportedNodes []*types.Node
	// Tier C: Any remaining matches (last resort)
	var otherNodes []*types.Node

	for _, node := range nodes {
		if filepath.Dir(node.File) == sourceDir {
			sameDirNodes = append(sameDirNodes, node)
		} else if node.Exported || node.Visibility == types.VisibilityPublic {
			exportedNodes = append(exportedNodes, node)
		} else {
			otherNodes = append(otherNodes, node)
		}
	}

	// Build result with priority ordering, capped at maxTargets
	var result []*types.Node
	for _, list := range [][]*types.Node{sameDirNodes, exportedNodes, otherNodes} {
		for _, node := range list {
			result = append(result, node)
			if len(result) >= maxTargets {
				return result
			}
		}
	}

	if len(result) > 0 {
		return result
	}
	return nil
}

// resolveImportPath resolves an import source path to a relative file path.
func resolveImportPath(ctx *PipelineContext, importSource, fromFile string) string {
	if importSource == "" {
		return ""
	}

	importPath := filepath.ToSlash(importSource)

	// Handle relative imports
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		fromDir := filepath.Dir(fromFile)
		parts := strings.Split(fromDir, "/")
		importParts := strings.Split(importPath, "/")

		for _, part := range importParts {
			if part == "." {
				continue
			}
			if part == ".." {
				if len(parts) > 0 {
					parts = parts[:len(parts)-1]
				}
			} else {
				parts = append(parts, part)
			}
		}

		basePath := strings.Join(parts, "/")
		if ctx.SuffixIndex != nil {
			if resolved := TryResolveWithExtensions(ctx.SuffixIndex, basePath); resolved != "" {
				return resolved
			}
		}
		return basePath
	}

	// Try suffix matching
	if ctx.SuffixIndex != nil {
		parts := strings.Split(importPath, "/")
		if resolved := SuffixResolve(ctx.SuffixIndex, parts); resolved != "" {
			return resolved
		}
	}

	// For C/C++ includes without explicit relative paths (e.g., #include "ngx_core.h"),
	// try resolving relative to the importing file's directory first.
	// This handles the common pattern where headers are in the same directory as the source.
	if !strings.Contains(importPath, "/") && ctx.SuffixIndex != nil {
		fromDir := filepath.Dir(fromFile)
		candidate := fromDir + "/" + importPath
		if resolved := TryResolveWithExtensions(ctx.SuffixIndex, candidate); resolved != "" {
			return resolved
		}
	}

	return ""
}

// rebuildEdgesForAffectedFiles rebuilds edges for existing files that reference
// nodes from changed/new files. This handles two scenarios:
// 1. An existing file calls a newly added function (new edge needed)
// 2. An existing file had edges to old nodes that were deleted (edges need recreation with new node IDs)
//
// The approach: find files that import changed files, or that had edges pointing
// to nodes in changed files, then re-parse and rebuild their edges.
// Returns the list of rebuilt edges (to be batch-inserted by the caller).
func rebuildEdgesForAffectedFiles(ctx *PipelineContext) []*types.Edge {
	// Collect changed file relative paths
	changedFiles := make(map[string]bool)
	for _, fc := range ctx.ParseChanges {
		changedFiles[ToRelativePath(fc.Path, ctx.Opts.RootDir)] = true
	}
	if len(changedFiles) == 0 {
		return nil
	}

	// Find affected files using two strategies:
	// 1. Files that import the same directory/package as changed files
	// 2. Files that had edges pointing to changed-file nodes (before they were deleted)
	affectedFiles := make(map[string]bool)

	// Strategy 1: Find files that import the same packages/directories as changed files.
	// If a changed file is in package X, other files that import X may need edge updates.
	changedDirs := make(map[string]bool)
	for f := range changedFiles {
		dir := filepath.Dir(f)
		changedDirs[dir] = true
	}
	// Find files in the NodesByDir index that share directories with changed files
	// These are files in the same package that might reference each other
	for dir := range changedDirs {
		if nodes, ok := ctx.NodesByDir[dir]; ok {
			for _, node := range nodes {
				if !changedFiles[node.File] {
					affectedFiles[node.File] = true
				}
			}
		}
	}

	// Strategy 2: For exported/public nodes in changed files, find files that
	// call them by name. We restrict this to files in the same package or
	// files that import the changed file's package, to avoid over-expansion
	// from common names like "New", "Init", "Handle" appearing in hundreds of files.
	changedDirsSet := make(map[string]bool) // reuse from Strategy 1
	for f := range changedFiles {
		changedDirsSet[filepath.Dir(f)] = true
	}
	for _, node := range ctx.AllNodes {
		if !changedFiles[node.File] {
			continue
		}
		if !node.Exported && node.Visibility != types.VisibilityPublic {
			continue // Non-exported nodes can only be called from the same file
		}
		// Find other files that have nodes with the same name,
		// but restrict to files that are in the same package (same dir) or
		// that import from the changed file's package
		nodesWithName := ctx.NodesByName[node.Name]
		for _, n := range nodesWithName {
			if changedFiles[n.File] {
				continue // skip changed files — they'll be rebuilt anyway
			}
			nDir := filepath.Dir(n.File)
			// Only mark as affected if:
			// 1. Same package (same directory as any changed file)
			// 2. Or already identified as affected via Strategy 1 or 3
			if changedDirsSet[nDir] || affectedFiles[n.File] {
				affectedFiles[n.File] = true
			}
		}
	}

	// Strategy 3: Find files that had edges pointing to nodes in changed files.
	// When DetectChanges deletes nodes from changed files, ON DELETE CASCADE
	// removes ALL edges referencing those nodes from ANY file. We must rebuild
	// edges for all files that lost edges this way.
	// We use the AffectedFilesFromDB field which is populated by DetectChanges
	// before it deletes the old nodes/edges.
	strategy3Count := 0
	for _, file := range ctx.AffectedFilesFromDB {
		if !changedFiles[file] && !affectedFiles[file] {
			affectedFiles[file] = true
			strategy3Count++
		}
	}
	if strategy3Count > 0 {
		logger.Debug("Strategy 3 found additional affected files from DB", zap.Int("count", strategy3Count))
	}

	if len(affectedFiles) == 0 {
		logger.Debug("No affected files found for edge rebuild")
		return nil
	}

	logger.Info("Rebuilding edges for affected files",
		zap.Int("affectedFileCount", len(affectedFiles)),
	)

	// For each affected file, re-parse and rebuild edges
	var rebuiltEdges []*types.Edge
	for affectedFile := range affectedFiles {
		absPath := filepath.Join(ctx.Opts.RootDir, affectedFile)

		// Read and parse the file
		content, err := os.ReadFile(absPath)
		if err != nil {
			logger.Debug("Failed to read affected file", zap.String("file", absPath), zap.Error(err))
			continue
		}

		lang := ctx.Registry.DetectLanguage(absPath)
		if lang == nil {
			continue
		}

		output, err := lang.Extractor.Extract(content, absPath)
		if err != nil {
			logger.Debug("Failed to parse affected file", zap.String("file", absPath), zap.Error(err))
			continue
		}

		// Collect old edge IDs and delete edges between affected file and changed files.
		// We combine Find+Delete into a single DeleteEdgesBetweenFiles call per pair
		// and collect old IDs via a separate query (needed for frontend delta removal).
		for changedFile := range changedFiles {
			// Collect old edge IDs for delta notification
			oldEdgeIDs, err := ctx.Repo.FindEdgeIDsBetweenFiles(affectedFile, changedFile)
			if err != nil {
				logger.Debug("Failed to query old edge IDs between files",
					zap.String("affectedFile", affectedFile),
					zap.String("changedFile", changedFile),
					zap.Error(err))
			} else {
				ctx.ChangedFileOldEdgeIDs = append(ctx.ChangedFileOldEdgeIDs, oldEdgeIDs...)
			}

			// Delete edges between affected file and this changed file
			if err := ctx.Repo.DeleteEdgesBetweenFiles(affectedFile, changedFile); err != nil {
				logger.Debug("Failed to delete edges between files",
					zap.String("affectedFile", affectedFile),
					zap.String("changedFile", changedFile),
					zap.Error(err))
			}
		}

		// Build a ParseResult for this file
		pr := ParseResult{
			FilePath: absPath,
			Output:   output,
		}

		// Match parsed definitions with existing DB nodes to get correct IDs
		nodesInFile := ctx.NodesByFile[affectedFile]
		for _, def := range output.Definitions {
			for _, existingNode := range nodesInFile {
				if existingNode.Name == def.Name && existingNode.Line == def.Line {
					pr.Nodes = append(pr.Nodes, existingNode)
					break
				}
			}
		}

		// Update memory indexes so that buildEdgesForFile can resolve
		// cross-file references for this affected file correctly.
		// The NodesByFile index already has this file's nodes (from DB),
		// but we need to update AllSymbols so import resolution works.
		ctx.AllSymbols[affectedFile] = output

		// Build edges using the existing infrastructure — collect for batch insert
		edges := buildEdgesForFile(ctx, &pr)
		for _, edge := range edges {
			if edge.SourceID == 0 || edge.TargetID == 0 {
				continue
			}
			rebuiltEdges = append(rebuiltEdges, edge)
		}
		logger.Debug("Edges for affected file",
			zap.String("file", affectedFile),
			zap.Int("totalEdges", len(edges)),
		)
	}

	logger.Info("Rebuilt edges for affected files", zap.Int("edgeCount", len(rebuiltEdges)))
	return rebuiltEdges
}

// nodeKindFilter is a function that filters nodes by kind.
type nodeKindFilter func(types.SymbolKind) bool

// typeFilterClassish matches class-like kinds (class, interface, struct).
func typeFilterClassish(kind types.SymbolKind) bool {
	return kind == types.SymbolKindClass || kind == types.SymbolKindInterface || kind == types.SymbolKindStruct
}

// typeFilterInterface matches only interfaces.
func typeFilterInterface(kind types.SymbolKind) bool {
	return kind == types.SymbolKindInterface
}

// findBestMatchByFile finds the best matching node from candidates by preferring:
// 1. Same file as the source file
// 2. Same directory (package) as the source file
// 3. First matching kind (fallback)
// This avoids incorrectly matching a class from a different package when
// multiple classes share the same name.
func findBestMatchByFile(candidates []*types.Node, sourceFile string, kindFilter nodeKindFilter) *types.Node {
	var sameFileMatch *types.Node
	var sameDirMatch *types.Node
	var anyMatch *types.Node

	sourceDir := filepath.Dir(sourceFile)

	for _, c := range candidates {
		if !kindFilter(c.Kind) {
			continue
		}
		if anyMatch == nil {
			anyMatch = c
		}
		if c.File == sourceFile && sameFileMatch == nil {
			sameFileMatch = c
		}
		if filepath.Dir(c.File) == sourceDir && sameDirMatch == nil {
			sameDirMatch = c
		}
	}

	if sameFileMatch != nil {
		return sameFileMatch
	}
	if sameDirMatch != nil {
		return sameDirMatch
	}
	return anyMatch
}

// collectUsedSymbolNames collects symbol names that are actually referenced
// (via calls) in the file's extractor output. This avoids creating import edges
// for symbols that are imported but never used.
func collectUsedSymbolNames(output *types.ExtractorOutput) map[string]bool {
	used := make(map[string]bool)
	for _, call := range output.Calls {
		if call.Name != "" {
			used[call.Name] = true
		}
	}
	// Also consider class relations (extends/implements reference imported names)
	for _, class := range output.Classes {
		if class.Extends != "" {
			used[class.Extends] = true
		}
		for _, iface := range class.Implements {
			used[iface] = true
		}
	}
	return used
}

// findBestImporterForSymbol finds the most specific node in the file that
// likely uses the given symbol. It prefers functions/methods that call the symbol,
// falling back to the top-level class/struct if no caller is found.
func findBestImporterForSymbol(nodesInFile []*types.Node, symbol string, output *types.ExtractorOutput) int64 {
	// First, try to find a function/method that calls this symbol
	if output != nil {
		for _, call := range output.Calls {
			if call.Name == symbol {
				// Find the enclosing function/method for this call
				for _, node := range nodesInFile {
					if node.Line <= call.Line && (node.EndLine == 0 || node.EndLine >= call.Line) {
						if node.Kind == types.SymbolKindFunction || node.Kind == types.SymbolKindMethod {
							return node.ID
						}
					}
				}
				// Fallback to enclosing class/struct
				for _, node := range nodesInFile {
					if node.Line <= call.Line && (node.EndLine == 0 || node.EndLine >= call.Line) {
						if node.Kind == types.SymbolKindClass || node.Kind == types.SymbolKindStruct {
							return node.ID
						}
					}
				}
			}
		}
	}

	// If the symbol is not called directly, it might be referenced in other ways
	// (type annotation, variable declaration, etc.). Fall back to the first
	// top-level class/struct/interface in the file.
	for _, node := range nodesInFile {
		if node.Kind == types.SymbolKindClass || node.Kind == types.SymbolKindStruct || node.Kind == types.SymbolKindInterface {
			return node.ID
		}
	}
	// Last resort: first function/method
	for _, node := range nodesInFile {
		if node.Kind == types.SymbolKindFunction || node.Kind == types.SymbolKindMethod {
			return node.ID
		}
	}
	return 0
}