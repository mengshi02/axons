package graph

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestClassifyRole(t *testing.T) {
	c := NewRoleClassifier()
	node := &types.Node{ID: 1, Name: "TestNode"}

	tests := []struct {
		name   string
		stats  GraphStats
		want   types.Role
	}{
		// Dead: FanIn <= 0 && !Exported (DeadFanInThreshold=0)
		{"dead - no callers not exported", GraphStats{FanIn: 0, FanOut: 0, Exported: false}, types.RoleDead},
		{"dead - fanIn=0 not exported even with fanOut", GraphStats{FanIn: 0, FanOut: 3, Exported: false}, types.RoleDead},
		// Entry: exported with high fanOut (FanOut >= EntryFanOutThreshold=3)
		{"entry - exported with high fanOut", GraphStats{FanIn: 10, FanOut: 10, Exported: true}, types.RoleEntry},
		// Core: FanIn >= CoreFanInThreshold=5 && FanOut >= CoreFanOutThreshold=5 (but must not be Entry first)
		{"core - exported high fanIn and fanOut but low fanOut than Entry threshold", GraphStats{FanIn: 5, FanOut: 5, Exported: false}, types.RoleCore},
		// Adapter: FanOut >= AdapterFanOutThreshold=5 && FanIn <= AdapterFanInThreshold=3
		{"adapter - high fanOut low fanIn", GraphStats{FanIn: 2, FanOut: 6, Exported: false}, types.RoleAdapter},
		// Utility: FanIn > 0 && FanIn <= 5 && FanOut > 0 && FanOut <= 5
		{"utility - moderate connectivity", GraphStats{FanIn: 3, FanOut: 3, Exported: false}, types.RoleUtility},
		// Leaf: FanIn > 0 && FanOut == 0
		{"leaf - has callers no callees", GraphStats{FanIn: 5, FanOut: 0, Exported: false}, types.RoleLeaf},
		// Leaf fallback: FanIn > 0 && FanOut < LeafFanOutThreshold=3
		{"leaf fallback - callers low fanOut", GraphStats{FanIn: 10, FanOut: 2, Exported: false}, types.RoleLeaf},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.ClassifyRole(node, tt.stats)
			if got != tt.want {
				t.Errorf("ClassifyRole(%v) = %v, want %v", tt.stats, got, tt.want)
			}
		})
	}
}

func TestClassifyRole_EdgeCases(t *testing.T) {
	c := NewRoleClassifier()
	node := &types.Node{ID: 1}

	t.Run("exported with zero fanOut", func(t *testing.T) {
		// FanIn=0, FanOut=0 → DeadFanInThreshold=0 → FanIn<=0 && !Exported=false
		// Then FanIn==0 && FanOut==0 → not Entry
		// Then Exported && FanOut>=3 → false
		// Then Core → FanIn<5 → false
		// Then Adapter → FanOut<5 → false
		// Then Utility → FanOut==0 → false (FanOut > 0 required)
		// Then Leaf → FanIn>0 → false (FanIn=0)
		// Then Leaf fallback → FanOut<3 → but FanIn=0 → false
		// Default → Utility
		got := c.ClassifyRole(node, GraphStats{FanIn: 0, FanOut: 0, Exported: true})
		if got != types.RoleUtility {
			t.Errorf("exported node with no connectivity = %v, want Utility", got)
		}
	})

	t.Run("fanIn=1 fanOut=1 moderate", func(t *testing.T) {
		got := c.ClassifyRole(node, GraphStats{FanIn: 1, FanOut: 1, Exported: false})
		if got != types.RoleUtility {
			t.Errorf("moderate connectivity = %v, want Utility", got)
		}
	})
}

