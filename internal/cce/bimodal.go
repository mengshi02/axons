package cce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// maxCharsPerText limits the character count for a single embedding input text.
// Most embedding models support 512-8192 tokens; for code text,
// token ratio is ~3-4 chars/token. Use 8000 chars as a generous safety margin.
const maxCharsPerText = 8000

// charsPerToken is the approximate character-to-token ratio for code text.
// Code tends to have ~2.5 chars per token on average (identifiers, operators,
// keywords are short tokens). We use 2.5 for chunk sizing decisions — close
// to the typical value so we don't waste token capacity.
// For hard safety caps (truncation fallback), we use charsPerTokenMin=2.0
// which is the lower bound to guarantee we never exceed the token budget.
const charsPerToken = 2.5

// charsPerTokenMin is the conservative lower bound of chars/token ratio.
// Used for hard safety caps where exceeding the token budget would cause
// API errors. At 2.0 chars/token, a 512-token budget allows ~1024 chars,
// which guarantees actual tokens stay under 512 even for identifier-heavy code.
const charsPerTokenMin = 2.0

// defaultMaxContextTokens is the fallback n_ctx when not configured.
// This represents the model's context window size (max tokens per input).
// We use 512 as a conservative default that works safely with all engines,
// including llama-server with n_batch=512. Users with larger context windows
// (e.g. OpenAI 8192, jina 8192) should set MaxContextTokens explicitly.
const defaultMaxContextTokens = 512

// chunkOverlapRatio is the fraction of each chunk that overlaps with the
// previous chunk. 0.15 (15%) provides enough overlap to preserve cross-boundary
// semantics without excessive redundancy.
const chunkOverlapRatio = 0.15

// tokenBudgetForModel determines the per-text max token count (n_ctx).
// This is the model's context window — a universal concept across all
// inference engines. It controls the maximum tokens for a single embedding
// input, which is used for chunking decisions.
// Fallback strategy:
//  1. Config: use user-configured maxContextTokens from DB settings
//  2. Default: fallback to defaultMaxContextTokens (8192)
func (b *BimodalEmbedder) tokenBudgetForModel(_ context.Context, _ embedding.Embedder) int {
	// Level 1: Try user config
	b.mu.Lock()
	cfgMaxCtx := b.maxContextTokens
	b.mu.Unlock()
	if cfgMaxCtx > 0 {
		logger.S().Debugw("[CCE] token budget from config", "max_context_tokens", cfgMaxCtx)
		return cfgMaxCtx
	}

	// Level 2: Default fallback
	logger.S().Debugw("[CCE] token budget using default", "budget", defaultMaxContextTokens)
	return defaultMaxContextTokens
}

// BimodalEmbedder generates both description-mode and code-mode embeddings.
// It extends the existing embedding pipeline with source code vectorization,
// enabling semantic search over actual code content, not just metadata.
type BimodalEmbedder struct {
	store            *Store
	embedder         embedding.Embedder
	rootPath         string
	maxContextTokens int // user-configured n_ctx (0 = auto-detect from API)
	mu               sync.Mutex
}

// NewBimodalEmbedder creates a new bimodal embedding service.
func NewBimodalEmbedder(store *Store, embedder embedding.Embedder, rootPath string) *BimodalEmbedder {
	return &BimodalEmbedder{
		store:    store,
		embedder: embedder,
		rootPath: rootPath,
	}
}

// SetMaxContextTokens sets the user-configured max context tokens (n_ctx).
// Set to 0 to auto-detect from the model API.
func (b *BimodalEmbedder) SetMaxContextTokens(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maxContextTokens = n
}

// SetEmbedder updates the embedder.
func (b *BimodalEmbedder) SetEmbedder(embedder embedding.Embedder) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.embedder = embedder
}

