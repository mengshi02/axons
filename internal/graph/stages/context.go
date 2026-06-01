// Package stages provides pipeline stages for graph building.
package stages

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/extractors"
	"github.com/mengshi02/axons/pkg/types"
)

// ParseResult holds the result of parsing a single file.
type ParseResult struct {
	FilePath string
	Output   *types.ExtractorOutput
	Nodes    []*types.Node
	Err      error
}

// FileChange represents a changed file to parse.
type FileChange struct {
	Path    string
	Hash    string
	Content []byte
	Mtime   int64
	Size    int64
}

// SuffixIndex provides O(1) suffix-based file path lookup.
// Maps every possible path suffix to its original file path.
type SuffixIndex struct {
	ExactMap map[string]string   // normalized suffix -> original file path
	LowerMap map[string]string   // lowercase suffix -> original file path
	DirFiles map[string][]string // directory suffix -> file paths
	AllFiles map[string]bool     // all file paths for exact matching
}

// NodeLookup provides on-demand node resolution for edge building.
// For small projects, the in-memory PipelineContext indexes are used.
// For large projects, DB queries are used to avoid loading all nodes into memory.
type NodeLookup interface {
	FindByName(name string) ([]*types.Node, error)
	FindByQualifiedName(qname string) ([]*types.Node, error)
}

// dbNodeLookup implements NodeLookup using database queries.
type dbNodeLookup struct {
	repo *repository.Repository
}

func (l *dbNodeLookup) FindByName(name string) ([]*types.Node, error) {
	return l.repo.FindNodesByNameExact(name)
}

func (l *dbNodeLookup) FindByQualifiedName(qname string) ([]*types.Node, error) {
	return l.repo.FindNodesByQualifiedName(qname)
}

// memoryNodeLookup implements NodeLookup using in-memory PipelineContext indexes.
type memoryNodeLookup struct {
	ctx *PipelineContext
}

func (l *memoryNodeLookup) FindByName(name string) ([]*types.Node, error) {
	return l.ctx.NodesByName[name], nil
}

func (l *memoryNodeLookup) FindByQualifiedName(qname string) ([]*types.Node, error) {
	return l.ctx.NodesByQualified[qname], nil
}

// PipelineContext holds shared state across all build stages.
type PipelineContext struct {
	// Inputs
	Opts       *types.BuildOptions
	Repo       *repository.Repository
	GlobalRepo *repository.GlobalRepository
	Registry   *extractors.Registry

	// Progress callback — called by each pipeline stage to report progress.
	// phase: stage name (collect/detect/parse/insert/edges/analyses/finalize)
	// percent: 0-100 overall progress
	// message: human-readable description
	OnProgress func(phase string, percent int, message string)

	// Delta callback — called after a stage completes to push intermediate graph data.
	// stage: stage name (insert/edges/analyses)
	// nodes: newly created nodes (nil if no nodes in this delta)
	// edges: newly created edges (nil if no edges in this delta)
	OnDelta func(stage string, nodes []*types.Node, edges []*types.Edge)

	// File collection
	AllFiles       []string
	DiscoveredDirs map[string]bool
	SupportedExts  map[string]bool
	LanguageStats  map[string]int // language -> file count

	// Change detection
	IsFullBuild              bool
	ParseChanges             []*FileChange
	RemovedFiles             []string
	EarlyExit                bool
	FileHashes               map[string]string
	ChangedFileOldNodeIDs    []int64   // Old node IDs from changed files (before deletion)
	ChangedFileOldEdgeIDs    []int64   // Old edge IDs from changed files (before deletion)
	AffectedFilesFromDB      []string  // Files that had edges to changed-file nodes (populated before deletion)

	// Parsing results
	ParseResults []ParseResult
	AllSymbols   map[string]*types.ExtractorOutput

	// Node indexes (built during insertNodes)
	AllNodes         []*types.Node
	AllEdges         []*types.Edge // edges created during BuildEdges (for delta push)
	NodesByName      map[string][]*types.Node
	NodesByFile      map[string][]*types.Node
	NodesByDir       map[string][]*types.Node
	NodesByQualified map[string][]*types.Node
	SuffixIndex      *SuffixIndex

	// NodeLookup for on-demand resolution (set during InsertNodes based on node count)
	// When UseDBLookup is true, BuildEdges uses DB queries instead of in-memory indexes
	NodeLookup NodeLookup
	UseDBLookup bool

	// Timing
	BuildStart time.Time
	Timing     map[string]time.Duration

	// Concurrency
	mu sync.RWMutex
}

