package cce

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/version"
)

// ContextAssembler takes retrieval results and assembles them into a structured
// context suitable for LLM consumption. It applies template-driven section
// organization, token budget management, and content deduplication.
type ContextAssembler struct {
	// maxTokensPerChar is an approximate ratio for token estimation.
	// English/code averages ~4 chars per token.
	maxTokensPerChar float64
}

// NewContextAssembler creates a new context assembler.
func NewContextAssembler() *ContextAssembler {
	return &ContextAssembler{
		maxTokensPerChar: 0.25, // ~4 chars per token
	}
}

// Assemble builds an AssembledContext from retrieval results and a template.
func (a *ContextAssembler) Assemble(results []*RetrievalResult, query *RetrievalQuery) *AssembledContext {
	startTime := time.Now()
	templateConfig := GetTemplate(query.Template)

	maxTokens := query.MaxTokens
	if maxTokens <= 0 {
		maxTokens = templateConfig.MaxTokens
	}

	// Organize results into template sections
	sections := a.organizeSections(results, templateConfig)

	// Apply token budget
	sections = a.applyBudget(sections, maxTokens)

	// Build source attribution
	sources := a.buildSources(results)

	// Calculate total tokens
	totalTokens := 0
	for _, sec := range sections {
		totalTokens += sec.Tokens
	}

	return &AssembledContext{
		Template:    query.Template,
		Sections:    sections,
		TotalTokens: totalTokens,
		Sources:     sources,
		Metadata: ContextMetadata{
			Query:         query.Query,
			Template:      string(query.Template),
			GeneratedAt:   startTime,
			AssemblyTime:  fmt.Sprintf("%d", time.Since(startTime).Milliseconds()),
			ResultCount:   len(results),
			AnchorsUsed:   len(query.Anchors),
			EngineVersion: version.Version,
		},
	}
}

// organizeSections groups retrieval results into template-defined sections.
func (a *ContextAssembler) organizeSections(results []*RetrievalResult, config *TemplateConfig) []ContextSection {
	sections := make([]ContextSection, 0, len(config.Sections))

	// Categorize results by their characteristics
	type categorized struct {
		result   *RetrievalResult
		category string // matches a section title pattern
	}

	var targets, callers, callees, types, dataflow, imports, general []*RetrievalResult

	for _, r := range results {
		switch {
		case r.Depth == 0 && (r.Source == "semantic_desc" || r.Source == "semantic_code" || r.Source == "keyword"):
			targets = append(targets, r)
		case strings.Contains(r.Source, "graph") && a.isCallerRelation(r):
			callers = append(callers, r)
		case strings.Contains(r.Source, "graph") && a.isCalleeRelation(r):
			callees = append(callees, r)
		case a.isTypeDefinition(r):
			types = append(types, r)
		case strings.Contains(r.Source, "dataflow"):
			dataflow = append(dataflow, r)
		case a.isImportRelation(r):
			imports = append(imports, r)
		default:
			general = append(general, r)
		}
	}

	// Map categories to template sections
	categoryMap := map[string][]*RetrievalResult{
		"Target Function":   targets,
		"Target Symbol":     targets,
		"Entry Point":       targets,
		"Module Overview":   targets,
		"Relevant Code":     targets,
		"Called Functions":  callees,
		"Call Chain":        callees,
		"Callers":           callers,
		"Direct Callers":    callers,
		"Indirect Callers":  callers,
		"Type Definitions":  types,
		"Implementations":   types,
		"Public API":        targets,
		"Data Flow":         dataflow,
		"Data Flow Sources": dataflow,
		"Dependencies":      imports,
		"Related Constants": general,
		"Internal Helpers":  general,
		"Error Handling":    general,
		"Context":           general,
		"Related Symbols":   general,
	}

	for _, spec := range config.Sections {
		items, ok := categoryMap[spec.Title]
		if !ok || len(items) == 0 {
			// If no specific category matches, use general results as fallback
			if len(general) > 0 {
				items = general
			} else {
				continue
			}
		}

		content := a.formatSectionContent(items, spec.MaxLines)
		tokens := a.estimateTokens(content)

		sections = append(sections, ContextSection{
			Title:    spec.Title,
			Content:  content,
			Tokens:   tokens,
			Priority: spec.Priority,
		})
	}

	return sections
}