// EmbeddingProgress tracks the progress of a bimodal embedding operation.
type EmbeddingProgress struct {
	Mode         EmbeddingMode `json:"mode"`
	Status       string        `json:"status"`
	Current      int           `json:"current"`
	Total        int           `json:"total"`
	NewCount     int           `json:"new_count"`
	UpdatedCount int           `json:"updated_count"`
	Error        string        `json:"error,omitempty"`
}

// GenerateDualEmbeddings generates both description and code embeddings for nodes.
// If force is true, it regenerates all embeddings; otherwise only missing ones.
func (b *BimodalEmbedder) GenerateDualEmbeddings(
	ctx context.Context,
	nodeIDs []int64,
	force bool,
	mode EmbeddingMode,
	progressCh chan<- EmbeddingProgress,
) (*EmbeddingProgress, error) {
	b.mu.Lock()
	if b.embedder == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("embedder not configured")
	}
	embedder := b.embedder
	b.mu.Unlock()

	progress := &EmbeddingProgress{
		Mode:   mode,
		Status: "running",
	}

	total := len(nodeIDs)
	progress.Total = total

	batchSize := 10 // Smaller batches for code embeddings (larger texts)
	vecCodeTableEnsured := false

	for i := 0; i < total; i += batchSize {
		select {
		case <-ctx.Done():
			progress.Status = "canceled"
			return progress, nil
		default:
		}

		end := i + batchSize
		if end > total {
			end = total
		}
		batch := nodeIDs[i:end]

		// Step 1: Extract code chunks and build embedding texts
		chunks, descTexts, codeTexts, nodeSignatures, err := b.extractTextsForBatch(batch)
		if err != nil {
			progress.Error = err.Error()
			progress.Status = "error"
			return progress, fmt.Errorf("failed to extract texts for batch %d-%d: %w", i, end, err)
		}

		// Step 2: Store code chunks
		if len(chunks) > 0 {
			if err := b.store.StoreCodeChunks(chunks); err != nil {
				// Log but continue - chunks are optional, embeddings are primary
				logger.S().Warnw("[CCE] failed to store code chunks", "error", err, "chunk_count", len(chunks))
			}
		}

		// Step 3: Generate embeddings based on mode
		switch mode {
		case ModeDescription:
			err = b.generateDescriptionEmbeddings(ctx, embedder, batch, descTexts, &vecCodeTableEnsured)
		case ModeCode:
			err = b.generateCodeEmbeddings(ctx, embedder, batch, codeTexts, nodeSignatures, &vecCodeTableEnsured)
		case ModeDual:
			err = b.generateDualEmbeddingBatch(ctx, embedder, batch, descTexts, codeTexts, nodeSignatures, &vecCodeTableEnsured)
		default:
			err = b.generateDualEmbeddingBatch(ctx, embedder, batch, descTexts, codeTexts, nodeSignatures, &vecCodeTableEnsured)
		}

		if err != nil {
			progress.Error = err.Error()
			progress.Status = "error"
			return progress, err
		}

		progress.Current = end
		progress.NewCount = end // Simplified tracking
		if progressCh != nil {
			progressCh <- *progress
		}
	}

	progress.Status = "complete"
	progress.Current = total
	return progress, nil
}