func TestClassifyRoles(t *testing.T) {
	c := NewRoleClassifier()
	nodes := []*types.Node{
		{ID: 1, Name: "main", Exported: false},
		{ID: 2, Name: "handler", Exported: true},
		{ID: 3, Name: "helper", Exported: false},
	}
	edges := []*types.Edge{
		{SourceID: 1, TargetID: 2, Kind: types.EdgeKindCalls}, // main calls handler
		{SourceID: 2, TargetID: 3, Kind: types.EdgeKindCalls}, // handler calls helper
		{SourceID: 1, TargetID: 3, Kind: types.EdgeKindCalls}, // main calls helper
	}
	roles := c.ClassifyRoles(nodes, edges)
	if len(roles) != 3 {
		t.Errorf("ClassifyRoles = %d roles, want 3", len(roles))
	}
	// main: FanIn=0, FanOut=2, Exported=false → Dead (DeadFanInThreshold=0 means FanIn<=0 && !Exported → Dead)
	if roles[1] != types.RoleDead {
		t.Errorf("main role = %v, want Dead (FanIn=0, not exported)", roles[1])
	}
	// helper: FanIn=2, FanOut=0 → Leaf
	if roles[3] != types.RoleLeaf {
		t.Errorf("helper role = %v, want Leaf", roles[3])
	}
}

func TestGetRoleStatistics(t *testing.T) {
	roles := map[int64]types.Role{
		1: types.RoleEntry,
		2: types.RoleCore,
		3: types.RoleUtility,
		4: types.RoleEntry,
	}
	stats := GetRoleStatistics(roles)
	if stats[types.RoleEntry] != 2 {
		t.Errorf("Entry count = %d, want 2", stats[types.RoleEntry])
	}
	if stats[types.RoleCore] != 1 {
		t.Errorf("Core count = %d, want 1", stats[types.RoleCore])
	}
}

func TestRoleCheckers(t *testing.T) {
	checks := []struct {
		name string
		fn   func(types.Role) bool
		role types.Role
		want bool
	}{
		{"IsDeadCode/Dead", IsDeadCode, types.RoleDead, true},
		{"IsDeadCode/Entry", IsDeadCode, types.RoleEntry, false},
		{"IsEntryPoint/Entry", IsEntryPoint, types.RoleEntry, true},
		{"IsEntryPoint/Core", IsEntryPoint, types.RoleCore, false},
		{"IsCore/Core", IsCore, types.RoleCore, true},
		{"IsCore/Utility", IsCore, types.RoleUtility, false},
		{"IsLeaf/Leaf", IsLeaf, types.RoleLeaf, true},
		{"IsLeaf/Entry", IsLeaf, types.RoleEntry, false},
		{"IsAdapter/Adapter", IsAdapter, types.RoleAdapter, true},
		{"IsAdapter/Core", IsAdapter, types.RoleCore, false},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.role)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoleDescription(t *testing.T) {
	roles := []types.Role{types.RoleEntry, types.RoleCore, types.RoleUtility, types.RoleAdapter, types.RoleDead, types.RoleTestOnly, types.RoleLeaf}
	for _, role := range roles {
		desc := RoleDescription(role)
		if desc == "" || desc == "Unknown role" {
			t.Errorf("RoleDescription(%v) = %q, expected non-empty known description", role, desc)
		}
	}
	// Unknown role
	desc := RoleDescription(types.Role("unknown"))
	if desc != "Unknown role" {
		t.Errorf("RoleDescription(unknown) = %q, want 'Unknown role'", desc)
	}
}

func TestAssessRisk(t *testing.T) {
	node := &types.Node{ID: 1, Name: "Test"}

	tests := []struct {
		name  string
		stats GraphStats
		want  RiskLevel
	}{
		{"critical - high fanIn", GraphStats{FanIn: 10, FanOut: 5, Exported: false}, RiskCritical},
		{"high - exported", GraphStats{FanIn: 1, FanOut: 2, Exported: true}, RiskHigh},
		{"high - moderate fanIn", GraphStats{FanIn: 5, FanOut: 2, Exported: false}, RiskHigh},
		{"medium - some callers", GraphStats{FanIn: 2, FanOut: 1, Exported: false}, RiskMedium},
		{"low - no callers", GraphStats{FanIn: 0, FanOut: 0, Exported: false}, RiskLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssessRisk(node, tt.stats)
			if got != tt.want {
				t.Errorf("AssessRisk(%v) = %v, want %v", tt.stats, got, tt.want)
			}
		})
	}
}

func TestRiskDescription(t *testing.T) {
	levels := []RiskLevel{RiskLow, RiskMedium, RiskHigh, RiskCritical}
	for _, level := range levels {
		desc := RiskDescription(level)
		if desc == "" {
			t.Errorf("RiskDescription(%v) should not be empty", level)
		}
	}
}