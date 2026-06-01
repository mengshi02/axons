// Package graph provides code graph building and querying capabilities.
package graph

import (
	"github.com/mengshi02/axons/pkg/types"
)

// RoleClassifier classifies nodes by their role in the codebase.
type RoleClassifier struct {
	// Thresholds for classification
	EntryFanOutThreshold   int // Min fan-out for entry role
	CoreFanInThreshold     int // Min fan-in for core role
	CoreFanOutThreshold    int // Min fan-out for core role
	AdapterFanOutThreshold int // Min fan-out for adapter role
	AdapterFanInThreshold  int // Max fan-in for adapter role
	LeafFanOutThreshold    int // Max fan-out for leaf role
	UtilityFanInThreshold  int // Max fan-in for utility role
	UtilityFanOutThreshold int // Max fan-out for utility role
	DeadFanInThreshold     int // Max fan-in for dead code
}

// NewRoleClassifier creates a new role classifier with default thresholds.
func NewRoleClassifier() *RoleClassifier {
	return &RoleClassifier{
		EntryFanOutThreshold:   3,
		CoreFanInThreshold:     5,
		CoreFanOutThreshold:    5,
		AdapterFanOutThreshold: 5,
		AdapterFanInThreshold:  3,
		LeafFanOutThreshold:    3,
		UtilityFanInThreshold:  5,
		UtilityFanOutThreshold: 5,
		DeadFanInThreshold:     0,
	}
}

// GraphStats holds statistics about a node's position in the graph.
type GraphStats struct {
	FanIn    int  // Number of callers
	FanOut   int  // Number of callees
	Exported bool // Whether the node is exported
}

// ClassifyRole classifies a node's role based on its graph position.
func (c *RoleClassifier) ClassifyRole(node *types.Node, stats GraphStats) types.Role {
	// Dead code: no callers and not exported
	if stats.FanIn <= c.DeadFanInThreshold && !stats.Exported {
		return types.RoleDead
	}

	// Entry point: no callers but has callees (or is exported)
	if stats.FanIn == 0 && stats.FanOut > 0 {
		return types.RoleEntry
	}

	// Entry point: exported with high fan-out
	if stats.Exported && stats.FanOut >= c.EntryFanOutThreshold {
		return types.RoleEntry
	}

	// Core: high fan-in and high fan-out
	if stats.FanIn >= c.CoreFanInThreshold && stats.FanOut >= c.CoreFanOutThreshold {
		return types.RoleCore
	}

	// Adapter: high fan-out but low fan-in
	if stats.FanOut >= c.AdapterFanOutThreshold && stats.FanIn <= c.AdapterFanInThreshold {
		return types.RoleAdapter
	}

	// Utility: moderate connectivity (check BEFORE leaf to avoid over-matching leaf)
	// A node with FanIn=2, FanOut=2 should be Utility, not Leaf
	if stats.FanIn > 0 && stats.FanIn <= c.UtilityFanInThreshold &&
		stats.FanOut > 0 && stats.FanOut <= c.UtilityFanOutThreshold {
		return types.RoleUtility
	}

	// Leaf: has callers but low fan-out (only truly terminal operations)
	if stats.FanIn > 0 && stats.FanOut == 0 {
		return types.RoleLeaf
	}

	// Fallback: nodes with FanIn > 0 and FanOut < LeafFanOutThreshold
	// that didn't match Utility (e.g., FanIn too high)
	if stats.FanIn > 0 && stats.FanOut < c.LeafFanOutThreshold {
		return types.RoleLeaf
	}

	// Default to utility
	return types.RoleUtility
}