// extractTextsForBatch extracts description and code texts for a batch of node IDs.
// Returns: code chunks, description texts, code texts, node signatures (qualified_name), error.
func (b *BimodalEmbedder) extractTextsForBatch(nodeIDs []int64) ([]*CodeChunk, []string, []string, []string, error) {
	if len(nodeIDs) == 0 {
		return nil, nil, nil, nil, nil
	}

	// Query node metadata
	placeholders := make([]string, len(nodeIDs))
	args := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, name, kind, file, line, end_line, qualified_name,
		       COALESCE(docstring, '') as docstring
		FROM nodes
		WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := b.store.DB().Query(query, args...)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()

	type nodeInfo struct {
		id       int64
		name     string
		kind     string
		file     string
		line     int
		endLine  int
		qualName string
		docstring string
	}

	nodeMap := make(map[int64]*nodeInfo)
	for rows.Next() {
		info := &nodeInfo{}
		if err := rows.Scan(&info.id, &info.name, &info.kind, &info.file,
			&info.line, &info.endLine, &info.qualName, &info.docstring); err != nil {
			return nil, nil, nil, nil, err
		}
		nodeMap[info.id] = info
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}

	// Build texts and chunks
	var chunks []*CodeChunk
	descTexts := make([]string, len(nodeIDs))
	codeTexts := make([]string, len(nodeIDs))
	nodeSignatures := make([]string, len(nodeIDs))

	for i, id := range nodeIDs {
		info, ok := nodeMap[id]
		if !ok {
			descTexts[i] = ""
			codeTexts[i] = ""
			nodeSignatures[i] = ""
			continue
		}

		// Node signature for context enrichment in chunking
		nodeSignatures[i] = buildNodeSignature(info.kind, info.qualName)

		// Description text (existing logic from embedding service)
		descTexts[i] = buildDescriptionText(info.name, info.kind, info.file, info.qualName, info.docstring)

		// Code text: extract source code
		codeContent, language := b.extractSourceCode(info.file, info.line, info.endLine)
		if codeContent != "" {
			chunks = append(chunks, &CodeChunk{
				NodeID:      id,
				File:        info.file,
				StartLine:   info.line,
				EndLine:     info.endLine,
				Content:     codeContent,
				Language:    language,
				ContentHash: computeHash(codeContent),
			})
			codeTexts[i] = codeContent
		} else {
			// Fallback: use description text if source code is unavailable
			codeTexts[i] = descTexts[i]
		}
	}

	return chunks, descTexts, codeTexts, nodeSignatures, nil
}

// buildNodeSignature creates a human-readable signature for context enrichment.
// Format: "func pkg.FuncName" or "type pkg.TypeName" etc.
func buildNodeSignature(kind, qualName string) string {
	if qualName == "" {
		return ""
	}
	// Map graph node kinds to readable prefixes
	prefix := kind
	switch kind {
	case "function", "method":
		prefix = "func"
	case "type", "class", "struct", "interface":
		prefix = "type"
	case "variable", "field":
		prefix = "var"
	case "constant":
		prefix = "const"
	case "package", "module":
		prefix = "pkg"
	}
	if prefix == "" {
		return qualName
	}
	return prefix + " " + qualName
}

// generateDescriptionEmbeddings generates description-mode embeddings only.
func (b *BimodalEmbedder) generateDescriptionEmbeddings(
	ctx context.Context,
	embedder embedding.Embedder,
	nodeIDs []int64,
	texts []string,
	_ *bool,
) error {
	// Filter out empty texts
	var validIDs []int64
	var validTexts []string
	for i, t := range texts {
		if t != "" {
			validIDs = append(validIDs, nodeIDs[i])
			// Truncate overly long texts to fit embedding model limits
			if len(t) > maxCharsPerText {
				t = t[:maxCharsPerText]
			}
			validTexts = append(validTexts, t)
		}
	}
	if len(validTexts) == 0 {
		return nil
	}

	// Use adaptive batch embedding to handle token budget limits
	vectors, err := b.embedWithAdaptiveBatch(ctx, embedder, validTexts)
	if err != nil {
		return fmt.Errorf("description embedding failed: %w", err)
	}

	model := embedder.ModelName()
	for j, id := range validIDs {
		if j < len(vectors) {
			// Store in existing embeddings table via the repository
			// This is handled by the existing EmbeddingService, so we skip here
			_ = id
			_ = model
		}
	}
	return nil
}

