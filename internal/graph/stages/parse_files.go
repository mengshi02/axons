// Package stages provides pipeline stages for graph building.
package stages

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// ParseChunk represents a group of files grouped by byte budget for parallel parsing.
type ParseChunk struct {
	Index   int           // chunk index
	Changes []*FileChange // files in this chunk
	Bytes   int64         // total bytes in this chunk
}

// ParseCache provides content-hash-based caching of parse results.
// On incremental builds, unchanged chunks can skip parsing entirely.
type ParseCache struct {
	mu    sync.RWMutex
	store map[string]*ParseResult // key: content hash -> result
}

// NewParseCache creates a new ParseCache.
func NewParseCache() *ParseCache {
	return &ParseCache{
		store: make(map[string]*ParseResult),
	}
}

// Get returns a cached parse result by content hash.
func (c *ParseCache) Get(hash string) (*ParseResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.store[hash]
	return r, ok
}

// Put stores a parse result by content hash.
func (c *ParseCache) Put(hash string, result *ParseResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[hash] = result
}

// chunkFilesByByteBudget groups FileChanges into chunks of ~chunkBudget bytes each.
// This balances work across goroutines and ensures large files don't starve small ones.
func chunkFilesByByteBudget(changes []*FileChange, chunkBudget int64) []*ParseChunk {
	if len(changes) == 0 {
		return nil
	}

	// Sort files by size descending so large files are placed first
	// (avoids the last chunk being dominated by a single huge file)
	sorted := make([]*FileChange, len(changes))
	copy(sorted, changes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Size > sorted[j].Size
	})

	var chunks []*ParseChunk
	var current *ParseChunk

	for _, fc := range sorted {
		if current == nil || current.Bytes+fc.Size > chunkBudget {
			current = &ParseChunk{
				Index:   len(chunks),
				Changes: []*FileChange{fc},
				Bytes:   fc.Size,
			}
			chunks = append(chunks, current)
		} else {
			current.Changes = append(current.Changes, fc)
			current.Bytes += fc.Size
		}
	}
	return chunks
}

