// Package stages provides pipeline stages for graph building.
package stages

import (
	"context"
	"fmt"
)

// StageFunc is the function signature for a pipeline stage.
// The context parameter is only used by stages that need cancellation (e.g. ParseFiles).
type StageFunc func(ctx context.Context, pctx *PipelineContext) error

// PipelineStage declares a named, dependency-driven pipeline stage.
type PipelineStage struct {
	Name    string   // unique stage identifier (e.g. "collect", "parse")
	Deps    []string // names of stages that must complete before this one
	Execute StageFunc
	// Progress reporting for this stage (start%, end%)
	ProgressStart int
	ProgressEnd   int
	ProgressMsg   string // initial progress message
}

// allStages is the ordered registry of all pipeline stages.
// The runner uses Kahn's algorithm to topologically sort by Deps,
// so insertion order here doesn't matter — only dependency declarations do.
var allStages = []PipelineStage{
	{
		Name:          "collect",
		Deps:          []string{},
		Execute:       stageCollectFiles,
		ProgressStart: 1,
		ProgressEnd:   5,
		ProgressMsg:   "Collecting files...",
	},
	{
		Name:          "detect",
		Deps:          []string{"collect"},
		Execute:       stageDetectChanges,
		ProgressStart: 7,
		ProgressEnd:   10,
		ProgressMsg:   "Detecting changes...",
	},
	{
		Name:          "parse",
		Deps:          []string{"detect"},
		Execute:       stageParseFiles,
		ProgressStart: 12,
		ProgressEnd:   40,
		ProgressMsg:   "Parsing files...",
	},
	{
		Name:          "insert",
		Deps:          []string{"parse"},
		Execute:       stageInsertNodes,
		ProgressStart: 42,
		ProgressEnd:   60,
		ProgressMsg:   "Inserting nodes...",
	},
	{
		Name:          "edges",
		Deps:          []string{"insert"},
		Execute:       stageBuildEdges,
		ProgressStart: 62,
		ProgressEnd:   80,
		ProgressMsg:   "Building edges...",
	},
	{
		Name:          "analyses",
		Deps:          []string{"edges"},
		Execute:       stageRunAnalyses,
		ProgressStart: 82,
		ProgressEnd:   90,
		ProgressMsg:   "Running analyses...",
	},
	{
		Name:          "finalize",
		Deps:          []string{"analyses"},
		Execute:       stageFinalize,
		ProgressStart: 92,
		ProgressEnd:   100,
		ProgressMsg:   "Finalizing...",
	},
}

// AllStages returns a copy of the registered pipeline stages.
func AllStages() []PipelineStage {
	return append([]PipelineStage{}, allStages...)
}

// TopologicalSort sorts stages by their dependencies using Kahn's algorithm.
// Returns an error if a cycle is detected or a dependency is missing.
func TopologicalSort(stages []PipelineStage) ([]PipelineStage, error) {
	// Build name -> stage map
	stageMap := make(map[string]PipelineStage, len(stages))
	for _, s := range stages {
		stageMap[s.Name] = s
	}

	// Build in-degree map and adjacency list
	inDegree := make(map[string]int, len(stages))
	dependents := make(map[string][]string) // dep -> list of stages that depend on it

	for _, s := range stages {
		if _, exists := inDegree[s.Name]; !exists {
			inDegree[s.Name] = 0
		}
		for _, dep := range s.Deps {
			if _, exists := stageMap[dep]; !exists {
				return nil, fmt.Errorf("pipeline stage %q depends on unknown stage %q", s.Name, dep)
			}
			inDegree[s.Name]++
			dependents[dep] = append(dependents[dep], s.Name)
		}
	}

	// Find all stages with in-degree 0 (no dependencies)
	var queue []string
	for _, s := range stages {
		if inDegree[s.Name] == 0 {
			queue = append(queue, s.Name)
		}
	}

	var sorted []PipelineStage
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		sorted = append(sorted, stageMap[name])

		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(stages) {
		return nil, fmt.Errorf("circular dependency detected in pipeline stages")
	}

	return sorted, nil
}

// Stage adapter functions that wrap the original stage functions
// to match the StageFunc signature (context.Context, *PipelineContext) error.

func stageCollectFiles(_ context.Context, pctx *PipelineContext) error {
	return CollectFiles(pctx)
}

func stageDetectChanges(_ context.Context, pctx *PipelineContext) error {
	return DetectChanges(pctx)
}

func stageParseFiles(ctx context.Context, pctx *PipelineContext) error {
	return ParseFiles(ctx, pctx)
}

func stageInsertNodes(_ context.Context, pctx *PipelineContext) error {
	return InsertNodes(pctx)
}

func stageBuildEdges(_ context.Context, pctx *PipelineContext) error {
	return BuildEdges(pctx)
}

func stageRunAnalyses(_ context.Context, pctx *PipelineContext) error {
	return RunAnalyses(pctx)
}

func stageFinalize(_ context.Context, pctx *PipelineContext) error {
	return Finalize(pctx)
}