// generateCodeEmbeddings generates code-mode embeddings with chunking support.
// For texts that exceed the model's n_ctx, it splits them into overlapping chunks
// with context enrichment, embeds each chunk independently, and stores all vectors.
func (b *BimodalEmbedder) generateCodeEmbeddings(
	ctx context.Context,
	embedder embedding.Embedder,
	nodeIDs []int64,
	texts []string,
	nodeSignatures []string,
	vecTableEnsured *bool,
) error {
	// Determine n_ctx for chunking decisions
	nCtx := b.tokenBudgetForModel(ctx, embedder)

	// Phase 1: Chunk all texts
	type chunkInfo struct {
		nodeID     int64
		chunkIndex int
		text       string
	}
	var allChunks []chunkInfo

	for i, t := range texts {
		if t == "" {
			continue
		}
		sig := ""
		if i < len(nodeSignatures) {
			sig = nodeSignatures[i]
		}
		chunks := chunkText(t, sig, nCtx)
		if len(chunks) > 1 {
			logger.S().Debugw("[CCE] text chunked", "node_id", nodeIDs[i], "original_tokens", estimateTokens(t), "chunks", len(chunks), "n_ctx", nCtx)
		}
		for _, ch := range chunks {
			allChunks = append(allChunks, chunkInfo{
				nodeID:     nodeIDs[i],
				chunkIndex: ch.Index,
				text:       ch.Text,
			})
		}
	}

	if len(allChunks) == 0 {
		return nil
	}

	// Phase 2: Collect all chunk texts for batch embedding
	chunkTexts := make([]string, len(allChunks))
	for i, c := range allChunks {
		chunkTexts[i] = c.text
	}

	// Phase 3: Embed all chunks using adaptive batch
	vectors, err := b.embedWithAdaptiveBatch(ctx, embedder, chunkTexts)
	if err != nil {
		return fmt.Errorf("code embedding failed: %w", err)
	}

	// Ensure vec_code_embeddings table
	if !*vecTableEnsured && len(vectors) > 0 {
		for _, v := range vectors {
			if len(v) > 0 {
				dim := len(v)
				if ensureErr := b.store.EnsureVecCodeTable(dim); ensureErr != nil {
					logger.S().Warnw("[CCE] EnsureVecCodeTable failed", "dim", dim, "error", ensureErr)
				}
				*vecTableEnsured = true
				break
			}
		}
	}

	// Phase 4: Store all vectors
	model := embedder.ModelName()
	for j := range allChunks {
		if j < len(vectors) && vectors[j] != nil {
			if err := b.store.StoreCodeEmbedding(allChunks[j].nodeID, allChunks[j].chunkIndex, vectors[j], model, allChunks[j].text); err != nil {
				logger.S().Warnw("[CCE] failed to store code embedding",
					"node_id", allChunks[j].nodeID, "chunk_index", allChunks[j].chunkIndex, "error", err)
			}
		}
	}
	return nil
}

// generateDualEmbeddingBatch generates both description and code embeddings.
func (b *BimodalEmbedder) generateDualEmbeddingBatch(
	ctx context.Context,
	embedder embedding.Embedder,
	nodeIDs []int64,
	descTexts []string,
	codeTexts []string,
	nodeSignatures []string,
	vecCodeTableEnsured *bool,
) error {
	// Generate code-mode embeddings (description embeddings are handled by existing service)
	return b.generateCodeEmbeddings(ctx, embedder, nodeIDs, codeTexts, nodeSignatures, vecCodeTableEnsured)
}

