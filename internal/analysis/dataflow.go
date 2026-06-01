// Package analysis provides code analysis utilities.
package analysis

import (
	"github.com/mengshi02/axons/pkg/types"
)

// DataflowAnalyzer analyzes data flow in source code.
type DataflowAnalyzer struct {
	nodeMap map[string]int64
}

// NewDataflowAnalyzer creates a new dataflow analyzer.
func NewDataflowAnalyzer() *DataflowAnalyzer {
	return &DataflowAnalyzer{
		nodeMap: make(map[string]int64),
	}
}

// SetNodeMap sets the node name to ID mapping.
func (a *DataflowAnalyzer) SetNodeMap(nodes []*types.Node) {
	a.nodeMap = make(map[string]int64)
	for _, node := range nodes {
		if node.Name != "" {
			a.nodeMap[node.Name] = node.ID
		}
	}
}

// AnalyzeFunction analyzes a function for data flow patterns.
func (a *DataflowAnalyzer) AnalyzeFunction(fnName string, params []string, astNodes []*types.AstNode) *types.DataflowResult {
	result := &types.DataflowResult{}

	for i, param := range params {
		flow := types.DataflowParam{
			FuncName:   fnName,
			ParamName:  param,
			ParamIndex: i,
			Line:       a.findParamLine(param, astNodes),
		}
		result.Parameters = append(result.Parameters, flow)
	}

	result.Returns = a.findReturnFlows(fnName, astNodes)
	result.Assignments = a.findAssignments(fnName, astNodes)
	result.ArgFlows = a.findArgFlows(fnName, astNodes)
	result.Mutations = a.findMutations(fnName, astNodes)

	return result
}

func (a *DataflowAnalyzer) findParamLine(param string, astNodes []*types.AstNode) int {
	for _, node := range astNodes {
		if node.Name == param && (node.Kind == "parameter" || node.Kind == "identifier") {
			return node.Line
		}
	}
	return 0
}

func (a *DataflowAnalyzer) findReturnFlows(fnName string, astNodes []*types.AstNode) []types.DataflowReturn {
	var returns []types.DataflowReturn

	for _, node := range astNodes {
		if node.Kind == "return_statement" {
			ret := types.DataflowReturn{
				FuncName:        fnName,
				Expression:      node.Text,
				ReferencedNames: a.extractVarsFromText(node.Text),
				Line:            node.Line,
			}
			returns = append(returns, ret)
		}
	}

	return returns
}

func (a *DataflowAnalyzer) findAssignments(fnName string, astNodes []*types.AstNode) []types.DataflowAssignment {
	var assignments []types.DataflowAssignment

	for _, node := range astNodes {
		if node.Kind == "assignment" || node.Kind == "variable_declarator" {
			assign := types.DataflowAssignment{
				VarName:        node.Name,
				CallerFunc:     fnName,
				Expression:     node.Text,
				SourceCallName: a.extractSourceFromText(node.Text),
				Line:           node.Line,
			}
			assignments = append(assignments, assign)
		}
	}

	return assignments
}

func (a *DataflowAnalyzer) findArgFlows(fnName string, astNodes []*types.AstNode) []types.DataflowArgFlow {
	var argFlows []types.DataflowArgFlow

	for _, node := range astNodes {
		if node.Kind == "call_expression" || node.Kind == "function_call" {
			args := a.extractArgsFromCall(node.Text)
			for i, arg := range args {
				call := types.DataflowArgFlow{
					CallerFunc: fnName,
					CalleeName: node.Name,
					ArgIndex:   i,
					ArgName:    arg,
					Expression: node.Text,
					Line:       node.Line,
					Confidence: 0.85,
				}
				argFlows = append(argFlows, call)
			}
		}
	}

	return argFlows
}

func (a *DataflowAnalyzer) findMutations(fnName string, astNodes []*types.AstNode) []types.DataflowMutation {
	var mutations []types.DataflowMutation

	for _, node := range astNodes {
		if node.Kind == "update_expression" || node.Kind == "augmented_assignment" {
			mut := types.DataflowMutation{
				FuncName:     fnName,
				ReceiverName: node.Name,
				MutatingExpr: node.Text,
				Line:         node.Line,
			}
			mutations = append(mutations, mut)
		}
	}

	return mutations
}

func (a *DataflowAnalyzer) extractVarsFromText(text string) []string {
	if text == "" {
		return nil
	}
	var vars []string
	words := tokenize(text)
	for _, word := range words {
		if isIdentifierStr(word) {
			vars = append(vars, word)
		}
	}
	return vars
}

func (a *DataflowAnalyzer) extractSourceFromText(text string) string {
	if text == "" {
		return ""
	}
	for i, ch := range text {
		if ch == '=' && i+1 < len(text) {
			return text[i+1:]
		}
	}
	return ""
}

