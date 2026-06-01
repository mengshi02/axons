// Package analysis provides code analysis utilities.
package analysis

import (
	"github.com/mengshi02/axons/pkg/types"
)

// CfgBuilder builds control flow graphs from source code.
type CfgBuilder struct{}

// NewCfgBuilder creates a new CFG builder.
func NewCfgBuilder() *CfgBuilder {
	return &CfgBuilder{}
}

// BuildFromAST builds a CFG from AST nodes.
func (b *CfgBuilder) BuildFromAST(astNodes []*types.AstNode) *types.CfgData {
	cfg := &types.CfgData{
		Blocks: []types.CfgBlock{},
		Edges:  []types.CfgEdge{},
	}

	if len(astNodes) == 0 {
		return cfg
	}

	// Group AST nodes into logical blocks
	blocks := b.groupIntoBlocks(astNodes)
	cfg.Blocks = blocks

	// Build edges between blocks
	cfg.Edges = b.buildEdges(blocks)

	return cfg
}

// BlockInfo represents information about a CFG block.
type BlockInfo struct {
	StartLine int
	EndLine   int
	Type      string
	Nodes     []*types.AstNode
}

// groupIntoGroups groups AST nodes into CFG blocks based on control flow.
func (b *CfgBuilder) groupIntoBlocks(astNodes []*types.AstNode) []types.CfgBlock {
	if len(astNodes) == 0 {
		return nil
	}

	var blocks []types.CfgBlock
	var currentBlock *BlockInfo

	for _, node := range astNodes {
		blockType := b.determineBlockType(node)

		// Start new block for control flow statements
		if b.isControlFlowStart(node.Kind) {
			if currentBlock != nil && currentBlock.StartLine > 0 {
				blocks = append(blocks, types.CfgBlock{
					Index:     len(blocks),
					Type:      currentBlock.Type,
					StartLine: currentBlock.StartLine,
					EndLine:   currentBlock.EndLine,
				})
			}
			currentBlock = &BlockInfo{
				StartLine: node.Line,
				EndLine:   node.Line,
				Type:      blockType,
			}
		} else if currentBlock == nil {
			currentBlock = &BlockInfo{
				StartLine: node.Line,
				EndLine:   node.Line,
				Type:      "statement",
			}
		} else {
			currentBlock.EndLine = node.Line
		}

		// Close block after control flow end
		if b.isControlFlowEnd(node.Kind) {
			if currentBlock != nil {
				blocks = append(blocks, types.CfgBlock{
					Index:     len(blocks),
					Type:      currentBlock.Type,
					StartLine: currentBlock.StartLine,
					EndLine:   currentBlock.EndLine,
				})
				currentBlock = nil
			}
		}
	}

	// Add remaining block
	if currentBlock != nil && currentBlock.StartLine > 0 {
		blocks = append(blocks, types.CfgBlock{
			Index:     len(blocks),
			Type:      currentBlock.Type,
			StartLine: currentBlock.StartLine,
			EndLine:   currentBlock.EndLine,
		})
	}

	return blocks
}

// determineBlockType determines the type of a CFG block.
func (b *CfgBuilder) determineBlockType(node *types.AstNode) string {
	switch node.Kind {
	case "if_statement", "if":
		return "condition"
	case "for_statement", "for", "while_statement", "while":
		return "loop"
	case "switch_statement", "switch":
		return "switch"
	case "return_statement", "return":
		return "return"
	case "break_statement", "break":
		return "break"
	case "continue_statement", "continue":
		return "continue"
	case "try_statement", "try":
		return "try"
	case "catch_clause", "catch":
		return "catch"
	case "throw_statement", "throw":
		return "throw"
	case "function_definition", "function_declaration", "method_declaration":
		return "entry"
	case "block", "compound_statement":
		return "block"
	default:
		return "statement"
	}
}

// isControlFlowStart checks if a node starts a new control flow block.
func (b *CfgBuilder) isControlFlowStart(kind string) bool {
	switch kind {
	case "if_statement", "if", "for_statement", "for", "while_statement", "while",
		"switch_statement", "switch", "try_statement", "try",
		"function_definition", "function_declaration", "method_declaration":
		return true
	default:
		return false
	}
}

// isControlFlowEnd checks if a node ends a control flow block.
func (b *CfgBuilder) isControlFlowEnd(kind string) bool {
	switch kind {
	case "return_statement", "return", "break_statement", "break",
		"continue_statement", "continue", "throw_statement", "throw":
		return true
	default:
		return false
	}
}