// embedWithAdaptiveBatch generates embeddings with adaptive batch splitting.
// Texts should already be chunked to fit within n_ctx by the caller.
// This function groups texts into sub-batches by token count for efficient
// API calls, and retries individual texts on batch failures.
func (b *BimodalEmbedder) embedWithAdaptiveBatch(
	ctx context.Context,
	embedder embedding.Embedder,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([][]float32, len(texts))

	// Determine token budget based on model's n_ctx (API > config > default)
	tokenBudget := b.tokenBudgetForModel(ctx, embedder)
	logger.S().Debugw("[CCE] adaptive batch embedding", "model", embedder.ModelName(), "token_budget", tokenBudget)

	// Phase 1: Group texts into sub-batches by token budget
	groups := b.groupByTokenBudget(texts, tokenBudget)

	for _, group := range groups {
		batch := texts[group.start:group.end]

		// Group within budget — try batch embed
		vectors, err := embedder.Embed(ctx, batch)
		if err == nil {
			for i, v := range vectors {
				result[group.start+i] = v
			}
			continue
		}

		// Batch failed — if it's a single text, try progressive truncation
		if group.start+1 == group.end {
			// Try truncating the text with progressively smaller sizes.
			// Start with the conservative charsPerTokenMin to maximize success rate.
			maxCharsBudget := int(float64(tokenBudget) * charsPerTokenMin)
			truncated := batch[0]
			for truncStep := 0; truncStep < 3; truncStep++ {
				if len(truncated) <= maxCharsBudget {
					break
				}
				truncated = truncated[:maxCharsBudget]
				vec, truncErr := embedder.Embed(ctx, []string{truncated})
				if truncErr == nil && len(vec) > 0 {
					result[group.start] = vec[0]
					logger.S().Infow("[CCE] embed succeeded after truncation fallback",
						"index", group.start, "original_len", len(batch[0]), "truncated_len", len(truncated), "step", truncStep)
					break
				}
				// Still failed — reduce budget by 25% and retry
				logger.S().Warnw("[CCE] truncation still too large, reducing",
					"index", group.start, "chars_budget", maxCharsBudget, "step", truncStep, "error", truncErr)
				maxCharsBudget = maxCharsBudget * 3 / 4
			}
			if result[group.start] == nil {
				logger.S().Warnw("[CCE] single text embed failed after truncation attempts, skipping",
					"index", group.start, "text_len", len(batch[0]), "error", err)
			}
			continue
		}

		// Batch failed with multiple texts — retry each individually
		logger.S().Warnw("[CCE] embed batch failed, retrying texts individually",
			"batch_start", group.start, "batch_end", group.end,
			"estimated_tokens", group.tokens, "budget", tokenBudget,
			"error", err)

		for i, t := range batch {
			vec, singleErr := embedder.Embed(ctx, []string{t})
			if singleErr != nil {
				// Try progressive truncation fallback.
				// Start with the conservative charsPerTokenMin to maximize success rate.
				maxCharsBudget := int(float64(tokenBudget) * charsPerTokenMin)
				truncated := t
				success := false
				for truncStep := 0; truncStep < 3; truncStep++ {
					if len(truncated) <= maxCharsBudget {
						break
					}
					truncated = truncated[:maxCharsBudget]
					vec, singleErr = embedder.Embed(ctx, []string{truncated})
					if singleErr == nil && len(vec) > 0 {
						result[group.start+i] = vec[0]
						success = true
						break
					}
					// Still failed — reduce budget by 25% and retry
					maxCharsBudget = maxCharsBudget * 3 / 4
				}
				if !success {
					logger.S().Warnw("[CCE] single text embed failed after truncation attempts, skipping",
						"index", group.start+i, "text_len", len(t), "error", singleErr)
				}
				continue
			}
			if len(vec) > 0 {
				result[group.start+i] = vec[0]
			}
		}
	}

	return result, nil
}

// tokenGroup represents a contiguous range of texts that fit within token budget.
type tokenGroup struct {
	start  int // inclusive index into original texts slice
	end    int // exclusive
	tokens int // total estimated tokens
}

// groupByTokenBudget partitions texts into groups where each group's total
// estimated tokens <= tokenBudget. A single text that exceeds the budget
// gets its own group (will be truncated later).
func (b *BimodalEmbedder) groupByTokenBudget(texts []string, tokenBudget int) []tokenGroup {
	var groups []tokenGroup
	start := 0
	accumTokens := 0

	for i, t := range texts {
		tok := estimateTokens(t)

		// If adding this text would exceed budget and we already have texts in the group, close it
		if i > start && accumTokens+tok > tokenBudget {
			groups = append(groups, tokenGroup{start: start, end: i, tokens: accumTokens})
			start = i
			accumTokens = 0
		}

		accumTokens += tok
	}

	// Close last group
	if start < len(texts) {
		groups = append(groups, tokenGroup{start: start, end: len(texts), tokens: accumTokens})
	}

	return groups
}

