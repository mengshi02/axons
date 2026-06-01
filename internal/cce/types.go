// Package cce implements the Cognitive Context Engine for axons.
// CCE provides bimodal embedding, context-aware retrieval, and structured
// context assembly to deliver high-quality code context for LLM conversations.
package cce

import (
	"fmt"
	"time"
)

// EmbeddingMode represents the embedding generation mode.
type EmbeddingMode string

const (
	// ModeDescription generates embeddings from metadata text only (legacy behavior).
	ModeDescription EmbeddingMode = "description"
	// ModeCode generates embeddings from source code snippets only.
	ModeCode EmbeddingMode = "code"
	// ModeDual generates both description and code embeddings (bimodal).
	ModeDual EmbeddingMode = "dual"
)

// ContextTemplate defines a preset strategy for context collection.
type ContextTemplate string

const (
	// TemplateUnderstandFunction collects context for understanding a function.
	TemplateUnderstandFunction ContextTemplate = "understand_function"
	// TemplateChangeImpact collects context for assessing change impact.
	TemplateChangeImpact ContextTemplate = "change_impact"
	// TemplateDebugTrace collects context for debugging/tracing issues.
	TemplateDebugTrace ContextTemplate = "debug_trace"
	// TemplateExploreModule collects context for exploring a module.
	TemplateExploreModule ContextTemplate = "explore_module"
	// TemplateGeneral is a general-purpose context collection template.
	TemplateGeneral ContextTemplate = "general"
)

// CodeChunk represents a chunk of source code extracted for embedding.
type CodeChunk struct {
	ID           int64  `json:"id"`
	NodeID       int64  `json:"node_id"`        // Associated graph node
	File         string `json:"file"`            // Source file path
	StartLine    int    `json:"start_line"`      // Start line (1-based)
	EndLine      int    `json:"end_line"`        // End line (inclusive)
	Content      string `json:"content"`         // Source code content
	Language     string `json:"language"`        // Programming language
	ContentHash  string `json:"content_hash"`    // SHA-256 hash for change detection
	CreatedAt    string `json:"created_at"`
}

// CodeEmbedding represents a code-mode embedding for a chunk.
type CodeEmbedding struct {
	ChunkID     int64     `json:"chunk_id"`
	ChunkIndex  int       `json:"chunk_index"` // Index of this chunk within the node (0-based)
	Vector      []float32 `json:"-"`
	Model       string    `json:"model"`
	Text        string    `json:"text,omitempty"` // Source text used for embedding
}

// DualEmbedding holds both description and code embeddings for a node.
type DualEmbedding struct {
	NodeID           int64     `json:"node_id"`
	DescriptionVec   []float32 `json:"-"`
	CodeVec          []float32 `json:"-"`
	DescriptionModel string    `json:"description_model"`
	CodeModel        string    `json:"code_model"`
	DescriptionText  string    `json:"description_text,omitempty"`
	CodeText         string    `json:"code_text,omitempty"`
}

// RetrievalQuery represents a context-aware retrieval request.
type RetrievalQuery struct {
	Query       string          `json:"query"`
	Template    ContextTemplate `json:"template"`
	Anchors     []Anchor        `json:"anchors,omitempty"`     // Anchor symbols for context-aware expansion
	MaxTokens   int             `json:"max_tokens"`            // Token budget for assembled context
	MinScore    float32         `json:"min_score"`             // Minimum relevance score
	MaxResults  int             `json:"max_results"`           // Maximum number of results
	NoTests     bool            `json:"no_tests"`              // Exclude test files
	FileFilter  string          `json:"file_filter,omitempty"` // File path pattern filter
}

// Anchor represents a reference point for context-aware retrieval.
type Anchor struct {
	NodeID       int64  `json:"node_id"`
	SymbolName   string `json:"symbol_name"`
	Kind         string `json:"kind"`
	File         string `json:"file"`
	RelationType string `json:"relation_type,omitempty"` // How this anchor relates to the query
}