// ParseFiles parses all changed files and extracts symbols.
// Files are grouped into byte-budget chunks and parsed in parallel using a goroutine pool.
// A content-hash-based ParseCache allows incremental builds to skip unchanged chunks.
func ParseFiles(ctx context.Context, pctx *PipelineContext) error {
	if pctx.EarlyExit {
		return nil
	}

	start := time.Now()
	defer func() {
		pctx.RecordTiming("parse", time.Since(start))
	}()

	// Configuration
	poolSize := runtime.NumCPU()
	chunkBudget := int64(2 * 1024 * 1024) // 2MB per chunk
	if pctx.Opts != nil {
		if pctx.Opts.WorkerPoolSize > 0 {
			poolSize = pctx.Opts.WorkerPoolSize
		}
		if pctx.Opts.ChunkByteBudget > 0 {
			chunkBudget = pctx.Opts.ChunkByteBudget
		}
	}

	// Group files into byte-budget chunks
	chunks := chunkFilesByByteBudget(pctx.ParseChanges, chunkBudget)
	if len(chunks) == 0 {
		pctx.ParseResults = nil
		logger.Info("No files to parse")
		return nil
	}

	// Initialize parse cache
	parseCache := NewParseCache()

	// Pre-populate cache from existing file hashes for incremental builds
	if !pctx.IsFullBuild && pctx.Repo != nil {
		// For incremental builds, check if we already have parse results in DB
		// that we can reuse (future optimization: store parse results in DB)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, poolSize)
	results := make([]ParseResult, 0, len(pctx.ParseChanges))
	filesParsed := 0
	cacheHits := 0
	totalFiles := len(pctx.ParseChanges)

	// Process chunks in parallel
	for _, chunk := range chunks {
		// Check for cancellation
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}

		wg.Add(1)
		go func(ch *ParseChunk) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			for _, fc := range ch.Changes {
				// Check for cancellation within chunk
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Recover from panics in parser
				func() {
					defer func() {
						if r := recover(); r != nil {
							stack := debug.Stack()
							mu.Lock()
							results = append(results, ParseResult{
								FilePath: fc.Path,
								Err:      fmt.Errorf("parser panic: %v", r),
							})
							mu.Unlock()
							logger.Warn("Parser panic recovered",
								zap.String("file", fc.Path),
								zap.Any("panic", r),
								zap.String("stack", string(stack)),
							)
						}
					}()

					// Read content if not already loaded
					content := fc.Content
					if content == nil {
						var err error
						content, err = os.ReadFile(fc.Path)
						if err != nil {
							mu.Lock()
							results = append(results, ParseResult{
								FilePath: fc.Path,
								Err:      fmt.Errorf("failed to read file: %w", err),
							})
							mu.Unlock()
							return
						}
						// Content was deferred (full build path) — compute hash now
						fc.Content = content
						fc.Hash = ComputeHash(content)
					}

					// Check parse cache
					if cached, ok := parseCache.Get(fc.Hash); ok {
						mu.Lock()
						filesParsed++
						cacheHits++
						results = append(results, ParseResult{
							FilePath: cached.FilePath,
							Output:   cached.Output,
							Nodes:    cached.Nodes,
						})
						pctx.FileHashes[fc.Path] = fc.Hash
						if cached.Output != nil {
							pctx.AllSymbols[fc.Path] = cached.Output
						}
						mu.Unlock()
						return
					}

					// Detect language and parse
					lang := pctx.Registry.DetectLanguage(fc.Path)
					if lang == nil {
						mu.Lock()
						results = append(results, ParseResult{
							FilePath: fc.Path,
							Err:      fmt.Errorf("unsupported file type"),
						})
						mu.Unlock()
						return
					}

					output, err := lang.Extractor.Extract(content, fc.Path)
					if err != nil {
						mu.Lock()
						results = append(results, ParseResult{
							FilePath: fc.Path,
							Err:      fmt.Errorf("failed to parse: %w", err),
						})
						mu.Unlock()
						logger.Warn("Failed to parse file",
							zap.String("file", fc.Path),
							zap.Error(err),
						)
						return
					}

					// Convert definitions to nodes
					nodes := convertDefinitions(output.Definitions, fc.Path, pctx.Opts.RootDir, fc.Hash)

					// Store in parse cache
					result := ParseResult{
						FilePath: fc.Path,
						Output:   output,
						Nodes:    nodes,
					}
					parseCache.Put(fc.Hash, &result)

					mu.Lock()
					filesParsed++
					results = append(results, result)
					pctx.FileHashes[fc.Path] = fc.Hash
					pctx.AllSymbols[fc.Path] = output
					mu.Unlock()

					// Report progress
					if pctx.OnProgress != nil {
						pct := 12 + int(float64(filesParsed)/float64(totalFiles)*28) // 12-40% range
						pctx.OnProgress("parse", pct, fmt.Sprintf("Parsed %d/%d files", filesParsed, totalFiles))
					}
				}()
			}
		}(chunk)
	}
	wg.Wait()

	pctx.ParseResults = results
	logger.Info("Parsed files",
		zap.Int("count", filesParsed),
		zap.Int("cacheHits", cacheHits),
		zap.Int("chunks", len(chunks)),
		zap.Int("poolSize", poolSize),
	)
	return nil
}

// convertDefinitions converts definitions to nodes.
func convertDefinitions(defs []types.Definition, filePath, rootDir, hash string) []*types.Node {
	nodes := make([]*types.Node, 0, len(defs))

	// Convert absolute path to relative path for storage
	relativePath := ToRelativePath(filePath, rootDir)

	for _, def := range defs {
		qualifiedName := def.QualifiedName
		if qualifiedName == "" && def.Parent != "" {
			qualifiedName = def.Parent + "." + def.Name
		} else if qualifiedName == "" {
			qualifiedName = def.Name
		}

		node := &types.Node{
			Name:          def.Name,
			Kind:          def.Kind,
			File:          relativePath,
			Line:          def.Line,
			EndLine:       def.EndLine,
			Exported:      def.Exported,
			QualifiedName: qualifiedName,
			Scope:         def.Scope,
			Visibility:    def.Visibility,
			FileHash:      hash,
		}
		nodes = append(nodes, node)
	}
	return nodes
}