// estimateTokens returns a rough token count estimate for a text.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return int(float64(len(text)) / charsPerToken)
}

// smartTruncate truncates code text while preserving structural integrity.
// It keeps the first signatureLen chars (function/class signature) and appends
// the tail (closing braces, return statements) if space allows.
// This preserves more semantic information than simple prefix truncation.
func smartTruncate(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	// For short budgets, just take the prefix
	const signatureLen = 200
	if maxLen <= signatureLen+100 {
		return text[:maxLen]
	}

	separator := "\n// ... (truncated) ...\n"
	sepLen := len(separator)
	tailLen := signatureLen / 2

	// Ensure total fits within maxLen: prefix + separator + tail <= maxLen
	prefixLen := maxLen - sepLen - tailLen
	if prefixLen <= 0 {
		return text[:maxLen]
	}

	// Look for the tail portion — last tailLen chars often contain
	// closing braces, return statements, etc.
	tailStart := len(text) - tailLen
	if tailStart <= prefixLen {
		// Text is short enough that prefix already covers the tail area
		return text[:maxLen]
	}

	prefix := text[:prefixLen]
	tail := text[tailStart:]
	result := prefix + separator + tail

	// Safety: ensure result doesn't exceed maxLen
	if len(result) > maxLen {
		return text[:maxLen]
	}
	return result
}

// extractSourceCode reads source code from the file system.
func (b *BimodalEmbedder) extractSourceCode(filePath string, startLine, endLine int) (string, string) {
	if filePath == "" || startLine <= 0 {
		return "", ""
	}

	// Resolve file path: if relative, join with project root
	absPath := filePath
	if !filepath.IsAbs(filePath) && b.rootPath != "" {
		absPath = filepath.Join(b.rootPath, filePath)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		// Try without rootPath as fallback
		if absPath != filePath {
			content, err = os.ReadFile(filePath)
			if err != nil {
				return "", ""
			}
		} else {
			return "", ""
		}
	}

	lines := strings.Split(string(content), "\n")
	if endLine <= 0 || endLine < startLine {
		endLine = startLine
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	result := strings.Join(lines[startLine-1:endLine], "\n")
	language := detectLanguage(filePath)
	return result, language
}

// buildDescriptionText builds the description text for embedding (mirrors EmbeddingService.buildEmbeddingText).
func buildDescriptionText(name, kind, file, qualName, docstring string) string {
	var parts []string
	if qualName != "" {
		parts = append(parts, qualName)
	} else {
		parts = append(parts, name)
	}
	if kind != "" {
		parts = append(parts, fmt.Sprintf("(%s)", kind))
	}
	if file != "" {
		parts = append(parts, fmt.Sprintf("in %s", file))
	}
	if docstring != "" {
		parts = append(parts, docstring)
	}
	return strings.Join(parts, " ")
}

// computeHash computes SHA-256 hash of content for change detection.
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// detectLanguage infers the programming language from file extension.
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".sh", ".bash":
		return "shell"
	case ".sql":
		return "sql"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}

// textChunk represents a chunk of code text with context enrichment.
type textChunk struct {
	// Index is the sequential chunk number (0-based).
	Index int
	// Text is the chunk content with context header prepended.
	Text string
	// IsOriginal indicates whether this is the unchunked original text
	// (i.e., the text fit within n_ctx and was not split).
	IsOriginal bool
}