// formatSectionContent formats retrieval results into readable content.
func (a *ContextAssembler) formatSectionContent(results []*RetrievalResult, maxLines int) string {
	seen := make(map[string]bool) // Deduplicate by file:line
	var lines []string

	for _, r := range results {
		key := fmt.Sprintf("%s:%d", r.File, r.Line)
		if seen[key] {
			continue
		}
		seen[key] = true

		// Format: file:line symbol_name (kind) [score]
		header := fmt.Sprintf("// %s:%d %s (%s)", r.File, r.Line, r.Name, r.Kind)

		if r.Content != "" {
			// Trim content to maxLines
			contentLines := strings.Split(r.Content, "\n")
			if len(contentLines) > maxLines {
				contentLines = contentLines[:maxLines]
				contentLines = append(contentLines, "// ... (truncated)")
			}
			lines = append(lines, header)
			lines = append(lines, contentLines...)
		} else {
			lines = append(lines, header)
		}
		lines = append(lines, "") // Blank line separator
	}

	return strings.Join(lines, "\n")
}

// applyBudget trims sections to fit within the token budget,
// preserving higher-priority sections first.
func (a *ContextAssembler) applyBudget(sections []ContextSection, maxTokens int) []ContextSection {
	if maxTokens <= 0 {
		return sections
	}

	// Sort sections by priority (highest first) for budget allocation
	sorted := make([]ContextSection, len(sections))
	copy(sorted, sections)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	// Allocate budget proportionally to priority
	totalPriority := 0
	for _, sec := range sorted {
		totalPriority += sec.Priority
	}

	var result []ContextSection
	remainingBudget := maxTokens

	for _, sec := range sorted {
		if remainingBudget <= 0 {
			break
		}

		// Calculate proportional budget for this section
		sectionBudget := maxTokens * sec.Priority / totalPriority
		if sectionBudget > remainingBudget {
			sectionBudget = remainingBudget
		}

		if sec.Tokens <= sectionBudget {
			// Section fits within budget
			result = append(result, sec)
			remainingBudget -= sec.Tokens
		} else {
			// Trim section content to fit budget
			maxChars := int(float64(sectionBudget) / a.maxTokensPerChar)
			if maxChars < len(sec.Content) {
				sec.Content = sec.Content[:maxChars] + "\n// ... (budget trimmed)"
			}
			sec.Tokens = a.estimateTokens(sec.Content)
			result = append(result, sec)
			remainingBudget -= sec.Tokens
		}
	}

	return result
}

// buildSources creates source attribution entries from results.
func (a *ContextAssembler) buildSources(results []*RetrievalResult) []ContextSource {
	sources := make([]ContextSource, 0, len(results))
	seen := make(map[int64]bool)

	for _, r := range results {
		if seen[r.NodeID] {
			continue
		}
		seen[r.NodeID] = true
		sources = append(sources, ContextSource{
			NodeID: r.NodeID,
			Name:   r.Name,
			File:   r.File,
			Score:  r.Score,
			Source: r.Source,
		})
	}
	return sources
}

// estimateTokens provides a rough token count estimate.
func (a *ContextAssembler) estimateTokens(text string) int {
	return int(float64(len(text)) * a.maxTokensPerChar)
}

// isCallerRelation checks if a result represents a caller relationship.
func (a *ContextAssembler) isCallerRelation(r *RetrievalResult) bool {
	return strings.Contains(r.Source, "called_by") ||
		(r.Kind == "function" && r.Depth > 0)
}

// isCalleeRelation checks if a result represents a callee relationship.
func (a *ContextAssembler) isCalleeRelation(r *RetrievalResult) bool {
	return strings.Contains(r.Source, "calls") ||
		(r.Kind == "function" && r.Depth > 0 && !a.isCallerRelation(r))
}

// isTypeDefinition checks if a result is a type definition.
func (a *ContextAssembler) isTypeDefinition(r *RetrievalResult) bool {
	return r.Kind == "type" || r.Kind == "interface" || r.Kind == "struct" ||
		r.Kind == "class" || r.Kind == "enum"
}

// isImportRelation checks if a result is an import relationship.
func (a *ContextAssembler) isImportRelation(r *RetrievalResult) bool {
	return strings.Contains(r.Source, "import") ||
		strings.Contains(r.Source, "contains")
}