func (a *DataflowAnalyzer) extractArgsFromCall(text string) []string {
	if text == "" {
		return nil
	}
	start := -1
	depth := 0
	for i, ch := range text {
		if ch == '(' {
			if start == -1 {
				start = i + 1
			}
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 && start != -1 {
				return splitArgs(text[start:i])
			}
		}
	}
	return nil
}

func (a *DataflowAnalyzer) detectMutationType(text string) string {
	if containsStr(text, "++") || containsStr(text, "+=") {
		return "increment"
	}
	if containsStr(text, "--") || containsStr(text, "-=") {
		return "decrement"
	}
	if containsStr(text, "*=") {
		return "multiply"
	}
	if containsStr(text, "/=") {
		return "divide"
	}
	return "unknown"
}

// BuildDataflowEdges builds dataflow edges from analysis results.
func (a *DataflowAnalyzer) BuildDataflowEdges(result *types.DataflowResult, fnID int64) []*types.DataflowEdge {
	var edges []*types.DataflowEdge

	for _, param := range result.Parameters {
		paramID, ok := a.nodeMap[param.ParamName]
		if !ok {
			continue
		}
		idx := param.ParamIndex
		edges = append(edges, &types.DataflowEdge{
			SourceID:   fnID,
			TargetID:   paramID,
			Kind:       types.DataflowKindParameter,
			ParamIndex: &idx,
			Line:       param.Line,
			Confidence: 0.9,
		})
	}

	for _, assign := range result.Assignments {
		targetID, ok := a.nodeMap[assign.VarName]
		if !ok {
			continue
		}
		edges = append(edges, &types.DataflowEdge{
			SourceID:   fnID,
			TargetID:   targetID,
			Kind:       types.DataflowKindAssignment,
			Expression: assign.Expression,
			Line:       assign.Line,
			Confidence: 0.95,
		})
	}

	for _, ret := range result.Returns {
		for _, v := range ret.ReferencedNames {
			targetID, ok := a.nodeMap[v]
			if !ok {
				continue
			}
			edges = append(edges, &types.DataflowEdge{
				SourceID:   targetID,
				TargetID:   fnID,
				Kind:       types.DataflowKindReturn,
				Line:       ret.Line,
				Confidence: 0.9,
			})
		}
	}

	for _, argFlow := range result.ArgFlows {
		calleeID, ok := a.nodeMap[argFlow.CalleeName]
		if !ok {
			continue
		}
		argID, ok := a.nodeMap[argFlow.ArgName]
		if ok {
			idx := argFlow.ArgIndex
			edges = append(edges, &types.DataflowEdge{
				SourceID:   argID,
				TargetID:   calleeID,
				Kind:       types.DataflowKindArgFlow,
				ParamIndex: &idx,
				Line:       argFlow.Line,
				Confidence: argFlow.Confidence,
			})
		}
	}

	for _, mut := range result.Mutations {
		varID, ok := a.nodeMap[mut.ReceiverName]
		if !ok {
			continue
		}
		edges = append(edges, &types.DataflowEdge{
			SourceID:   fnID,
			TargetID:   varID,
			Kind:       types.DataflowKindMutation,
			Line:       mut.Line,
			Confidence: 0.95,
		})
	}

	return edges
}

func containsVar(text, varName string) bool {
	words := tokenize(text)
	for _, w := range words {
		if w == varName {
			return true
		}
	}
	return false
}

func tokenize(text string) []string {
	var tokens []string
	var current []rune

	for _, ch := range text {
		if isAlphaNum(ch) || ch == '_' {
			current = append(current, ch)
		} else {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}

	return tokens
}

func isIdentifierStr(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, ch := range s {
		if i == 0 && !isAlpha(ch) && ch != '_' {
			return false
		}
		if i > 0 && !isAlphaNum(ch) && ch != '_' {
			return false
		}
	}
	return true
}

func isAlpha(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isAlphaNum(ch rune) bool {
	return isAlpha(ch) || (ch >= '0' && ch <= '9')
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitArgs(args string) []string {
	var result []string
	var current []rune
	depth := 0

	for _, ch := range args {
		if ch == '(' || ch == '[' || ch == '{' {
			depth++
			current = append(current, ch)
		} else if ch == ')' || ch == ']' || ch == '}' {
			depth--
			current = append(current, ch)
		} else if ch == ',' && depth == 0 {
			if len(current) > 0 {
				result = append(result, trim(string(current)))
				current = nil
			}
		} else {
			current = append(current, ch)
		}
	}

	if len(current) > 0 {
		result = append(result, trim(string(current)))
	}

	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}