// chunkText splits code text into chunks that fit within maxTokens.
// It uses three techniques for high precision:
//  1. Structure-aware splitting: prefers splitting at blank lines, brace-only
//     lines, or indentation changes to avoid breaking logical units.
//  2. Overlap: adjacent chunks overlap by chunkOverlapRatio of maxTokens,
//     preserving cross-boundary semantics.
//  3. Context enrichment: each chunk gets a header with the node signature
//     and chunk index, so the embedding vector carries global context.
//
// If the text fits within maxTokens, it returns a single chunk with IsOriginal=true.
func chunkText(text, nodeSignature string, maxTokens int) []textChunk {
	if text == "" || maxTokens <= 0 {
		return nil
	}

	// Reserve token budget for the context enrichment header.
	// Header format: "// [func pkg.Name — chunk 2/5]\n" ≈ 40-60 chars ≈ 15-20 tokens.
	// We reserve 30 tokens as a safety margin.
	const headerTokenReserve = 30
	effectiveMaxTokens := maxTokens - headerTokenReserve
	if effectiveMaxTokens <= 0 {
		effectiveMaxTokens = maxTokens / 2
	}

	estimatedTokens := estimateTokens(text)
	// Use 85% of maxTokens as the threshold for "fits without chunking",
	// to account for estimation inaccuracy (charsPerToken is an average;
	// code with many short identifiers can have higher actual token counts).
	if estimatedTokens <= int(float64(maxTokens)*0.85) {
		// Text fits within budget — no chunking needed.
		// Apply a hard character cap using the conservative ratio as safety net.
		maxSafeChars := int(float64(effectiveMaxTokens) * charsPerTokenMin)
		if len(text) > maxSafeChars {
			text = text[:maxSafeChars]
		}
		return []textChunk{{Index: 0, Text: text, IsOriginal: true}}
	}

	// Calculate character limits using effective budget (after header reserve)
	// Use charsPerToken (typical ratio) for sizing to maximize token utilization.
	maxChars := int(float64(effectiveMaxTokens) * charsPerToken)
	overlapChars := int(float64(maxTokens) * chunkOverlapRatio * charsPerToken)
	stepChars := maxChars - overlapChars
	if stepChars <= 0 {
		stepChars = maxChars / 2
	}

	var chunks []textChunk
	start := 0
	chunkIndex := 0

	// maxChunkChars is the absolute character limit for any chunk body
	// (excluding header). Uses the conservative charsPerTokenMin to guarantee
	// we never exceed the token budget, even for identifier-heavy code.
	// This prevents findStructureAwareSplit drift and unbounded last chunks.
	maxChunkChars := int(float64(effectiveMaxTokens) * charsPerTokenMin)

	for start < len(text) {
		end := start + maxChars
		if end >= len(text) {
			// Last chunk — take the rest, but enforce hard char limit
			chunkText := text[start:]
			if len(chunkText) > maxChunkChars {
				chunkText = chunkText[:maxChunkChars]
			}
			chunks = append(chunks, textChunk{
				Index:      chunkIndex,
				Text:       enrichChunk(chunkText, nodeSignature, chunkIndex, len(chunks)+1),
				IsOriginal: false,
			})
			break
		}

		// Find a good split point within [end-searchRadius, end+searchRadius]
		splitPos := findStructureAwareSplit(text, end, maxChars)
		// Enforce hard char limit: findStructureAwareSplit may return a
		// position beyond start+maxChars (it searches ±searchRadius).
		if splitPos-start > maxChunkChars {
			splitPos = start + maxChunkChars
		}
		chunkText := text[start:splitPos]

		chunks = append(chunks, textChunk{
			Index:      chunkIndex,
			Text:       enrichChunk(chunkText, nodeSignature, chunkIndex, -1 /* total unknown yet */),
			IsOriginal: false,
		})

		// Advance with overlap
		nextStart := splitPos - overlapChars
		if nextStart <= start {
			nextStart = splitPos // ensure progress
		}
		start = nextStart
		chunkIndex++
	}

	// Second pass: fix up total count in headers
	totalChunks := len(chunks)
	for i := range chunks {
		if !chunks[i].IsOriginal {
			// Re-enrich with correct total
			headerEnd := strings.Index(chunks[i].Text, "\n")
			if headerEnd > 0 {
				body := chunks[i].Text[headerEnd+1:]
				chunks[i].Text = enrichChunk(body, nodeSignature, i, totalChunks)
			}
		}
	}

	return chunks
}