// RetrievalResult represents a single retrieved context item.
type RetrievalResult struct {
	NodeID       int64   `json:"node_id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	File         string  `json:"file"`
	Line         int     `json:"line"`
	EndLine      int     `json:"end_line"`
	Content      string  `json:"content,omitempty"`       // Source code content
	Score        float32 `json:"score"`                   // Combined relevance score
	Source       string  `json:"source"`                  // "semantic", "keyword", "graph"
	Depth        int     `json:"depth,omitempty"`          // Graph traversal depth (0=direct, 1=neighbor...)
}

// AssembledContext represents the final assembled context for LLM consumption.
type AssembledContext struct {
	Template      ContextTemplate     `json:"template"`
	Sections      []ContextSection    `json:"sections"`
	TotalTokens   int                 `json:"total_tokens"`
	Sources       []ContextSource     `json:"sources"`
	Metadata      ContextMetadata     `json:"metadata"`
}

// ContextSection represents a structured section in the assembled context.
type ContextSection struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Tokens   int    `json:"tokens"`
	Priority int    `json:"priority"` // Higher = more important, used for budget allocation
}

// ContextSource tracks the origin of context items for attribution.
type ContextSource struct {
	NodeID int64   `json:"node_id"`
	Name   string  `json:"name"`
	File   string  `json:"file"`
	Score  float32 `json:"score"`
	Source string  `json:"source"`
}

// ContextMetadata contains metadata about the context assembly process.
type ContextMetadata struct {
	Query         string    `json:"query"`
	Template      string    `json:"template"`
	GeneratedAt   time.Time `json:"generated_at"`
	RetrievalTime string    `json:"retrieval_time_ms"`
	AssemblyTime  string    `json:"assembly_time_ms"`
	ResultCount   int       `json:"result_count"`
	AnchorsUsed   int       `json:"anchors_used"`
	EngineVersion string    `json:"engine_version"`
}

// TemplateConfig defines the retrieval and assembly parameters for a context template.
type TemplateConfig struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	GraphDepth        int             `json:"graph_depth"`         // How deep to traverse the graph
	MaxTokens         int             `json:"max_tokens"`          // Default token budget
	MaxResults        int             `json:"max_results"`         // Max retrieval results
	Sections          []SectionSpec   `json:"sections"`            // Sections to assemble
	EdgeTypes         []string        `json:"edge_types"`          // Which edge types to follow
	IncludeCallers    bool            `json:"include_callers"`     // Include caller symbols
	IncludeCallees    bool            `json:"include_callees"`     // Include callee symbols
	IncludeImports    bool            `json:"include_imports"`     // Include imported symbols
	IncludeDataflow   bool            `json:"include_dataflow"`    // Include data flow context
	MinScore          float32         `json:"min_score"`           // Minimum relevance score
}

// SectionSpec defines a section to include in assembled context.
type SectionSpec struct {
	Title    string `json:"title"`
	MaxLines int    `json:"max_lines"`   // Max lines of source code per item
	Priority int    `json:"priority"`    // Higher priority sections are kept when trimming
}

// BuiltinTemplates returns the built-in context template configurations.
func BuiltinTemplates() map[ContextTemplate]*TemplateConfig {
	return map[ContextTemplate]*TemplateConfig{
		TemplateUnderstandFunction: {
			Name:        "understand_function",
			Description: "Understand what a function does and how it works",
			GraphDepth:  1,
			MaxTokens:   4000,
			MaxResults:  15,
			Sections: []SectionSpec{
				{Title: "Target Function", MaxLines: 80, Priority: 10},
				{Title: "Called Functions", MaxLines: 40, Priority: 7},
				{Title: "Callers", MaxLines: 40, Priority: 6},
				{Title: "Type Definitions", MaxLines: 30, Priority: 5},
				{Title: "Related Constants", MaxLines: 20, Priority: 3},
			},
			EdgeTypes:      []string{"calls", "called_by", "uses_type", "defines"},
			IncludeCallers:  true,
			IncludeCallees:  true,
			IncludeImports:  false,
			IncludeDataflow: false,
			MinScore:        0.15,
		},
		TemplateChangeImpact: {
			Name:        "change_impact",
			Description: "Assess the impact of changing a symbol",
			GraphDepth:  2,
			MaxTokens:   6000,
			MaxResults:  25,
			Sections: []SectionSpec{
				{Title: "Target Symbol", MaxLines: 60, Priority: 10},
				{Title: "Direct Callers", MaxLines: 50, Priority: 9},
				{Title: "Indirect Callers", MaxLines: 30, Priority: 6},
				{Title: "Implementations", MaxLines: 40, Priority: 8},
				{Title: "Data Flow Sources", MaxLines: 30, Priority: 5},
			},
			EdgeTypes:      []string{"calls", "called_by", "implements", "extends", "dataflow"},
			IncludeCallers:  true,
			IncludeCallees:  false,
			IncludeImports:  true,
			IncludeDataflow: true,
			MinScore:        0.1,
		},
		TemplateDebugTrace: {
			Name:        "debug_trace",
			Description: "Trace execution flow for debugging",
			GraphDepth:  2,
			MaxTokens:   5000,
			MaxResults:  20,
			Sections: []SectionSpec{
				{Title: "Entry Point", MaxLines: 60, Priority: 10},
				{Title: "Call Chain", MaxLines: 40, Priority: 9},
				{Title: "Data Flow", MaxLines: 30, Priority: 8},
				{Title: "Error Handling", MaxLines: 30, Priority: 7},
			},
			EdgeTypes:      []string{"calls", "dataflow", "handles_error"},
			IncludeCallers:  false,
			IncludeCallees:  true,
			IncludeImports:  false,
			IncludeDataflow: true,
			MinScore:        0.12,
		},
		TemplateExploreModule: {
			Name:        "explore_module",
			Description: "Explore and understand a module or package",
			GraphDepth:  1,
			MaxTokens:   5000,
			MaxResults:  20,
			Sections: []SectionSpec{
				{Title: "Module Overview", MaxLines: 40, Priority: 9},
				{Title: "Public API", MaxLines: 50, Priority: 10},
				{Title: "Internal Helpers", MaxLines: 30, Priority: 5},
				{Title: "Dependencies", MaxLines: 30, Priority: 6},
			},
			EdgeTypes:      []string{"contains", "imports", "uses_type"},
			IncludeCallers:  false,
			IncludeCallees:  true,
			IncludeImports:  true,
			IncludeDataflow: false,
			MinScore:        0.15,
		},
		TemplateGeneral: {
			Name:        "general",
			Description: "General-purpose context collection",
			GraphDepth:  1,
			MaxTokens:   4000,
			MaxResults:  15,
			Sections: []SectionSpec{
				{Title: "Relevant Code", MaxLines: 60, Priority: 10},
				{Title: "Related Symbols", MaxLines: 40, Priority: 7},
				{Title: "Context", MaxLines: 30, Priority: 5},
			},
			EdgeTypes:      []string{"calls", "called_by", "uses_type"},
			IncludeCallers:  true,
			IncludeCallees:  true,
			IncludeImports:  false,
			IncludeDataflow: false,
			MinScore:        0.2,
		},
	}
}

// GetTemplate returns the config for a template, falling back to General.
func GetTemplate(t ContextTemplate) *TemplateConfig {
	templates := BuiltinTemplates()
	if cfg, ok := templates[t]; ok {
		return cfg
	}
	return templates[TemplateGeneral]
}

// FormatContextForLLM formats the assembled context into a string for LLM consumption.
func (ac *AssembledContext) FormatContextForLLM() string {
	if len(ac.Sections) == 0 {
		return ""
	}

	var result string
	for _, section := range ac.Sections {
		if section.Content == "" {
			continue
		}
		result += fmt.Sprintf("### %s\n%s\n\n", section.Title, section.Content)
	}
	return result
}

// FormatCCEBanner returns a brand banner indicating CCE is providing context.
func FormatCCEBanner(template ContextTemplate, resultCount int) string {
	return fmt.Sprintf("[axons-cce] Context Engine active | template: %s | %d sources gathered", template, resultCount)
}