// ClassifyRoles classifies all nodes in the graph.
func (c *RoleClassifier) ClassifyRoles(nodes []*types.Node, edges []*types.Edge) map[int64]types.Role {
	// Build adjacency lists
	fanIn := make(map[int64]int)
	fanOut := make(map[int64]int)
	exported := make(map[int64]bool)

	for _, node := range nodes {
		fanIn[node.ID] = 0
		fanOut[node.ID] = 0
		exported[node.ID] = node.Exported
	}

	// Count fan-in and fan-out (include calls, imports, extends, implements)
	for _, edge := range edges {
		switch edge.Kind {
		case types.EdgeKindCalls:
			fanOut[edge.SourceID]++
			fanIn[edge.TargetID]++
		case types.EdgeKindImports, types.EdgeKindImportsType:
			// Imports contribute to fan-out for the importer
			fanOut[edge.SourceID]++
			// And fan-in for the imported symbol
			fanIn[edge.TargetID]++
		case types.EdgeKindExtends, types.EdgeKindImplements:
			// Inheritance contributes to fan-out for the child
			fanOut[edge.SourceID]++
			// And fan-in for the parent/interface
			fanIn[edge.TargetID]++
		}
	}

	// Classify each node
	roles := make(map[int64]types.Role)
	for _, node := range nodes {
		stats := GraphStats{
			FanIn:    fanIn[node.ID],
			FanOut:   fanOut[node.ID],
			Exported: exported[node.ID],
		}
		roles[node.ID] = c.ClassifyRole(node, stats)
	}

	return roles
}

// GetRoleStatistics returns statistics about role distribution.
func GetRoleStatistics(roles map[int64]types.Role) map[types.Role]int {
	stats := make(map[types.Role]int)
	for _, role := range roles {
		stats[role]++
	}
	return stats
}

// IsDeadCode checks if a node is classified as dead code.
func IsDeadCode(role types.Role) bool {
	return role == types.RoleDead
}

// IsEntryPoint checks if a node is an entry point.
func IsEntryPoint(role types.Role) bool {
	return role == types.RoleEntry
}

// IsCore checks if a node is a core component.
func IsCore(role types.Role) bool {
	return role == types.RoleCore
}

// IsLeaf checks if a node is a leaf node.
func IsLeaf(role types.Role) bool {
	return role == types.RoleLeaf
}

// IsAdapter checks if a node is an adapter.
func IsAdapter(role types.Role) bool {
	return role == types.RoleAdapter
}

// RoleDescription returns a human-readable description of a role.
func RoleDescription(role types.Role) string {
	switch role {
	case types.RoleEntry:
		return "Entry point - high fan-out, initiates operations"
	case types.RoleCore:
		return "Core component - high connectivity, central to system"
	case types.RoleUtility:
		return "Utility - moderate connectivity, helper functions"
	case types.RoleAdapter:
		return "Adapter - connects external systems, high fan-out"
	case types.RoleDead:
		return "Dead code - no references, can be removed"
	case types.RoleTestOnly:
		return "Test-only - only referenced by tests"
	case types.RoleLeaf:
		return "Leaf node - low fan-out, terminal operations"
	default:
		return "Unknown role"
	}
}

// RiskLevel represents the risk level of modifying a node.
type RiskLevel int

const (
	// RiskLow - Safe to modify
	RiskLow RiskLevel = iota
	// RiskMedium - Moderate impact
	RiskMedium
	// RiskHigh - High impact, many dependents
	RiskHigh
	// RiskCritical - Critical, changes affect many components
	RiskCritical
)

// AssessRisk assesses the risk level of modifying a node.
func AssessRisk(node *types.Node, stats GraphStats) RiskLevel {
	// Critical: core components with high fan-in
	if stats.FanIn >= 10 {
		return RiskCritical
	}

	// High: exported functions or moderate fan-in
	if stats.Exported || stats.FanIn >= 5 {
		return RiskHigh
	}

	// Medium: has some callers
	if stats.FanIn > 0 {
		return RiskMedium
	}

	// Low: no callers
	return RiskLow
}

// RiskDescription returns a human-readable description of a risk level.
func RiskDescription(risk RiskLevel) string {
	switch risk {
	case RiskLow:
		return "Low risk - safe to modify"
	case RiskMedium:
		return "Medium risk - verify changes don't break callers"
	case RiskHigh:
		return "High risk - many components depend on this"
	case RiskCritical:
		return "Critical risk - changes will impact many components"
	default:
		return "Unknown risk level"
	}
}