// findStructureAwareSplit finds a good position to split text near targetPos.
// It searches within a window around targetPos for structural boundaries:
//  1. Blank lines (best — separates logical blocks)
//  2. Lines with only braces (good — end of blocks)
//  3. Indentation decrease (acceptable — end of nested block)
//  4. Any newline (fallback — at least don't break mid-line)
//
// If no good split point is found, returns targetPos as-is.
func findStructureAwareSplit(text string, targetPos, maxChars int) int {
	searchRadius := maxChars / 10 // 10% search window
	if searchRadius < 200 {
		searchRadius = 200
	}

	searchStart := targetPos - searchRadius
	if searchStart < 0 {
		searchStart = 0
	}
	searchEnd := targetPos + searchRadius
	if searchEnd > len(text) {
		searchEnd = len(text)
	}

	// Find all newline positions in the search window
	type splitCandidate struct {
		pos   int
		score int // higher is better
	}
	var candidates []splitCandidate

	for i := searchStart; i < searchEnd; i++ {
		if text[i] != '\n' {
			continue
		}
		// Check the line that ends at this newline
		lineStart := findLineStart(text, i)
		line := strings.TrimSpace(text[lineStart:i])

		score := 0
		if line == "" {
			score = 3 // blank line — best split point
		} else if line == "}" || line == "});" || line == "}," || line == ");" {
			score = 2 // closing brace — good split point
		} else if line == "{" || line == "} else {" || line == "} else if" {
			score = 0 // opening constructs — bad split point
		} else {
			// Check indentation: less indentation = higher scope = better split
			indent := len(text[lineStart:i]) - len(strings.TrimLeft(text[lineStart:i], " \t"))
			if indent == 0 {
				score = 1 // top-level statement — acceptable
			}
		}

		if score > 0 {
			// Prefer candidates closer to targetPos
			distance := i - targetPos
			if distance < 0 {
				distance = -distance
			}
			// Penalty for distance (as fraction of searchRadius)
			penalty := float64(distance) / float64(searchRadius)
			effectiveScore := float64(score) - penalty*0.5
			candidates = append(candidates, splitCandidate{pos: i + 1, score: int(effectiveScore * 100)})
		}
	}

	// Pick the best candidate
	bestPos := targetPos
	bestScore := -1
	for _, c := range candidates {
		if c.score > bestScore {
			bestScore = c.score
			bestPos = c.pos
		}
	}

	return bestPos
}

// findLineStart finds the start position of the line containing position pos.
func findLineStart(text string, pos int) int {
	for i := pos - 1; i >= 0; i-- {
		if text[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// enrichChunk prepends a context header to a chunk, providing global context
// for the embedding model. This ensures each chunk's vector carries information
// about which function/class it belongs to, improving search precision.
//
// Example header: "// [func processOrder(order Order) error — chunk 2/5]"
//
// The header cost is ~20-40 tokens, which is negligible compared to the
// semantic benefit.
func enrichChunk(chunkText, nodeSignature string, chunkIndex, totalChunks int) string {
	if nodeSignature == "" {
		return chunkText
	}
	var header string
	if totalChunks > 0 {
		header = fmt.Sprintf("// [%s — chunk %d/%d]", nodeSignature, chunkIndex+1, totalChunks)
	} else {
		header = fmt.Sprintf("// [%s — chunk %d]", nodeSignature, chunkIndex+1)
	}
	return header + "\n" + chunkText
}