// NewPipelineContext creates a new pipeline context.
func NewPipelineContext() *PipelineContext {
	return &PipelineContext{
		Opts:             &types.BuildOptions{},
		Registry:         extractors.DefaultRegistry,
		DiscoveredDirs:   make(map[string]bool),
		SupportedExts:    make(map[string]bool),
		LanguageStats:    make(map[string]int),
		FileHashes:       make(map[string]string),
		AllSymbols:       make(map[string]*types.ExtractorOutput),
		NodesByName:      make(map[string][]*types.Node),
		NodesByFile:      make(map[string][]*types.Node),
		NodesByDir:       make(map[string][]*types.Node),
		NodesByQualified: make(map[string][]*types.Node),
		Timing:           make(map[string]time.Duration),
	}
}

// RecordTiming records the duration of a stage.
func (c *PipelineContext) RecordTiming(stage string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Timing[stage] = d
}

// GetTiming returns the duration of a stage.
func (c *PipelineContext) GetTiming(stage string) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Timing[stage]
}

// AddNode adds a node to the indexes.
func (c *PipelineContext) AddNode(node *types.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.AllNodes = append(c.AllNodes, node)
	c.NodesByName[node.Name] = append(c.NodesByName[node.Name], node)
	c.NodesByFile[node.File] = append(c.NodesByFile[node.File], node)
	if node.QualifiedName != "" {
		c.NodesByQualified[node.QualifiedName] = append(c.NodesByQualified[node.QualifiedName], node)
	}
	dir := ""
	for i := len(node.File) - 1; i >= 0; i-- {
		if node.File[i] == '/' || node.File[i] == '\\' {
			dir = node.File[:i]
			break
		}
	}
	if dir != "" {
		c.NodesByDir[dir] = append(c.NodesByDir[dir], node)
	}
}

// GetNodesByName returns nodes by name.
func (c *PipelineContext) GetNodesByName(name string) []*types.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.NodesByName[name]
}

// GetNodesByFile returns nodes by file.
func (c *PipelineContext) GetNodesByFile(file string) []*types.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.NodesByFile[file]
}

// GetNodeByQualified returns nodes by qualified name.
func (c *PipelineContext) GetNodeByQualified(qualified string) []*types.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.NodesByQualified[qualified]
}

// BuildSuffixIndex builds a suffix index for O(1) file path lookups.
func (c *PipelineContext) BuildSuffixIndex(filePaths []string) {
	exactMap := make(map[string]string)
	lowerMap := make(map[string]string)
	dirFiles := make(map[string][]string)
	allFiles := make(map[string]bool)

	for _, path := range filePaths {
		allFiles[path] = true
		normalized := filepath.ToSlash(path)
		parts := strings.Split(normalized, "/")

		// Index all suffixes
		for i := len(parts) - 1; i >= 0; i-- {
			suffix := strings.Join(parts[i:], "/")
			if _, exists := exactMap[suffix]; !exists {
				exactMap[suffix] = path
			}
			lowerSuffix := strings.ToLower(suffix)
			if _, exists := lowerMap[lowerSuffix]; !exists {
				lowerMap[lowerSuffix] = path
			}
		}

		// Index directory membership for package-level imports
		if len(parts) > 1 {
			dir := strings.Join(parts[:len(parts)-1], "/")
			ext := filepath.Ext(path)
			key := dir + ":" + ext
			dirFiles[key] = append(dirFiles[key], path)
		}
	}

	c.SuffixIndex = &SuffixIndex{
		ExactMap: exactMap,
		LowerMap: lowerMap,
		DirFiles: dirFiles,
		AllFiles: allFiles,
	}
}