// buildEdges builds edges between CFG blocks.
func (b *CfgBuilder) buildEdges(blocks []types.CfgBlock) []types.CfgEdge {
	if len(blocks) < 2 {
		return nil
	}

	var edges []types.CfgEdge

	for i := 0; i < len(blocks); i++ {
		block := blocks[i]

		switch block.Type {
		case "condition":
			// If statement: true branch and false branch
			if i+1 < len(blocks) {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: i + 1,
					Kind:        "true",
				})
			}
			// Find else branch (if exists)
			elseIdx := b.findElseBranch(blocks, i)
			if elseIdx >= 0 {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: elseIdx,
					Kind:        "false",
				})
			} else if i+2 < len(blocks) {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: i + 2,
					Kind:        "false",
				})
			}

		case "loop":
			// Loop: body and exit
			if i+1 < len(blocks) {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: i + 1,
					Kind:        "enter",
				})
				// Loop back edge
				edges = append(edges, types.CfgEdge{
					SourceIndex: i + 1,
					TargetIndex: i,
					Kind:        "loop",
				})
			}
			// Exit edge
			if i+2 < len(blocks) {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: i + 2,
					Kind:        "exit",
				})
			}

		case "switch":
			// Switch: multiple case branches
			for j := i + 1; j < len(blocks) && blocks[j].Type == "case"; j++ {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: j,
					Kind:        "case",
				})
			}

		case "return", "break", "continue", "throw":
			// Control flow break - no outgoing edge to next block
			// (exit, continue to loop, etc.)

		default:
			// Sequential flow
			if i+1 < len(blocks) {
				edges = append(edges, types.CfgEdge{
					SourceIndex: i,
					TargetIndex: i + 1,
					Kind:        "sequential",
				})
			}
		}
	}

	return edges
}

// findElseBranch finds the else branch for an if statement.
func (b *CfgBuilder) findElseBranch(blocks []types.CfgBlock, ifIndex int) int {
	depth := 1
	for i := ifIndex + 1; i < len(blocks); i++ {
		if blocks[i].Type == "condition" {
			depth++
		} else if blocks[i].Type == "block" && depth > 0 {
			// Check if this is an else block
			if depth == 1 {
				return i
			}
		}
	}
	return -1
}

// BuildForFunction builds a CFG for a single function.
func (b *CfgBuilder) BuildForFunction(fnName string, astNodes []*types.AstNode) *types.CfgData {
	cfg := b.BuildFromAST(astNodes)
	return cfg
}

// AnalyzeComplexity computes cyclomatic complexity from a CFG.
func (b *CfgBuilder) AnalyzeComplexity(cfg *types.CfgData) int {
	if cfg == nil {
		return 1
	}

	// Cyclomatic complexity = edges - nodes + 2
	edges := len(cfg.Edges)
	nodes := len(cfg.Blocks)

	if nodes == 0 {
		return 1
	}

	complexity := edges - nodes + 2
	if complexity < 1 {
		return 1
	}

	return complexity
}

// FindReachableBlocks finds all blocks reachable from a starting block.
func (b *CfgBuilder) FindReachableBlocks(cfg *types.CfgData, startIndex int) []int {
	if cfg == nil || startIndex < 0 || startIndex >= len(cfg.Blocks) {
		return nil
	}

	visited := make(map[int]bool)
	var reachable []int

	var dfs func(index int)
	dfs = func(index int) {
		if visited[index] {
			return
		}
		visited[index] = true
		reachable = append(reachable, index)

		for _, edge := range cfg.Edges {
			if edge.SourceIndex == index {
				dfs(edge.TargetIndex)
			}
		}
	}

	dfs(startIndex)
	return reachable
}

// FindUnreachableBlocks finds blocks that cannot be reached from the entry point.
func (b *CfgBuilder) FindUnreachableBlocks(cfg *types.CfgData) []int {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return nil
	}

	reachable := b.FindReachableBlocks(cfg, 0)
	reachableSet := make(map[int]bool)
	for _, idx := range reachable {
		reachableSet[idx] = true
	}

	var unreachable []int
	for i := range cfg.Blocks {
		if !reachableSet[i] {
			unreachable = append(unreachable, i)
		}
	}

	return unreachable
}