// Package api provides HTTP API handlers for the axons daemon.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/cce"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/extractors"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/internal/notification"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/task"
	"github.com/mengshi02/axons/pkg/clients/embedding"
	"github.com/mengshi02/axons/pkg/types"
)

// handleBuild handles graph build requests.
func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Create task for async build
	t := s.taskMgr.CreateTask("build")
	taskID := t.ID

	// Associate project ID with the task so build-status API can find it
	if req.ProjectID != "" {
		t.Labels = task.Map{"project_id": req.ProjectID}
	}

	go func() {
		ctx := t.Context()
		t.SetStatus(task.StatusRunning)

		opts := &types.BuildOptions{
			RootDir:         req.RootDir,
			FullBuild:       req.FullBuild,
			ExcludePatterns: req.ExcludePatterns,
			IncludeDataflow: req.IncludeDataflow,
			IncludeAST:      req.IncludeAST,
			ProjectID:       req.ProjectID,
			ProjectName:     req.ProjectName,
			MaxFileSize:     req.MaxFileSize,
		}

		// Use project-specific DB if project ID is provided
		pipelineRepo := s.repo
		if req.ProjectID != "" {
			if pr, err := s.projectRepo(req.ProjectID); err == nil {
				pipelineRepo = pr
			}
		}

		pipeline := graph.NewPipelineWithGlobal(pipelineRepo, s.globalRepo, opts)

		// Resolve project ID early for progress callbacks
		projectID := req.ProjectID
		if projectID == "" && req.RootDir != "" {
			projects, _ := s.globalRepo.ListProjects()
			for _, p := range projects {
				if p.RootPath == req.RootDir {
					projectID = p.ID
					break
				}
			}
		}

		pipeline.SetOnProgress(func(phase string, percent int, message string) {
			s.eventBroker.BroadcastBuildProgress(taskID, percent, message, projectID, phase)
		})
		pipeline.SetOnDelta(func(stage string, nodes []*types.Node, edges []*types.Edge) {
			// Convert nodes/edges to JSON-friendly maps for SSE
			nodeMaps := make([]map[string]interface{}, 0, len(nodes))
			for _, n := range nodes {
				nodeMaps = append(nodeMaps, map[string]interface{}{
					"id":              fmt.Sprintf("%d", n.ID),
					"name":            n.Name,
					"kind":            string(n.Kind),
					"file":            n.File,
					"line":            n.Line,
					"qualified_name":  n.QualifiedName,
					"exported":        n.Exported,
				})
			}
			edgeMaps := make([]map[string]interface{}, 0)
			for _, e := range edges {
				edgeMaps = append(edgeMaps, map[string]interface{}{
					"id":       fmt.Sprintf("%d", e.ID),
					"source":   fmt.Sprintf("%d", e.SourceID),
					"target":   fmt.Sprintf("%d", e.TargetID),
					"kind":     string(e.Kind),
				})
			}
			s.eventBroker.BroadcastBuildDelta(taskID, projectID, stage, nodeMaps, edgeMaps)
		})
		result, err := pipeline.Build(ctx)
		if err != nil {
			t.SetError(err.Error())
			// Send notification for build failure
			if s.notificationService != nil {
				s.notificationService.Create(context.Background(), &notification.Notification{
					Source:  "host",
					Type:    "error",
					Title:   "Build Failed",
					Message: err.Error(),
				})
			}
			return
		}

		t.SetResult(map[string]interface{}{
			"files_parsed":  result.FilesParsed,
			"nodes_created": result.NodesCreated,
			"edges_created": result.EdgesCreated,
			"changed_files": len(result.ChangedFiles),
			"removed_files": len(result.RemovedFiles),
		})

		// Broadcast build complete
		s.eventBroker.BroadcastBuildComplete(taskID, projectID, result.FilesParsed, result.NodesCreated, result.EdgesCreated, result.ChangedFiles, result.RemovedFiles, result.ChangedFileOldNodeIDs, result.ChangedFileOldEdgeIDs)

		// Send notification for build complete
		if s.notificationService != nil {
			s.notificationService.Create(context.Background(), &notification.Notification{
				Source:  "host",
				Type:    "success",
				Title:   "Build Complete",
				Message: fmt.Sprintf("%d nodes, %d edges created", result.NodesCreated, result.EdgesCreated),
			})
		}

		// Auto-trigger embedding if configured
		if result.NodesCreated > 0 || len(result.ChangedFiles) > 0 {
			go s.triggerAutoEmbedding(projectID)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"task_id": taskID,
		"status":  "pending",
	})
}

// triggerAutoEmbedding triggers automatic embedding generation if configured.
func (s *Server) triggerAutoEmbedding(projectID string) {
	// Check if embedding is configured
	configured, err := s.repo.IsEmbeddingConfigured()
	if err != nil || !configured {
		return
	}

	// Check if auto-embedding is enabled
	enabled, _ := s.repo.GetSetting("embedding_enabled")
	if enabled != "true" {
		return
	}

	// Always recreate embedder from current settings so dimension is up-to-date.
	{
		embedder := s.createEmbedder()
		if embedder == nil {
			return
		}
		s.embeddingSvc.SetEmbedder(embedder)
		config, _ := s.repo.GetEmbeddingConfig()
		s.embeddingSvc.SetModelConfig(config)
	}

	// Create a background context for auto-embedding
	ctx := context.Background()

	// Progress callback
	progressChan := make(chan service.Progress, 10)
	go func() {
		for prog := range progressChan {
			s.eventBroker.BroadcastEmbedProgress("auto", projectID, prog.Current, prog.Total, string(prog.Status))
		}
	}()

	// Use project-specific repo if project ID is available
	var embedRepo *repository.Repository
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			embedRepo = pr
		}
	}

	// Run embedding for nodes without embeddings (incremental)
	prog, err := s.embeddingSvc.GenerateEmbeddingsWithRepo(ctx, embedRepo, false, nil, progressChan)
	close(progressChan)

	if err != nil {
		fmt.Printf("Auto-embedding failed: %v\n", err)
		return
	}

	if prog != nil && prog.Status == service.StatusComplete {
		s.eventBroker.BroadcastEmbedComplete("auto", projectID, prog.Total, prog.NewCount, prog.UpdatedCount)
	}
}

// handleQuery handles graph query requests.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	// Use project-specific repo if project ID is provided
	queryRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			queryRepo = pr
		}
	}

	// Query nodes by name
	nodes, err := queryRepo.FindNodesByName(req.Name, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Query failed: %v", err))
		return
	}

	if len(nodes) == 0 {
		writeJSON(w, http.StatusOK, &QueryResponse{Nodes: []NodeInfo{}})
		return
	}

	// Convert nodes
	result := &QueryResponse{
		Nodes: make([]NodeInfo, len(nodes)),
	}

	for i, node := range nodes {
		result.Nodes[i] = NodeInfo{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
			Exported:      node.Exported,
			Visibility:    string(node.Visibility),
		}
	}

	// Get callers/callees for first result if requested
	if len(nodes) > 0 {
		node := nodes[0]
		if req.Callers {
			callers, err := queryRepo.FindCallers(node.ID)
			if err == nil && len(callers) > 0 {
				result.Callers = make([]NodeInfo, len(callers))
				for j, c := range callers {
					result.Callers[j] = NodeInfo{
						ID:   c.ID,
						Name: c.Name,
						Kind: string(c.Kind),
						File: c.File,
						Line: c.Line,
					}
				}
			}
		}

		if req.Callees {
			callees, err := queryRepo.FindCallees(node.ID)
			if err == nil && len(callees) > 0 {
				result.Callees = make([]NodeInfo, len(callees))
				for j, c := range callees {
					result.Callees[j] = NodeInfo{
						ID:   c.ID,
						Name: c.Name,
						Kind: string(c.Kind),
						File: c.File,
						Line: c.Line,
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// createEmbedder creates an embedder based on database configuration.
// Falls back to environment variables if database config is not set.
func (s *Server) createEmbedder() embedding.Embedder {
	// Try to get config from global settings database
	config, err := s.globalRepo.GetEmbeddingConfig()
	fmt.Printf("info: createEmbedder config=%v err=%v\n", config, err)
	if err == nil && config != nil {
		provider := config["embedding_provider"]
		if provider != "" {
			apiKey := config["embedding_api_key"]
			model := config["embedding_model"]
			baseURL := config["embedding_base_url"]
			
			// Parse dimension from config
			var dimension int
			if dimStr := config["embedding_dimension"]; dimStr != "" {
				if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
					dimension = d
					fmt.Printf("info: createEmbedder parsed dimension=%d from config\n", dimension)
				} else {
					fmt.Printf("info: createEmbedder failed to parse dimension: dimStr=%q err=%v\n", dimStr, err)
				}
			} else {
				fmt.Printf("info: createEmbedder no embedding_dimension in config\n")
			}

			switch provider {
			case "openai":
				if apiKey != "" {
					if model == "" {
						model = "text-embedding-3-small"
					}
					e := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
						APIKey:    apiKey,
						Model:     model,
						Dimension: dimension,
					})
					fmt.Printf("info: createEmbedder => openai model=%s dim=%d\n", model, e.EmbeddingDimension())
					return e
				}
			case "ollama":
				if baseURL == "" {
					baseURL = "http://localhost:11434/v1"
				}
				if model == "" {
					model = "nomic-embed-text"
				}
				e := embedding.NewOllamaEmbedder(baseURL, model)
				fmt.Printf("info: createEmbedder => ollama model=%s dim=%d\n", model, e.EmbeddingDimension())
				return e
			case "jina":
				if apiKey != "" {
					if baseURL == "" {
						baseURL = "https://api.jina.ai/v1"
					}
					if model == "" {
						model = "jina-embeddings-v2-base-en"
					}
					e := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
						APIKey:    apiKey,
						BaseURL:   baseURL,
						Model:     model,
						Dimension: dimension,
					})
					fmt.Printf("info: createEmbedder => jina model=%s dim=%d\n", model, e.EmbeddingDimension())
					return e
				}
			case "custom":
				if baseURL != "" {
					e := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
						APIKey:    apiKey,
						BaseURL:   baseURL,
						Model:     model,
						Dimension: dimension,
					})
					fmt.Printf("info: createEmbedder => custom model=%s dim=%d (config dimension=%d)\n", model, e.EmbeddingDimension(), dimension)
					return e
				}
			}
		}
	}

	// Fall back to environment variables
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		model := os.Getenv("OPENAI_EMBED_MODEL")
		if model == "" {
			model = "text-embedding-3-small"
		}
		e := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey: apiKey,
			Model:  model,
		})
		fmt.Printf("info: createEmbedder => fallback openai model=%s dim=%d\n", model, e.EmbeddingDimension())
		return e
	}

	// Fall back to Ollama
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	model := os.Getenv("OLLAMA_EMBED_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}
	e := embedding.NewOllamaEmbedder(baseURL, model)
	fmt.Printf("info: createEmbedder => fallback ollama model=%s dim=%d\n", model, e.EmbeddingDimension())
	return e
}

// isTestFile checks if a file path is a test file.
func isTestFile(file string) bool {
	return strings.HasSuffix(file, "_test.go") ||
		strings.HasSuffix(file, ".test.ts") ||
		strings.HasSuffix(file, ".spec.ts") ||
		strings.HasSuffix(file, "_test.py") ||
		strings.HasSuffix(file, ".test.js") ||
		strings.HasSuffix(file, ".spec.js")
}

// handleSearch handles search requests.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Set defaults
	mode := req.Mode
	if mode == "" {
		mode = "keyword" // Default to keyword for backward compatibility
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 15
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.2
	}

	// For keyword mode, use simple repository search
	if mode == "keyword" {
		nodes, err := s.repo.SearchNodes(req.Query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Search failed: %v", err))
			return
		}

		results := make([]SearchResult, len(nodes))
		for i, node := range nodes {
			results[i] = SearchResult{
				ID:            fmt.Sprintf("%d", node.ID),
				Name:          node.Name,
				Kind:          string(node.Kind),
				File:          node.File,
				Line:          node.Line,
				EndLine:       node.EndLine,
				QualifiedName: node.QualifiedName,
				Score:         1.0 - float64(i)*0.01,
				FilePath:      node.File,
				ContentType:   string(node.Kind),
				Content:       node.Name,
			}
		}

		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: results,
			Total:   len(results),
		})
		return
	}

	// For semantic/hybrid mode, create embedder
	embedder := s.createEmbedder()
	if embedder == nil {
		// Fall back to keyword search
		nodes, err := s.repo.SearchNodes(req.Query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Search failed: %v", err))
			return
		}

		results := make([]SearchResult, len(nodes))
		for i, node := range nodes {
			results[i] = SearchResult{
				ID:            fmt.Sprintf("%d", node.ID),
				Name:          node.Name,
				Kind:          string(node.Kind),
				File:          node.File,
				Line:          node.Line,
				EndLine:       node.EndLine,
				QualifiedName: node.QualifiedName,
				Score:         1.0 - float64(i)*0.01,
				FilePath:      node.File,
				ContentType:   string(node.Kind),
				Content:       node.Name,
			}
		}

		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: results,
			Total:   len(results),
			Message: "Embeddings not available. Showing keyword results only.",
		})
		return
	}

	// Semantic or hybrid search
	ctx := r.Context()

	// Get query embedding
	vectors, err := embedder.Embed(ctx, []string{req.Query})
	if err != nil || len(vectors) == 0 {
		// Fall back to keyword search
		nodes, ferr := s.repo.SearchNodes(req.Query, limit)
		if ferr != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Search failed: %v", ferr))
			return
		}

		results := make([]SearchResult, len(nodes))
		for i, node := range nodes {
			results[i] = SearchResult{
				ID:            fmt.Sprintf("%d", node.ID),
				Name:          node.Name,
				Kind:          string(node.Kind),
				File:          node.File,
				Line:          node.Line,
				EndLine:       node.EndLine,
				QualifiedName: node.QualifiedName,
				Score:         1.0 - float64(i)*0.01,
				FilePath:      node.File,
				ContentType:   string(node.Kind),
				Content:       node.Name,
			}
		}

		msg := "Embedding failed. Showing keyword results only."
		if err != nil {
			msg = fmt.Sprintf("Embedding failed: %v. Showing keyword results only.", err)
		}
		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: results,
			Total:   len(results),
			Message: msg,
		})
		return
	}

	// Perform semantic search
	// Use project-specific repo if project_id is provided
	searchRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			searchRepo = pr
		}
	}
	semResults, err := searchRepo.SemanticSearch(vectors[0], limit*2, float32(minScore))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Semantic search failed: %v", err))
		return
	}

	// Apply filters and convert results
	results := make([]SearchResult, 0, len(semResults))
	for i, sr := range semResults {
		// Apply kind filter
		if req.Kind != "" && sr.Kind != req.Kind {
			continue
		}
		// Apply file filter
		if req.File != "" && !strings.Contains(sr.File, req.File) {
			continue
		}
		// Apply test filter
		if req.NoTests && isTestFile(sr.File) {
			continue
		}

		results = append(results, SearchResult{
			ID:            fmt.Sprintf("%d", sr.NodeID),
			Name:          sr.Name,
			Kind:          sr.Kind,
			File:          sr.File,
			Line:          sr.Line,
			EndLine:       sr.EndLine,
			QualifiedName: sr.QualifiedName,
			Score:         float64(sr.Score),
			SemanticRank:  i + 1,
			FilePath:      sr.File,
			ContentType:   sr.Kind,
		})

		if len(results) >= limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, &SearchResponse{
		Results: results,
		Total:   len(results),
	})
}

// handleStats handles stats requests.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	// Use project-specific repo if project ID is provided
	statsRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			statsRepo = pr
		}
	}

	stats, err := statsRepo.GetStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_ERROR", fmt.Sprintf("Failed to get stats: %v", err))
		return
	}

	// Convert NodesByKind keys to strings
	nodesByKind := make(map[string]int)
	for k, v := range stats.NodesByKind {
		nodesByKind[string(k)] = int(v)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_nodes":   stats.TotalNodes,
		"total_edges":   stats.TotalEdges,
		"nodes_by_kind": nodesByKind,
	})
}

// handleListFiles handles file listing requests.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	// Use project-specific repo if project ID is provided
	filesRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			filesRepo = pr
		}
	}

	files, err := filesRepo.GetAllFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_ERROR", fmt.Sprintf("Failed to list files: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
		"count": len(files),
	})
}

// handleGetSymbol handles symbol detail requests.
func (s *Server) handleGetSymbol(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	idStr := ps.ByName("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidSymbolId"))
		return
	}

	// Use project-specific repo if project ID is provided
	symbolRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			symbolRepo = pr
		}
	}

	node, err := symbolRepo.FindNodeByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.symbolNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to get symbol: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, NodeInfo{
		ID:            node.ID,
		Name:          node.Name,
		Kind:          string(node.Kind),
		File:          node.File,
		Line:          node.Line,
		EndLine:       node.EndLine,
		QualifiedName: node.QualifiedName,
		Exported:      node.Exported,
		Visibility:    string(node.Visibility),
	})
}

// handleGetCallers handles callers requests.
func (s *Server) handleGetCallers(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	idStr := ps.ByName("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidSymbolId"))
		return
	}

	// Use project-specific repo if project ID is provided
	callersRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			callersRepo = pr
		}
	}

	callers, err := callersRepo.FindCallers(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to get callers: %v", err))
		return
	}

	results := make([]NodeInfo, len(callers))
	for i, c := range callers {
		results[i] = NodeInfo{
			ID:   c.ID,
			Name: c.Name,
			Kind: string(c.Kind),
			File: c.File,
			Line: c.Line,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"callers": results,
		"count":   len(results),
	})
}

// handleGetCallees handles callees requests.
func (s *Server) handleGetCallees(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	idStr := ps.ByName("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidSymbolId"))
		return
	}

	// Use project-specific repo if project ID is provided
	calleesRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			calleesRepo = pr
		}
	}

	callees, err := calleesRepo.FindCallees(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to get callees: %v", err))
		return
	}

	results := make([]NodeInfo, len(callees))
	for i, c := range callees {
		results[i] = NodeInfo{
			ID:   c.ID,
			Name: c.Name,
			Kind: string(c.Kind),
			File: c.File,
			Line: c.Line,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"callees": results,
		"count":   len(results),
	})
}

// handleEmbed handles embedding build requests.
func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req EmbedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Check if embedding is configured
	configured, err := s.repo.IsEmbeddingConfigured()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedCheckEmbedConfig"))
		return
	}
	if !configured {
		writeError(w, http.StatusBadRequest, "NOT_CONFIGURED", i18n.T("api.error.embeddingNotConfigured"))
		return
	}

	// Always recreate embedder from current settings so dimension changes are picked up.
	{
		embedder := s.createEmbedder()
		if embedder == nil {
			writeError(w, http.StatusBadRequest, "NOT_CONFIGURED", i18n.T("api.error.embeddingProviderNotConfigured"))
			return
		}
		s.embeddingSvc.SetEmbedder(embedder)

		// Set model config for tracking
		config, _ := s.repo.GetEmbeddingConfig()
		s.embeddingSvc.SetModelConfig(config)
	}

	// Create task for async embedding
	t := s.taskMgr.CreateTask("embed")
	taskID := t.ID

	// Get project ID if provided
	var projectID string
	if req.ProjectID != "" {
		projectID = req.ProjectID
	} else if req.RootDir != "" {
		projects, _ := s.globalRepo.ListProjects()
		for _, p := range projects {
			if p.RootPath == req.RootDir {
				projectID = p.ID
				break
			}
		}
	}

	go func() {
		ctx := t.Context()
		t.SetStatus(task.StatusRunning)
		t.SetProgress(0, 100, "Starting embedding generation...")

		// Progress callback
		progressChan := make(chan service.Progress, 10)
		go func() {
			for prog := range progressChan {
				t.SetProgress(prog.Current, prog.Total, prog.Message)
				s.eventBroker.BroadcastEmbedProgress(taskID, projectID, prog.Current, prog.Total, string(prog.Status))
			}
		}()

		// Determine if force re-embedding
		force := req.Strategy == "full"

		// Get kinds to embed (default to functions, methods, classes)
		kinds := []string{"function", "method", "class", "interface", "struct"}
		if req.Strategy == "incremental" {
			kinds = nil // Embed all kinds for incremental
		}

		// Use project-specific repo if project ID is available
		var embedRepo *repository.Repository
		if projectID != "" {
			if pr, err := s.projectRepo(projectID); err == nil {
				embedRepo = pr
			}
		}

		prog, err := s.embeddingSvc.GenerateEmbeddingsWithRepo(ctx, embedRepo, force, kinds, progressChan)
		close(progressChan)

		if err != nil {
			t.SetError(err.Error())
			s.eventBroker.BroadcastEmbedError(taskID, projectID, err.Error())
			return
		}

		if prog.Status == service.StatusCanceled {
			t.SetError("Embedding canceled")
			return
		}

		descNewCount := prog.NewCount
		descUpdatedCount := prog.UpdatedCount
		descTotal := prog.Total

		// Phase 2: CCE code embeddings
		// After description embeddings complete, generate code-mode embeddings
		// so that the Cognitive Context Engine has both modalities available.
		var codeNewCount int
		var codeTotal int
		if embedRepo != nil {
			cceEmbedder := s.createEmbedder()
			if cceEmbedder != nil {
				logger.S().Infow("[handleEmbed] Starting CCE code embedding phase",
					"project_id", projectID)

				// Get project root path for source code extraction
				var projectRootPath string
				if project, pErr := s.globalRepo.GetProject(projectID); pErr == nil && project != nil {
					projectRootPath = project.RootPath
				}

				cceEngine := cce.NewEngine(embedRepo, cceEmbedder, projectRootPath)

				// Apply max context tokens from config
				if cfg, cfgErr := s.globalRepo.GetEmbeddingConfig(); cfgErr == nil && cfg != nil {
					if val, ok := cfg["embedding_max_context_tokens"]; ok && val != "" {
						if n, atoiErr := strconv.Atoi(val); atoiErr == nil && n > 0 {
							cceEngine.SetMaxContextTokens(n)
						}
					}
				}

				cceProgressCh := make(chan cce.EmbeddingProgress, 10)
				go func() {
					for p := range cceProgressCh {
						t.SetProgress(p.Current, p.Total, fmt.Sprintf("CCE code embedding: %d/%d", p.Current, p.Total))
						s.eventBroker.BroadcastEmbedProgress(taskID, projectID, p.Current, p.Total, string(p.Status))
					}
				}()

				cceProg, cceErr := cceEngine.GenerateEmbeddings(ctx, force, cce.ModeCode, kinds, cceProgressCh)
				close(cceProgressCh)

				if cceErr != nil {
					// Log but don't fail the whole task — description embeddings succeeded
					logger.S().Warnw("[handleEmbed] CCE code embedding failed (non-fatal)",
						"error", cceErr)
				} else if cceProg != nil {
					codeNewCount = cceProg.NewCount
					codeTotal = cceProg.Total
					logger.S().Infow("[handleEmbed] CCE code embedding complete",
						"new_count", codeNewCount,
						"total", codeTotal)
				}
			}
		}

		t.SetResult(map[string]interface{}{
			"total":              descTotal,
			"new_embeddings":     descNewCount,
			"updated_embeddings": descUpdatedCount,
			"status":             string(prog.Status),
			"cce_code_new":       codeNewCount,
			"cce_code_total":     codeTotal,
		})

		s.eventBroker.BroadcastEmbedComplete(taskID, projectID, descTotal, descNewCount, descUpdatedCount)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"task_id": taskID,
		"status":  "pending",
	})
}

// handleEmbedStatus handles embedding status requests.
// Supports optional ?project_id= query parameter to get count from project-specific DB.
func (s *Server) handleEmbedStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.embeddingSvc == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "not_configured",
			"message": "Embedding service not initialized",
		})
		return
	}

	progress := s.embeddingSvc.GetStatus()

	// Determine which repo to count embeddings from
	var count int64
	var codeEmbeddingCount int64
	projectID := r.URL.Query().Get("project_id")
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			count, _ = pr.GetEmbeddingCount()
			// Also count CCE code embeddings
			cceStore := cce.NewStore(pr.DB())
			if _, codeCount, err := cceStore.GetDualEmbeddingStats(); err == nil {
				codeEmbeddingCount = codeCount
			}
		}
	} else {
		count, _ = s.embeddingSvc.GetEmbeddingCount()
	}

	needsReembedding, currentModel, _ := s.embeddingSvc.NeedsReembedding()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":               string(progress.Status),
		"current":              progress.Current,
		"total":                progress.Total,
		"message":              progress.Message,
		"error":                progress.Error,
		"new_count":            progress.NewCount,
		"updated_count":        progress.UpdatedCount,
		"embedding_count":      count,
		"code_embedding_count": codeEmbeddingCount,
		"model":                currentModel,
		"needs_reembedding":    needsReembedding,
		"is_configured":        s.embeddingSvc.IsConfigured(),
	})
}

// handleEmbedCancel handles embedding cancel requests.
func (s *Server) handleEmbedCancel(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.embeddingSvc == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "no_operation",
		})
		return
	}

	s.embeddingSvc.Cancel()
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "canceled",
	})
}

// handleEmbedTest handles embedding connection test requests.
// It tests the embedding API connection and returns the model dimension.
func (s *Server) handleEmbedTest(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Create embedder based on request parameters
	var embedder embedding.Embedder
	switch req.Provider {
	case "openai":
		if req.APIKey == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   "API key is required for OpenAI",
			})
			return
		}
		model := req.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		embedder = embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey: req.APIKey,
			Model:  model,
		})
	case "ollama":
		baseURL := req.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		model := req.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		embedder = embedding.NewOllamaEmbedder(baseURL, model)
	case "jina":
		if req.APIKey == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   "API key is required for Jina",
			})
			return
		}
		baseURL := req.BaseURL
		if baseURL == "" {
			baseURL = "https://api.jina.ai/v1"
		}
		model := req.Model
		if model == "" {
			model = "jina-embeddings-v2-base-en"
		}
		embedder = embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey:  req.APIKey,
			BaseURL: baseURL,
			Model:   model,
		})
	case "custom":
		if req.BaseURL == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   "Base URL is required for custom provider",
			})
			return
		}
		embedder = embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey:  req.APIKey,
			BaseURL: req.BaseURL,
			Model:   req.Model,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Unknown provider: " + req.Provider,
		})
		return
	}

	// Test the embedding by sending a simple query
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	vectors, err := embedder.Embed(ctx, []string{"test"})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	if len(vectors) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "No embedding returned from API",
		})
		return
	}

	dimension := len(vectors[0])
	model := req.Model
	if model == "" {
		model = embedder.ModelName()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"dimension": dimension,
		"model":     model,
		"provider":  req.Provider,
	})
}

// handleSemanticSearch handles semantic search requests.
func (s *Server) handleSemanticSearch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req SemanticSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.queryRequired"))
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 0.2
	}

	// Get project-specific repo if project ID is provided
	var searchRepo *repository.Repository
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			searchRepo = pr
		}
	}
	if searchRepo == nil {
		searchRepo = s.repo
	}

	// Check if embedding is configured
	configured, err := s.repo.IsEmbeddingConfigured()
	if err != nil || !configured {
		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
			Message: "Embedding is not configured. Please configure embedding in settings.",
		})
		return
	}

	// Create embedder from current settings
	embedder := s.createEmbedder()
	if embedder == nil {
		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
			Message: "Embedding provider is not configured.",
		})
		return
	}

	// Get query embedding
	vectors, err := embedder.Embed(r.Context(), []string{req.Query})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EMBED_ERROR", fmt.Sprintf("Failed to embed query: %v", err))
		return
	}
	if len(vectors) == 0 {
		writeJSON(w, http.StatusOK, &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
		})
		return
	}

	// Perform semantic search
	semResults, err := searchRepo.SemanticSearch(vectors[0], limit, threshold)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Semantic search failed: %v", err))
		return
	}

	// Convert results
	results := make([]SearchResult, len(semResults))
	for i, r := range semResults {
		results[i] = SearchResult{
			ID:            fmt.Sprintf("%d", r.NodeID),
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			Score:         float64(r.Score),
			SemanticRank:  i + 1,
			QualifiedName: r.QualifiedName,
		}
	}

	writeJSON(w, http.StatusOK, &SearchResponse{
		Results: results,
		Total:   len(results),
	})
}

// handleMCP handles MCP protocol requests.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// TODO: Implement MCP handler
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", i18n.T("api.error.mcpComingSoon"))
}

// Request/Response types

// BuildRequest represents a build request.
type BuildRequest struct {
	RootDir         string   `json:"root_dir"`
	FullBuild       bool     `json:"full_build"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	IncludeDataflow bool     `json:"include_dataflow"`
	IncludeAST      bool     `json:"include_ast"`
	ProjectID       string   `json:"project_id,omitempty"`
	ProjectName     string   `json:"project_name,omitempty"`
	MaxFileSize     int64    `json:"max_file_size,omitempty"` // max file size in bytes, 0 = use default (1MB) or AXONS_MAX_FILE_SIZE env
}

// QueryRequest represents a query request.
type QueryRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	File      string `json:"file,omitempty"`
	Callers   bool   `json:"callers"`
	Callees   bool   `json:"callees"`
	NoTests   bool   `json:"no_tests"`
	Limit     int    `json:"limit,omitempty"`
	ProjectID string `json:"project_id,omitempty"` // Project-specific DB
}

// NodeInfo represents a code node.
type NodeInfo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	EndLine       int    `json:"end_line,omitempty"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Exported      bool   `json:"exported"`
	Visibility    string `json:"visibility,omitempty"`
}

// QueryResponse represents a query response.
type QueryResponse struct {
	Nodes   []NodeInfo `json:"nodes"`
	Callers []NodeInfo `json:"callers,omitempty"`
	Callees []NodeInfo `json:"callees,omitempty"`
}

// SearchRequest represents a search request.
type SearchRequest struct {
	Query     string  `json:"query"`
	Mode      string  `json:"mode,omitempty"`       // hybrid, semantic, keyword
	Limit     int     `json:"limit,omitempty"`
	MinScore  float64 `json:"min_score,omitempty"`  // Minimum similarity score (0-1)
	Kind      string  `json:"kind,omitempty"`       // Filter by symbol kind
	File      string  `json:"file,omitempty"`       // Filter by file path pattern
	NoTests   bool    `json:"no_tests,omitempty"`   // Exclude test files
	ProjectID string  `json:"project_id,omitempty"` // Project-specific DB
}

// SearchResult represents a search result.
type SearchResult struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line,omitempty"`
	QualifiedName string  `json:"qualified_name,omitempty"`
	Score         float64 `json:"score"`
	RRFScore      float64 `json:"rrf_score,omitempty"`
	BM25Score     float64 `json:"bm25_score,omitempty"`
	SemanticRank  int     `json:"semantic_rank,omitempty"`
	// Legacy fields for backward compatibility
	FilePath    string `json:"file_path,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content,omitempty"`
}

// SearchResponse represents a search response.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
	Message string         `json:"message,omitempty"`
}

// EmbedRequest represents an embed request.
type EmbedRequest struct {
	RootDir   string `json:"root_dir"`
	ProjectID string `json:"project_id,omitempty"`
	Provider  string `json:"provider"`
	Model     string `json:"model,omitempty"`
	Strategy  string `json:"strategy"`
	BatchSize int    `json:"batch_size,omitempty"`
}

// SemanticSearchRequest represents a semantic search request.
type SemanticSearchRequest struct {
	Query     string `json:"query"`
	Limit     int    `json:"limit,omitempty"`
	Threshold float32 `json:"threshold,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// handleAudit handles audit requests.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req AuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	maxCycles := req.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 10
	}
	maxComplex := req.MaxComplexity
	if maxComplex <= 0 {
		maxComplex = 15
	}

	// Use project-specific repo if project ID is provided
	auditRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			auditRepo = pr
		}
	}

	result := s.buildAuditResult(auditRepo, maxCycles, maxComplex)
	writeJSON(w, http.StatusOK, result)
}

// AuditRequest represents an audit request.
type AuditRequest struct {
	MaxCycles     int    `json:"max_cycles,omitempty"`
	MaxComplexity int    `json:"max_complexity,omitempty"`
	ProjectID     string `json:"project_id,omitempty"` // Project-specific DB
}

// AuditResponse represents an audit response.
type AuditResponse struct {
	Summary        AuditSummary   `json:"summary"`
	Cycles         []CycleInfo    `json:"cycles,omitempty"`
	DeadCode       []DeadCodeInfo `json:"dead_code,omitempty"`
	HighComplexity []ComplexInfo  `json:"high_complexity,omitempty"`
	EntryPoints    []string       `json:"entry_points,omitempty"`
	Issues         int            `json:"issues"`
}

// AuditSummary summarizes the audit.
type AuditSummary struct {
	TotalNodes      int `json:"total_nodes"`
	TotalEdges      int `json:"total_edges"`
	TotalFunctions  int `json:"total_functions"`
	TotalClasses    int `json:"total_classes"`
	CyclesFound     int `json:"cycles_found"`
	DeadCodeCount   int `json:"dead_code_count"`
	ComplexWarnings int `json:"complex_warnings"`
	EntryPoints     int `json:"entry_points"`
}

// CycleInfo represents a detected cycle.
type CycleInfo struct {
	Nodes  []string `json:"nodes"`
	Length int      `json:"length"`
}

// DeadCodeInfo represents dead code.
type DeadCodeInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// ComplexInfo represents a high complexity function.
type ComplexInfo struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
}

func (s *Server) buildAuditResult(repo *repository.Repository, maxCycles, maxComplex int) *AuditResponse {
	result := &AuditResponse{}
	db := repo.DB()

	// Get summary stats
	db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&result.Summary.TotalNodes)
	db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&result.Summary.TotalEdges)
	db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind IN ('function', 'method')`).Scan(&result.Summary.TotalFunctions)
	db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'class'`).Scan(&result.Summary.TotalClasses)

	// Detect cycles
	result.Cycles = s.detectCycles(db, maxCycles)
	result.Summary.CyclesFound = len(result.Cycles)

	// Find dead code
	result.DeadCode = s.findDeadCode(db)
	result.Summary.DeadCodeCount = len(result.DeadCode)

	// Find high complexity functions
	result.HighComplexity = s.findHighComplexity(repo, maxComplex)
	result.Summary.ComplexWarnings = len(result.HighComplexity)

	// Find entry points
	result.EntryPoints = s.findEntryPoints(db)
	result.Summary.EntryPoints = len(result.EntryPoints)

	// Count total issues
	result.Issues = result.Summary.CyclesFound + result.Summary.DeadCodeCount + result.Summary.ComplexWarnings

	return result
}

func (s *Server) detectCycles(db *sql.DB, maxCycles int) []CycleInfo {
	var cycles []CycleInfo

	// Get all call edges
	edges := make(map[int64][]int64)
	rows, err := db.Query(`SELECT source_id, target_id FROM edges WHERE kind = 'calls'`)
	if err != nil {
		return cycles
	}
	defer rows.Close()

	for rows.Next() {
		var source, target int64
		if err := rows.Scan(&source, &target); err != nil {
			continue
		}
		edges[source] = append(edges[source], target)
	}

	// Find cycles using DFS
	visited := make(map[int64]bool)
	recStack := make(map[int64]bool)
	path := []int64{}

	var findCyclesDFS func(node int64)
	findCyclesDFS = func(node int64) {
		if len(cycles) >= maxCycles {
			return
		}

		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range edges[node] {
			if !visited[neighbor] {
				findCyclesDFS(neighbor)
			} else if recStack[neighbor] {
				// Found cycle
				cycleStart := -1
				for i, n := range path {
					if n == neighbor {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cyclePath := path[cycleStart:]
					if len(cyclePath) > 1 {
						cycles = append(cycles, CycleInfo{
							Nodes:  s.getNodeNames(db, cyclePath),
							Length: len(cyclePath),
						})
					}
				}
			}
		}

		path = path[:len(path)-1]
		recStack[node] = false
	}

	// Start DFS from each unvisited node
	for node := range edges {
		if !visited[node] && len(cycles) < maxCycles {
			findCyclesDFS(node)
		}
	}

	return cycles
}

func (s *Server) getNodeNames(db *sql.DB, nodeIDs []int64) []string {
	names := make([]string, len(nodeIDs))
	for i, id := range nodeIDs {
		db.QueryRow(`SELECT name FROM nodes WHERE id = ?`, id).Scan(&names[i])
	}
	return names
}

func (s *Server) findDeadCode(db *sql.DB) []DeadCodeInfo {
	var dead []DeadCodeInfo

	// Find functions with no callers that are not entry points
	rows, err := db.Query(`
		SELECT n.id, n.name, n.kind, n.file, n.line
		FROM nodes n
		WHERE n.kind IN ('function', 'method')
		AND n.exported = 0
		AND NOT EXISTS (
			SELECT 1 FROM edges e WHERE e.target_id = n.id AND e.kind = 'calls'
		)
		AND n.name NOT LIKE 'main%'
		AND n.name NOT LIKE 'Test%'
		LIMIT 100
	`)
	if err != nil {
		return dead
	}
	defer rows.Close()

	for rows.Next() {
		var info DeadCodeInfo
		var id int64
		if err := rows.Scan(&id, &info.Name, &info.Kind, &info.File, &info.Line); err != nil {
			continue
		}
		dead = append(dead, info)
	}

	return dead
}

func (s *Server) findHighComplexity(repo *repository.Repository, threshold int) []ComplexInfo {
	var results []ComplexInfo

	metrics, err := repo.GetHighComplexityFunctions(threshold, 50)
	if err != nil {
		return results
	}

	for _, m := range metrics {
		node, err := repo.FindNodeByID(m.NodeID)
		if err != nil {
			continue
		}
		results = append(results, ComplexInfo{
			Name:       node.Name,
			File:       node.File,
			Line:       node.Line,
			Cyclomatic: m.Cyclomatic,
			Cognitive:  m.Cognitive,
		})
	}

	return results
}

func (s *Server) findEntryPoints(db *sql.DB) []string {
	var entries []string

	rows, err := db.Query(`
		SELECT name FROM nodes
		WHERE kind = 'function'
		AND (name LIKE 'main%' OR name LIKE 'init%' OR role = 'entry')
		LIMIT 50
	`)
	if err != nil {
		return entries
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		entries = append(entries, name)
	}

	return entries
}

// handleCheck handles CI check requests.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	maxComplex := req.MaxComplexity
	if maxComplex <= 0 {
		maxComplex = 15
	}

	// Use project-specific repo if project ID is provided
	checkRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			checkRepo = pr
		}
	}

	result := s.runChecks(checkRepo, maxComplex, req.FailOnDeadCode, req.FailOnComplex, req.NoNewCycles)
	writeJSON(w, http.StatusOK, result)
}

// CheckRequest represents a check request.
type CheckRequest struct {
	MaxComplexity  int    `json:"max_complexity,omitempty"`
	FailOnDeadCode bool   `json:"fail_on_dead_code"`
	FailOnComplex  bool   `json:"fail_on_complex"`
	NoNewCycles    bool   `json:"no_new_cycles"`
	ProjectID      string `json:"project_id,omitempty"` // Project-specific DB
}

// CheckResponse represents a check response.
type CheckResponse struct {
	Passed       bool        `json:"passed"`
	Checks       []CheckItem `json:"checks"`
	TotalChecks  int         `json:"total_checks"`
	PassedChecks int         `json:"passed_checks"`
	FailedChecks int         `json:"failed_checks"`
	Summary      string      `json:"summary"`
}

// CheckItem represents a single check.
type CheckItem struct {
	Name       string   `json:"name"`
	Passed     bool     `json:"passed"`
	Severity   string   `json:"severity"`
	Message    string   `json:"message"`
	Details    []string `json:"details,omitempty"`
	Suggestion string   `json:"suggestion,omitempty"`
}

func (s *Server) runChecks(repo *repository.Repository, maxComplex int, failOnDeadCode, failOnComplex, noNewCycles bool) *CheckResponse {
	result := &CheckResponse{
		Checks: []CheckItem{},
	}
	db := repo.DB()

	// Check 1: No cycles
	cycles := s.detectCycles(db, 10)
	cycleCheck := CheckItem{
		Name:     "no_cycles",
		Severity: "error",
	}
	if len(cycles) == 0 {
		cycleCheck.Passed = true
		cycleCheck.Message = "No circular dependencies found"
	} else {
		cycleCheck.Passed = false
		cycleCheck.Message = fmt.Sprintf("Found %d circular dependencies", len(cycles))
		for _, c := range cycles {
			cycleCheck.Details = append(cycleCheck.Details, strings.Join(c.Nodes, " -> "))
		}
		cycleCheck.Suggestion = "Consider refactoring to break circular dependencies"
	}
	result.Checks = append(result.Checks, cycleCheck)

	// Check 2: No dead code
	deadCode := s.findDeadCode(db)
	deadCodeCheck := CheckItem{
		Name:     "no_dead_code",
		Severity: "warning",
	}
	if len(deadCode) == 0 {
		deadCodeCheck.Passed = true
		deadCodeCheck.Message = "No dead code detected"
	} else {
		deadCodeCheck.Passed = !failOnDeadCode
		deadCodeCheck.Message = fmt.Sprintf("Found %d potentially unused functions", len(deadCode))
		for _, dc := range deadCode {
			deadCodeCheck.Details = append(deadCodeCheck.Details, fmt.Sprintf("%s (%s:%d)", dc.Name, dc.File, dc.Line))
		}
		deadCodeCheck.Suggestion = "Review and remove or export these functions"
	}
	result.Checks = append(result.Checks, deadCodeCheck)

	// Check 3: Complexity within limits
	highComplex := s.findHighComplexity(repo, maxComplex)
	complexCheck := CheckItem{
		Name:     "complexity_limit",
		Severity: "warning",
	}
	if len(highComplex) == 0 {
		complexCheck.Passed = true
		complexCheck.Message = "All functions within complexity limits"
	} else {
		complexCheck.Passed = !failOnComplex
		complexCheck.Message = fmt.Sprintf("Found %d functions exceeding complexity threshold", len(highComplex))
		for _, hc := range highComplex {
			complexCheck.Details = append(complexCheck.Details, fmt.Sprintf("%s (complexity: %d) at %s:%d", hc.Name, hc.Cyclomatic, hc.File, hc.Line))
		}
		complexCheck.Suggestion = "Consider refactoring complex functions"
	}
	result.Checks = append(result.Checks, complexCheck)

	// Calculate summary
	result.TotalChecks = len(result.Checks)
	for _, c := range result.Checks {
		if c.Passed {
			result.PassedChecks++
		} else {
			result.FailedChecks++
		}
	}
	result.Passed = result.FailedChecks == 0

	if result.Passed {
		result.Summary = "All checks passed"
	} else {
		result.Summary = fmt.Sprintf("%d check(s) failed", result.FailedChecks)
	}

	return result
}

// handleComplexity handles complexity analysis requests.
func (s *Server) handleComplexity(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req ComplexityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 10
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	// Use project-specific repo if project ID is provided
	complexRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			complexRepo = pr
		}
	}

	var metrics []*repository.ComplexityMetrics
	var err error

	if req.File != "" {
		metrics, err = complexRepo.GetComplexityByFile(req.File)
	} else {
		metrics, err = complexRepo.GetHighComplexityFunctions(threshold, limit)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to get complexity metrics: %v", err))
		return
	}

	results := make([]ComplexityResult, len(metrics))
	for i, m := range metrics {
		node, err := complexRepo.FindNodeByID(m.NodeID)
		if err != nil {
			continue
		}
		results[i] = ComplexityResult{
			Name:       node.Name,
			File:       node.File,
			Line:       node.Line,
			Cyclomatic: m.Cyclomatic,
			Cognitive:  m.Cognitive,
			Nesting:    m.Nesting,
		}
	}

	writeJSON(w, http.StatusOK, ComplexityResponse{
		Threshold: threshold,
		Functions: results,
		Total:     len(results),
	})
}

// ComplexityRequest represents a complexity request.
type ComplexityRequest struct {
	Threshold int    `json:"threshold,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	File      string `json:"file,omitempty"`
	ProjectID string `json:"project_id,omitempty"` // Project-specific DB
}

// ComplexityResponse represents a complexity response.
type ComplexityResponse struct {
	Threshold int                `json:"threshold"`
	Functions []ComplexityResult `json:"functions"`
	Total     int                `json:"total"`
}

// ComplexityResult represents a complexity result.
type ComplexityResult struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	Nesting    int    `json:"nesting"`
}

// handlePath handles path finding requests.
func (s *Server) handlePath(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req PathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fromToRequired"))
		return
	}

	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	// Use project-specific repo if project ID is provided
	pathRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			pathRepo = pr
		}
	}

	result := s.findPaths(pathRepo.DB(), req.From, req.To, maxDepth, req.FindAll)
	writeJSON(w, http.StatusOK, result)
}

// PathRequest represents a path finding request.
type PathRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	MaxDepth  int    `json:"max_depth,omitempty"`
	FindAll   bool   `json:"find_all"`
	ProjectID string `json:"project_id,omitempty"` // Project-specific DB
}

// PathResponse represents a path finding response.
type PathResponse struct {
	From       string       `json:"from"`
	To         string       `json:"to"`
	Paths      [][]PathStep `json:"paths"`
	TotalPaths int          `json:"total_paths"`
	MaxDepth   int          `json:"max_depth"`
	Truncated  bool         `json:"truncated"`
}

// PathStep represents a step in a call path.
type PathStep struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

func (s *Server) findPaths(db *sql.DB, fromName, toName string, maxDepth int, findAll bool) *PathResponse {
	result := &PathResponse{
		From:     fromName,
		To:       toName,
		MaxDepth: maxDepth,
		Paths:    [][]PathStep{},
	}

	// Find source and target nodes
	var fromNodes []int64
	var toNodes []int64

	rows, err := db.Query(`SELECT id FROM nodes WHERE name = ?`, fromName)
	if err != nil {
		return result
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			fromNodes = append(fromNodes, id)
		}
	}
	rows.Close()

	rows, err = db.Query(`SELECT id FROM nodes WHERE name = ?`, toName)
	if err != nil {
		return result
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			toNodes = append(toNodes, id)
		}
	}
	rows.Close()

	if len(fromNodes) == 0 || len(toNodes) == 0 {
		return result
	}

	// Build adjacency list
	edges := make(map[int64][]int64)
	rows, err = db.Query(`SELECT source_id, target_id FROM edges WHERE kind = 'calls'`)
	if err != nil {
		return result
	}
	for rows.Next() {
		var source, target int64
		if err := rows.Scan(&source, &target); err == nil {
			edges[source] = append(edges[source], target)
		}
	}
	rows.Close()

	// BFS to find paths
	toSet := make(map[int64]bool)
	for _, id := range toNodes {
		toSet[id] = true
	}

	// Find all paths from source nodes
	for _, startID := range fromNodes {
		paths := s.bfsPaths(startID, toSet, edges, maxDepth, findAll)
		for _, path := range paths {
			steps := s.nodePathToSteps(db, path)
			if len(steps) > 0 {
				result.Paths = append(result.Paths, steps)
			}
		}
	}

	result.TotalPaths = len(result.Paths)
	return result
}

func (s *Server) bfsPaths(start int64, targets map[int64]bool, edges map[int64][]int64, maxDepth int, findAll bool) [][]int64 {
	var results [][]int64
	visited := make(map[int64]bool)
	queue := [][]int64{{start}}

	for len(queue) > 0 && len(results) < 100 {
		path := queue[0]
		queue = queue[1:]

		if len(path) > maxDepth {
			continue
		}

		current := path[len(path)-1]

		if targets[current] && len(path) > 1 {
			results = append(results, path)
			if !findAll {
				return results
			}
			continue
		}

		if visited[current] {
			continue
		}
		visited[current] = true

		for _, next := range edges[current] {
			if !visited[next] || targets[next] {
				newPath := make([]int64, len(path)+1)
				copy(newPath, path)
				newPath[len(path)] = next
				queue = append(queue, newPath)
			}
		}
	}

	return results
}

func (s *Server) nodePathToSteps(db *sql.DB, nodeIDs []int64) []PathStep {
	steps := make([]PathStep, len(nodeIDs))
	for i, id := range nodeIDs {
		var name, kind, file string
		var line int
		err := db.QueryRow(`SELECT name, kind, file, line FROM nodes WHERE id = ?`, id).Scan(&name, &kind, &file, &line)
		if err != nil {
			continue
		}
		steps[i] = PathStep{
			Name: name,
			Kind: kind,
			File: file,
			Line: line,
		}
	}
	return steps
}

// handleSequence handles sequence diagram generation requests.
func (s *Server) handleSequence(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req SequenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.nameRequired"))
		return
	}

	depth := req.Depth
	if depth <= 0 {
		depth = 10
	}

	// Use project-specific repo if project ID is provided
	seqRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			seqRepo = pr
		}
	}

	result := s.generateSequence(seqRepo.DB(), req.Name, depth, req.FileFilters, req.KindFilter, req.NoTests)
	writeJSON(w, http.StatusOK, result)
}

// SequenceRequest represents a sequence diagram request.
type SequenceRequest struct {
	Name        string   `json:"name"`
	Depth       int      `json:"depth,omitempty"`
	FileFilters []string `json:"file_filters,omitempty"`
	KindFilter  string   `json:"kind_filter,omitempty"`
	NoTests     bool     `json:"no_tests"`
	ProjectID   string   `json:"project_id,omitempty"` // Project-specific DB
}

// SequenceResponse represents a sequence diagram response.
type SequenceResponse struct {
	Entry         *SequenceEntry    `json:"entry"`
	Participants  []string          `json:"participants"`
	Messages      []SequenceMessage `json:"messages"`
	TotalMessages int               `json:"total_messages"`
	Depth         int               `json:"depth"`
	Truncated     bool              `json:"truncated"`
}

// SequenceEntry represents the entry point.
type SequenceEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// SequenceMessage represents a message in the sequence diagram.
type SequenceMessage struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Function  string `json:"function"`
	Line      int    `json:"line,omitempty"`
	Param     string `json:"param,omitempty"`
	ReturnVal string `json:"return_val,omitempty"`
}

func (s *Server) generateSequence(db *sql.DB, name string, depth int, fileFilters []string, kindFilter string, noTests bool) *SequenceResponse {
	result := &SequenceResponse{
		Depth:        depth,
		Participants: []string{},
		Messages:     []SequenceMessage{},
	}

	// Find entry point
	var entryID int64
	var entryName, entryKind, entryFile string
	var entryLine int
	err := db.QueryRow(`SELECT id, name, kind, file, line FROM nodes WHERE name = ? LIMIT 1`, name).Scan(
		&entryID, &entryName, &entryKind, &entryFile, &entryLine)
	if err != nil {
		return result
	}

	result.Entry = &SequenceEntry{
		Name: entryName,
		Kind: entryKind,
		File: entryFile,
		Line: entryLine,
	}

	// Build call graph and extract messages
	visited := make(map[int64]bool)
	s.traverseCalls(db, entryID, entryFile, depth, fileFilters, kindFilter, noTests, visited, result)

	result.TotalMessages = len(result.Messages)
	if result.TotalMessages >= 100 {
		result.Truncated = true
	}

	return result
}

func (s *Server) traverseCalls(db *sql.DB, nodeID int64, fromFile string, depth int, fileFilters []string, kindFilter string, noTests bool, visited map[int64]bool, result *SequenceResponse) {
	if depth <= 0 || visited[nodeID] {
		return
	}
	visited[nodeID] = true

	// Get callees
	rows, err := db.Query(`
		SELECT n.id, n.name, n.kind, n.file, n.line
		FROM edges e
		JOIN nodes n ON e.target_id = n.id
		WHERE e.source_id = ? AND e.kind = 'calls'
	`, nodeID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var calleeID int64
		var calleeName, calleeKind, calleeFile string
		var calleeLine int
		if err := rows.Scan(&calleeID, &calleeName, &calleeKind, &calleeFile, &calleeLine); err != nil {
			continue
		}

		// Apply filters
		if noTests && (strings.Contains(calleeFile, "_test.") || strings.Contains(calleeFile, "_spec.")) {
			continue
		}
		if kindFilter != "" && calleeKind != kindFilter {
			continue
		}
		if len(fileFilters) > 0 {
			matched := false
			for _, f := range fileFilters {
				if strings.Contains(calleeFile, f) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Add participant if not exists
		participant := calleeFile
		found := false
		for _, p := range result.Participants {
			if p == participant {
				found = true
				break
			}
		}
		if !found {
			result.Participants = append(result.Participants, participant)
		}

		// Add message
		result.Messages = append(result.Messages, SequenceMessage{
			From:     fromFile,
			To:       calleeFile,
			Function: calleeName,
			Line:     calleeLine,
		})

		// Recurse
		s.traverseCalls(db, calleeID, calleeFile, depth-1, fileFilters, kindFilter, noTests, visited, result)
	}
}

// handleExport handles graph export requests.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	format := req.Format
	if format == "" {
		format = "json"
	}
	limit := req.Limit
	// 0 means no limit (default)
	if limit < 0 {
		limit = 0
	}

	// Use project-specific repo if project ID is provided
	exportRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			exportRepo = pr
		}
	}

	result := s.exportGraph(exportRepo, format, limit, req.Filter)
	result.Format = format
	writeJSON(w, http.StatusOK, result)
}

// ExportRequest represents an export request.
type ExportRequest struct {
	Format    string `json:"format"`
	Filter    string `json:"filter,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	ProjectID string `json:"project_id,omitempty"` // Project-specific DB
}

// ExportResponse represents an export response.
type ExportResponse struct {
	Format string     `json:"format"`
	Nodes  []NodeInfo `json:"nodes"`
	Edges  []EdgeInfo `json:"edges"`
	Raw    string     `json:"raw,omitempty"`
}

// EdgeInfo represents an edge in export.
type EdgeInfo struct {
	SourceID int64  `json:"source_id"`
	TargetID int64  `json:"target_id"`
	Kind     string `json:"kind"`
}

func (s *Server) exportGraph(repo *repository.Repository, format string, limit int, filter string) *ExportResponse {
	result := &ExportResponse{
		Nodes: []NodeInfo{},
		Edges: []EdgeInfo{},
	}
	db := repo.DB()

	// Get nodes
	nodes, err := repo.FindNodesByName("", limit)
	if err == nil {
		for _, n := range nodes {
			result.Nodes = append(result.Nodes, NodeInfo{
				ID:            n.ID,
				Name:          n.Name,
				Kind:          string(n.Kind),
				File:          n.File,
				Line:          n.Line,
				EndLine:       n.EndLine,
				QualifiedName: n.QualifiedName,
				Exported:      n.Exported,
				Visibility:    string(n.Visibility),
			})
		}
	}

	// Get edges
	var rows *sql.Rows
	if limit > 0 {
		rows, err = db.Query(`SELECT source_id, target_id, kind FROM edges LIMIT ?`, limit)
	} else {
		rows, err = db.Query(`SELECT source_id, target_id, kind FROM edges`)
	}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var edge EdgeInfo
			if err := rows.Scan(&edge.SourceID, &edge.TargetID, &edge.Kind); err != nil {
				continue
			}
			if filter == "" || edge.Kind == filter {
				result.Edges = append(result.Edges, edge)
			}
		}
	}

	// For DOT and Mermaid formats, generate raw output
	switch format {
	case "dot":
		result.Raw = s.generateDOT(result)
	case "mermaid":
		result.Raw = s.generateMermaid(result)
	}

	return result
}

func (s *Server) generateDOT(data *ExportResponse) string {
	var sb strings.Builder
	sb.WriteString("digraph codegraph {\n")
	sb.WriteString("  rankdir=LR;\n")

	// Add nodes
	for _, n := range data.Nodes {
		sb.WriteString(fmt.Sprintf("  n%d [label=\"%s\", shape=box];\n", n.ID, n.Name))
	}

	// Add edges
	for _, e := range data.Edges {
		sb.WriteString(fmt.Sprintf("  n%d -> n%d [label=\"%s\"];\n", e.SourceID, e.TargetID, e.Kind))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func (s *Server) generateMermaid(data *ExportResponse) string {
	var sb strings.Builder
	sb.WriteString("graph LR\n")

	// Add nodes and edges
	for _, e := range data.Edges {
		var sourceName, targetName string
		for _, n := range data.Nodes {
			if n.ID == e.SourceID {
				sourceName = n.Name
			}
			if n.ID == e.TargetID {
				targetName = n.Name
			}
		}
		if sourceName != "" && targetName != "" {
			sb.WriteString(fmt.Sprintf("  %s -->|%s| %s\n", sourceName, e.Kind, targetName))
		}
	}

	return sb.String()
}

// handleDataflow handles dataflow analysis requests.
func (s *Server) handleDataflow(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req DataflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.functionNameRequired"))
		return
	}

	result := s.analyzeDataflow(req.Name, req.File, req.Detail)
	writeJSON(w, http.StatusOK, result)
}

// DataflowRequest represents a dataflow analysis request.
type DataflowRequest struct {
	Name   string `json:"name"`
	File   string `json:"file,omitempty"`
	Detail bool   `json:"detail"`
}

// DataflowResponse represents a dataflow analysis response.
type DataflowResponse struct {
	Function      string             `json:"function"`
	File          string             `json:"file"`
	Line          int                `json:"line"`
	Parameters    []ParameterFlow    `json:"parameters,omitempty"`
	Variables     []VariableFlow     `json:"variables,omitempty"`
	Returns       []ReturnFlow       `json:"returns,omitempty"`
	Warnings      []DataflowWarning  `json:"warnings,omitempty"`
	DataflowEdges []DataflowEdgeInfo `json:"dataflow_edges,omitempty"`
}

// ParameterFlow represents parameter data flow.
type ParameterFlow struct {
	Name    string   `json:"name"`
	Mutable bool     `json:"mutable"`
	Reads   int      `json:"reads"`
	Writes  int      `json:"writes"`
	FlowsTo []string `json:"flows_to,omitempty"`
}

// VariableFlow represents variable data flow.
type VariableFlow struct {
	Name      string   `json:"name"`
	Scope     string   `json:"scope"`
	DefinedAt int      `json:"defined_at"`
	Reads     int      `json:"reads"`
	Writes    int      `json:"writes"`
	FlowsTo   []string `json:"flows_to,omitempty"`
	FlowsFrom []string `json:"flows_from,omitempty"`
}

// ReturnFlow represents return value data flow.
type ReturnFlow struct {
	Line      int      `json:"line"`
	Variables []string `json:"variables"`
}

// DataflowWarning represents a dataflow warning.
type DataflowWarning struct {
	Type     string `json:"type"`
	Variable string `json:"variable"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// DataflowEdgeInfo represents a dataflow edge.
type DataflowEdgeInfo struct {
	From     string `json:"from"`
	To       string `json:"to"`
	EdgeType string `json:"edge_type"`
	Line     int    `json:"line"`
}

func (s *Server) analyzeDataflow(name, file string, detail bool) *DataflowResponse {
	result := &DataflowResponse{
		Function: name,
	}

	// Find the function
	var nodeID int64
	var nodeFile string
	var nodeLine int
	query := `SELECT id, file, line FROM nodes WHERE name = ? AND kind IN ('function', 'method')`
	args := []interface{}{name}
	if file != "" {
		query += " AND file LIKE ?"
		args = append(args, "%"+file+"%")
	}
	query += " LIMIT 1"

	err := s.db.QueryRow(query, args...).Scan(&nodeID, &nodeFile, &nodeLine)
	if err != nil {
		return result
	}

	result.File = nodeFile
	result.Line = nodeLine

	// Get dataflow edges if available
	if detail {
		rows, err := s.db.Query(`
			SELECT from_var, to_var, edge_type, line
			FROM dataflow_edges
			WHERE node_id = ?
			ORDER BY line
		`, nodeID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var edge DataflowEdgeInfo
				if err := rows.Scan(&edge.From, &edge.To, &edge.EdgeType, &edge.Line); err == nil {
					result.DataflowEdges = append(result.DataflowEdges, edge)
				}
			}
		}
	}

	return result
}

// handleDiffImpact handles diff impact analysis requests.
func (s *Server) handleDiffImpact(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req DiffImpactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	result := s.analyzeDiffImpact(req.Branch, req.Depth, req.Callers)
	writeJSON(w, http.StatusOK, result)
}

// DiffImpactRequest represents a diff impact request.
type DiffImpactRequest struct {
	Branch  string `json:"branch,omitempty"`
	Depth   int    `json:"depth,omitempty"`
	Callers bool   `json:"callers"`
}

// DiffImpactResponse represents a diff impact response.
type DiffImpactResponse struct {
	Branch         string             `json:"branch,omitempty"`
	ChangedFiles   []string           `json:"changed_files"`
	ChangedSymbols []DiffSymbolChange `json:"changed_symbols"`
	Impact         DiffImpactSummary  `json:"impact"`
	Callers        []DiffCallerInfo   `json:"callers,omitempty"`
}

// DiffSymbolChange represents a changed symbol.
type DiffSymbolChange struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	ChangeType string `json:"change_type"`
}

// DiffImpactSummary summarizes impact.
type DiffImpactSummary struct {
	DirectCallers     int      `json:"direct_callers"`
	TransitiveCallers int      `json:"transitive_callers"`
	AffectedFiles     []string `json:"affected_files"`
	AffectedSymbols   []string `json:"affected_symbols"`
}

// DiffCallerInfo represents caller information.
type DiffCallerInfo struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Distance int    `json:"distance"`
}

func (s *Server) analyzeDiffImpact(branch string, depth int, includeCallers bool) *DiffImpactResponse {
	result := &DiffImpactResponse{
		Branch:         branch,
		ChangedFiles:   []string{},
		ChangedSymbols: []DiffSymbolChange{},
	}

	if depth <= 0 {
		depth = 3
	}

	// This would require git integration - return basic info for now
	// The actual implementation would analyze git diff and map to nodes
	return result
}

// handleOwners handles code owners analysis requests.
func (s *Server) handleOwners(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req OwnersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	result := s.analyzeOwners(req.Owner, req.Files, req.Kind, req.Boundary, req.NoTests)
	writeJSON(w, http.StatusOK, result)
}

// OwnersRequest represents an owners request.
type OwnersRequest struct {
	Owner    string   `json:"owner,omitempty"`
	Files    []string `json:"files,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Boundary bool     `json:"boundary"`
	NoTests  bool     `json:"no_tests"`
}

// OwnersResponse represents an owners response.
type OwnersResponse struct {
	CodeownersFile string         `json:"codeowners_file,omitempty"`
	Files          []FileOwners   `json:"files,omitempty"`
	Symbols        []SymbolOwners `json:"symbols,omitempty"`
	Boundaries     []BoundaryEdge `json:"boundaries,omitempty"`
	Summary        OwnersSummary  `json:"summary"`
}

// FileOwners represents file ownership.
type FileOwners struct {
	File   string   `json:"file"`
	Owners []string `json:"owners"`
}

// SymbolOwners represents symbol ownership.
type SymbolOwners struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Owners []string `json:"owners"`
}

// BoundaryEdge represents a cross-owner call.
type BoundaryEdge struct {
	From     Endpoint `json:"from"`
	To       Endpoint `json:"to"`
	EdgeKind string   `json:"edge_kind"`
}

// Endpoint represents a call endpoint.
type Endpoint struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Owners []string `json:"owners"`
}

// OwnersSummary summarizes ownership.
type OwnersSummary struct {
	TotalFiles     int            `json:"total_files"`
	TotalSymbols   int            `json:"total_symbols"`
	ByOwner        map[string]int `json:"by_owner"`
	BoundaryEdges  int            `json:"boundary_edges"`
	UnownedFiles   int            `json:"unowned_files"`
	UnownedSymbols int            `json:"unowned_symbols"`
}

func (s *Server) analyzeOwners(owner string, files []string, kind string, boundary, noTests bool) *OwnersResponse {
	result := &OwnersResponse{
		Summary: OwnersSummary{
			ByOwner: make(map[string]int),
		},
	}

	// Get files/symbols from database and map to owners
	// This is a simplified implementation
	query := `SELECT name, kind, file, line FROM nodes WHERE 1=1`
	var args []interface{}

	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}
	if noTests {
		query += " AND file NOT LIKE '%_test%' AND file NOT LIKE '%_spec%'"
	}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, "%"+f+"%")
		}
		query += " AND (" + strings.Join(placeholders, " OR ") + ")"
	}

	rows, err := s.db.Query(query, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sym SymbolOwners
			if err := rows.Scan(&sym.Name, &sym.Kind, &sym.File, &sym.Line); err == nil {
				result.Symbols = append(result.Symbols, sym)
				result.Summary.TotalSymbols++
			}
		}
	}

	return result
}

// handleTriage handles triage analysis requests.
func (s *Server) handleTriage(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req TriageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	top := req.Top
	if top <= 0 {
		top = 20
	}

	result := s.analyzeTriage(req.Files, req.Base, top, req.SortBy)
	writeJSON(w, http.StatusOK, result)
}

// TriageRequest represents a triage request.
type TriageRequest struct {
	Files  []string `json:"files,omitempty"`
	Base   string   `json:"base,omitempty"`
	Top    int      `json:"top,omitempty"`
	SortBy string   `json:"sort_by,omitempty"`
}

// TriageResponse represents a triage response.
type TriageResponse struct {
	TotalFiles   int           `json:"total_files"`
	TotalSymbols int           `json:"total_symbols"`
	Items        []TriageItem  `json:"items"`
	Summary      TriageSummary `json:"summary"`
}

// TriageItem represents a triaged item.
type TriageItem struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	RiskScore   float64 `json:"risk_score"`
	ImpactScore float64 `json:"impact_score"`
	Complexity  int     `json:"complexity"`
	Callers     int     `json:"callers"`
	Role        string  `json:"role"`
	Reason      string  `json:"reason"`
}

// TriageSummary summarizes the triage.
type TriageSummary struct {
	HighRisk    int `json:"high_risk"`
	MediumRisk  int `json:"medium_risk"`
	LowRisk     int `json:"low_risk"`
	EntryPoints int `json:"entry_points"`
	CoreFuncs   int `json:"core_funcs"`
}

func (s *Server) analyzeTriage(files []string, base string, top int, sortBy string) *TriageResponse {
	result := &TriageResponse{
		Items: []TriageItem{},
	}

	// Get symbols with caller counts and complexity
	query := `
		SELECT n.name, n.kind, n.file, n.line, n.role,
			   COALESCE(c.cyclomatic, 1) as complexity,
			   (SELECT COUNT(*) FROM edges e WHERE e.target_id = n.id AND e.kind = 'calls') as callers
		FROM nodes n
		LEFT JOIN function_complexity c ON c.node_id = n.id
		WHERE n.kind IN ('function', 'method')
	`
	var args []interface{}

	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "n.file LIKE ?"
			args = append(args, "%"+f+"%")
		}
		query += " AND (" + strings.Join(placeholders, " OR ") + ")"
	}

	query += " ORDER BY callers DESC LIMIT ?"
	args = append(args, top)

	rows, err := s.db.Query(query, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item TriageItem
			var role sql.NullString
			if err := rows.Scan(&item.Name, &item.Kind, &item.File, &item.Line, &role, &item.Complexity, &item.Callers); err == nil {
				if role.Valid {
					item.Role = role.String
				}
				// Calculate risk and impact scores
				item.ImpactScore = float64(item.Callers) * 0.5
				item.RiskScore = float64(item.Complexity)*0.3 + item.ImpactScore*0.7
				result.Items = append(result.Items, item)
			}
		}
	}

	// Calculate summary
	for _, item := range result.Items {
		if item.RiskScore >= 5 {
			result.Summary.HighRisk++
		} else if item.RiskScore >= 2 {
			result.Summary.MediumRisk++
		} else {
			result.Summary.LowRisk++
		}
		if item.Role == "entry" {
			result.Summary.EntryPoints++
		}
	}

	return result
}

// handleCoChange handles co-change analysis requests.
func (s *Server) handleCoChange(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CoChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	result := s.analyzeCoChange(req.File, req.Since, req.MinSupport, req.MinJaccard, req.Limit, req.NoTests)
	writeJSON(w, http.StatusOK, result)
}

// CoChangeRequest represents a co-change request.
type CoChangeRequest struct {
	File       string  `json:"file,omitempty"`
	Since      string  `json:"since,omitempty"`
	MinSupport int     `json:"min_support,omitempty"`
	MinJaccard float64 `json:"min_jaccard,omitempty"`
	Limit      int     `json:"limit,omitempty"`
	NoTests    bool    `json:"no_tests"`
}

// CoChangeResponse represents a co-change response.
type CoChangeResponse struct {
	Pairs          []CoChangePair    `json:"pairs,omitempty"`
	Partners       []CoChangePartner `json:"partners,omitempty"`
	PairsFound     int               `json:"pairs_found"`
	CommitsScanned int               `json:"commits_scanned"`
	Since          string            `json:"since"`
}

// CoChangePair represents a co-change pair.
type CoChangePair struct {
	FileA      string  `json:"file_a"`
	FileB      string  `json:"file_b"`
	CoCount    int     `json:"co_count"`
	Jaccard    float64 `json:"jaccard"`
	LastCommit string  `json:"last_commit,omitempty"`
}

// CoChangePartner represents a co-change partner for a file.
type CoChangePartner struct {
	File    string  `json:"file"`
	Count   int     `json:"count"`
	Jaccard float64 `json:"jaccard"`
}

func (s *Server) analyzeCoChange(file, since string, minSupport int, minJaccard float64, limit int, noTests bool) *CoChangeResponse {
	result := &CoChangeResponse{
		Since: since,
	}

	if minSupport <= 0 {
		minSupport = 3
	}
	if minJaccard <= 0 {
		minJaccard = 0.3
	}
	if limit <= 0 {
		limit = 20
	}

	// Get root directory - use current directory
	rootDir := "."

	// Scan git history
	commits := scanGitHistory(rootDir, since)
	result.CommitsScanned = len(commits)

	if len(commits) == 0 {
		return result
	}

	// Known files from database not needed for co-change analysis
	knownFiles := make(map[string]bool)

	if file != "" {
		// Find partners for a specific file
		partners := computeCoChangePartners(commits, file, knownFiles, minSupport)
		for _, p := range partners {
			if p.Jaccard >= minJaccard {
				if noTests && isTestFile(p.File) {
					continue
				}
				result.Partners = append(result.Partners, p)
			}
		}
		// Sort by Jaccard descending
		for i := 0; i < len(result.Partners)-1; i++ {
			for j := i + 1; j < len(result.Partners); j++ {
				if result.Partners[i].Jaccard < result.Partners[j].Jaccard {
					result.Partners[i], result.Partners[j] = result.Partners[j], result.Partners[i]
				}
			}
		}
		if len(result.Partners) > limit {
			result.Partners = result.Partners[:limit]
		}
	} else {
		// Find all co-change pairs
		pairs := computeCoChanges(commits, minSupport, 50)
		for _, pair := range pairs {
			if pair.Jaccard >= minJaccard {
				if noTests && (isTestFile(pair.FileA) || isTestFile(pair.FileB)) {
					continue
				}
				result.Pairs = append(result.Pairs, pair)
			}
		}
		// Sort by Jaccard descending
		for i := 0; i < len(result.Pairs)-1; i++ {
			for j := i + 1; j < len(result.Pairs); j++ {
				if result.Pairs[i].Jaccard < result.Pairs[j].Jaccard {
					result.Pairs[i], result.Pairs[j] = result.Pairs[j], result.Pairs[i]
				}
			}
		}
		if len(result.Pairs) > limit {
			result.Pairs = result.Pairs[:limit]
		}
	}

	result.PairsFound = len(result.Pairs) + len(result.Partners)
	return result
}

// CommitInfo represents a parsed commit
type CommitInfo struct {
	SHA   string
	Epoch int64
	Files []string
}

func scanGitHistory(rootDir, since string) []CommitInfo {
	args := []string{
		"log",
		"--name-only",
		"--pretty=format:%H%n%at",
		"--no-merges",
		"--diff-filter=AMRC",
		fmt.Sprintf("--since=%s", since),
		"--", ".",
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var commits []CommitInfo
	blocks := strings.Split(strings.TrimSpace(string(output)), "\n\n")

	for _, block := range blocks {
		lines := strings.Split(block, "\n")
		if len(lines) < 2 {
			continue
		}
		sha := lines[0]
		epoch, err := strconv.ParseInt(lines[1], 10, 64)
		if err != nil {
			continue
		}
		var files []string
		for _, f := range lines[2:] {
			f = strings.TrimSpace(f)
			if f != "" {
				files = append(files, filepath.ToSlash(f))
			}
		}
		if len(files) > 0 {
			commits = append(commits, CommitInfo{
				SHA:   sha,
				Epoch: epoch,
				Files: files,
			})
		}
	}

	return commits
}

func computeCoChanges(commits []CommitInfo, minSupport, maxFilesPerCommit int) []CoChangePair {
	fileCommitCount := make(map[string]int)
	pairCount := make(map[string]int)

	for _, commit := range commits {
		if len(commit.Files) > maxFilesPerCommit {
			continue
		}

		// Count per-file commits
		for _, f := range commit.Files {
			fileCommitCount[f]++
		}

		// Count pairs
		for i := 0; i < len(commit.Files); i++ {
			for j := i + 1; j < len(commit.Files); j++ {
				a, b := commit.Files[i], commit.Files[j]
				if a > b {
					a, b = b, a
				}
				key := a + "||" + b
				pairCount[key]++
			}
		}
	}

	// Compute Jaccard similarity and filter
	var pairs []CoChangePair
	for key, count := range pairCount {
		if count < minSupport {
			continue
		}
		parts := strings.Split(key, "||")
		if len(parts) != 2 {
			continue
		}
		a, b := parts[0], parts[1]
		countA := fileCommitCount[a]
		countB := fileCommitCount[b]
		jaccard := float64(count) / float64(countA+countB-count)
		pairs = append(pairs, CoChangePair{
			FileA:   a,
			FileB:   b,
			CoCount: count,
			Jaccard: jaccard,
		})
	}

	return pairs
}

func computeCoChangePartners(commits []CommitInfo, targetFile string, knownFiles map[string]bool, minSupport int) []CoChangePartner {
	fileCommitCount := make(map[string]int)
	pairCount := make(map[string]int)

	for _, commit := range commits {
		// Check if target file is in this commit
		hasTarget := false
		for _, f := range commit.Files {
			if f == targetFile {
				hasTarget = true
				break
			}
		}
		if !hasTarget {
			continue
		}

		for _, f := range commit.Files {
			fileCommitCount[f]++
		}

		for _, f := range commit.Files {
			if f != targetFile {
				if len(knownFiles) > 0 && !knownFiles[f] {
					continue
				}
				pairCount[f]++
			}
		}
	}

	targetCount := fileCommitCount[targetFile]
	var partners []CoChangePartner
	for file, count := range pairCount {
		if count < minSupport {
			continue
		}
		otherCount := fileCommitCount[file]
		jaccard := float64(count) / float64(targetCount+otherCount-count)
		partners = append(partners, CoChangePartner{
			File:    file,
			Count:   count,
			Jaccard: jaccard,
		})
	}

	return partners
}

// handleSnapshot handles snapshot operations.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	action := params.ByName("action")

	switch action {
	case "list":
		s.listSnapshots(w, r)
	case "save":
		s.saveSnapshot(w, r)
	case "restore":
		s.restoreSnapshot(w, r)
	case "delete":
		s.deleteSnapshot(w, r)
	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.unknownSnapshotAction"))
	}
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshotsDir := s.getSnapshotsDir()

	entries, err := os.ReadDir(snapshotsDir)
	if err != nil || len(entries) == 0 {
		result := &SnapshotListResponse{
			Snapshots: []SnapshotInfo{},
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	var snapshots []SnapshotInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".db")
		snapshots = append(snapshots, SnapshotInfo{
			Name:      name,
			Path:      snapshotsDir + "/" + entry.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
		})
	}

	// Sort by creation time (newest first)
	for i := 0; i < len(snapshots)-1; i++ {
		for j := i + 1; j < len(snapshots); j++ {
			if snapshots[i].CreatedAt < snapshots[j].CreatedAt {
				snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
			}
		}
	}

	result := &SnapshotListResponse{
		Snapshots: snapshots,
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) saveSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.snapshotNameRequired"))
		return
	}

	snapshotsDir := s.getSnapshotsDir()
	dest := snapshotsDir + "/" + req.Name + ".db"

	// Ensure directory exists
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "IO_ERROR", fmt.Sprintf("Failed to create snapshots directory: %v", err))
		return
	}

	// Check if exists
	if _, err := os.Stat(dest); err == nil {
		writeError(w, http.StatusConflict, "ALREADY_EXISTS", i18n.T("api.error.snapshotAlreadyExists"))
		return
	}

	// Get database path
	dbPath := s.getDBPath()

	// Copy database file
	if err := copyFile(dbPath, dest); err != nil {
		writeError(w, http.StatusInternalServerError, "IO_ERROR", fmt.Sprintf("Failed to copy database: %v", err))
		return
	}

	// Remove WAL/SHM sidecar files from snapshot
	os.Remove(dest + "-wal")
	os.Remove(dest + "-shm")

	result := &SnapshotResponse{
		Name:    req.Name,
		Message: "Snapshot saved successfully",
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.snapshotNameRequired"))
		return
	}

	snapshotsDir := s.getSnapshotsDir()
	src := snapshotsDir + "/" + req.Name + ".db"

	if _, err := os.Stat(src); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.snapshotNotFound"))
		return
	}

	dbPath := s.getDBPath()

	// Remove WAL/SHM sidecar files for clean restore
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	// Copy snapshot to database location
	if err := copyFile(src, dbPath); err != nil {
		writeError(w, http.StatusInternalServerError, "IO_ERROR", fmt.Sprintf("Failed to restore snapshot: %v", err))
		return
	}

	result := &SnapshotResponse{
		Name:    req.Name,
		Message: "Snapshot restored successfully",
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.snapshotNameRequired"))
		return
	}

	snapshotsDir := s.getSnapshotsDir()
	target := snapshotsDir + "/" + req.Name + ".db"

	if _, err := os.Stat(target); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.snapshotNotFound"))
		return
	}

	if err := os.Remove(target); err != nil {
		writeError(w, http.StatusInternalServerError, "IO_ERROR", fmt.Sprintf("Failed to delete snapshot: %v", err))
		return
	}

	result := &SnapshotResponse{
		Name:    req.Name,
		Message: "Snapshot deleted successfully",
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getSnapshotsDir() string {
	dbPath := s.getDBPath()
	return dbPath[:len(dbPath)-len("/axons.db")] + "/snapshots"
}

func (s *Server) getDBPath() string {
	if s.config != nil && s.config.Database.Path != "" {
		return s.config.Database.Path
	}
	return ".axons/axons.db"
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// SnapshotRequest represents a snapshot request.
type SnapshotRequest struct {
	Name string `json:"name"`
}

// SnapshotResponse represents a snapshot response.
type SnapshotResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// SnapshotListResponse represents a snapshot list response.
type SnapshotListResponse struct {
	Snapshots []SnapshotInfo `json:"snapshots"`
}

// SnapshotInfo represents snapshot information.
type SnapshotInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

// BranchCompareRequest represents a branch compare request.
type BranchCompareRequest struct {
	BaseRef   string `json:"base_ref"`
	TargetRef string `json:"target_ref"`
	Depth     int    `json:"depth,omitempty"`
	NoTests   bool   `json:"no_tests"`
}

// BranchCompareResponse represents a branch compare response.
type BranchCompareResponse struct {
	BaseRef      string         `json:"base_ref"`
	TargetRef    string         `json:"target_ref"`
	BaseSHA      string         `json:"base_sha"`
	TargetSHA    string         `json:"target_sha"`
	ChangedFiles []string       `json:"changed_files"`
	Added        []SymbolDiff   `json:"added"`
	Removed      []SymbolDiff   `json:"removed"`
	Changed      []SymbolChange `json:"changed"`
	Summary      CompareSummary `json:"summary"`
}

// SymbolDiff represents an added or removed symbol.
type SymbolDiff struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Impact []string `json:"impact,omitempty"`
}

// SymbolChange represents a changed symbol.
type SymbolChange struct {
	Name    string     `json:"name"`
	Kind    string     `json:"kind"`
	File    string     `json:"file"`
	Base    SymbolInfo `json:"base"`
	Target  SymbolInfo `json:"target"`
	Changes ChangeDiff `json:"changes"`
	Impact  []string   `json:"impact,omitempty"`
}

// SymbolInfo contains symbol metadata.
type SymbolInfo struct {
	Line      int `json:"line"`
	LineCount int `json:"line_count"`
	FanIn     int `json:"fan_in"`
	FanOut    int `json:"fan_out"`
}

// ChangeDiff shows the difference between base and target.
type ChangeDiff struct {
	LineCount int `json:"line_count"`
	FanIn     int `json:"fan_in"`
	FanOut    int `json:"fan_out"`
}

// CompareSummary contains summary statistics.
type CompareSummary struct {
	Added         int `json:"added"`
	Removed       int `json:"removed"`
	Changed       int `json:"changed"`
	TotalImpacted int `json:"total_impacted"`
	FilesAffected int `json:"files_affected"`
}

// handleBranchCompare handles branch comparison.
func (s *Server) handleBranchCompare(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req BranchCompareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// This is a placeholder implementation
	// A full implementation would:
	// 1. Create git worktrees for both refs
	// 2. Build graphs for each
	// 3. Compare the symbols
	result := &BranchCompareResponse{
		BaseRef:      req.BaseRef,
		TargetRef:    req.TargetRef,
		BaseSHA:      "placeholder",
		TargetSHA:    "placeholder",
		ChangedFiles: []string{},
		Added:        []SymbolDiff{},
		Removed:      []SymbolDiff{},
		Changed:      []SymbolChange{},
		Summary:      CompareSummary{},
	}

	writeJSON(w, http.StatusOK, result)
}

// Registry handlers

// handleListRepos handles list repositories requests.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// For now, return empty list as registry is managed locally
	// In a full implementation, this would query a central registry
	repos := []RegistryRepo{}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"repos": repos,
		"count": len(repos),
	})
}

// handleRegisterRepo handles register repository requests.
func (s *Server) handleRegisterRepo(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req RegisterRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	// For now, return a basic response
	// In a full implementation, this would register to a central registry
	repo := &RegistryRepo{
		Path:    req.Path,
		Name:    req.Name,
		DBPath:  req.Path + "/.axons/graph.db",
		AddedAt: time.Now().Format(time.RFC3339),
	}
	if repo.Name == "" {
		// Use basename of path as name
		repo.Name = filepath.Base(req.Path)
	}

	writeJSON(w, http.StatusOK, repo)
}

// handleGetRepo handles get repository requests.
func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.repoNameRequired"))
		return
	}

	// For now, return not found
	// In a full implementation, this would query a central registry
	writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.repositoryNotFound", map[string]string{"name": name}))
}

// handleUnregisterRepo handles unregister repository requests.
func (s *Server) handleUnregisterRepo(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.repoNameRequired"))
		return
	}

	// For now, return success
	// In a full implementation, this would remove from a central registry
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "removed",
		"message": "Repository " + name + " removed",
	})
}

// handlePruneRepos handles prune repositories requests.
func (s *Server) handlePruneRepos(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req PruneReposRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default values if body is empty
		req.TTL = 30
	}

	if req.TTL <= 0 {
		req.TTL = 30
	}

	// For now, return empty prune result
	// In a full implementation, this would prune stale entries
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pruned": []interface{}{},
		"count":  0,
	})
}

// Registry request/response types

// RegisterRepoRequest represents a register repo request.
type RegisterRepoRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// RegistryRepo represents a registered repository.
type RegistryRepo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	DBPath         string `json:"db_path"`
	AddedAt        string `json:"added_at"`
	LastAccessedAt string `json:"last_accessed_at"`
}

// PruneReposRequest represents a prune repos request.
type PruneReposRequest struct {
	TTL     int      `json:"ttl"`
	Exclude []string `json:"exclude,omitempty"`
	DryRun  bool     `json:"dry_run"`
}

// CFG handlers

// handleCFG handles CFG requests.
func (s *Server) handleCFG(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CFGRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.functionNameRequired"))
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	// Use project-specific repo if project ID is provided
	cfgRepo := s.repo
	if req.ProjectID != "" {
		if pr, err := s.projectRepo(req.ProjectID); err == nil {
			cfgRepo = pr
		}
	}

	result := s.getCFG(cfgRepo.DB(), req.Name, req.File, req.Kind, req.NoTests, limit)
	writeJSON(w, http.StatusOK, result)
}

// CFGRequest represents a CFG request.
type CFGRequest struct {
	Name      string   `json:"name"`
	File      []string `json:"file,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	NoTests   bool     `json:"no_tests"`
	Limit     int      `json:"limit,omitempty"`
	ProjectID string   `json:"project_id,omitempty"` // Project-specific DB
}

// CFGResponse represents a CFG response.
type CFGResponse struct {
	Count   int         `json:"count"`
	Results []CFGResult `json:"results"`
}

// CFGResult represents CFG data for a function.
type CFGResult struct {
	Name    string     `json:"name"`
	Kind    string     `json:"kind"`
	File    string     `json:"file"`
	Line    int        `json:"line"`
	Blocks  []CFGBlock `json:"blocks"`
	Edges   []CFGEdge  `json:"edges"`
	Summary struct {
		BlockCount int `json:"block_count"`
		EdgeCount  int `json:"edge_count"`
	} `json:"summary"`
}

// CFGBlock represents a basic block in the CFG.
type CFGBlock struct {
	Index     int    `json:"index"`
	Type      string `json:"type"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Label     string `json:"label,omitempty"`
}

// CFGEdge represents an edge in the CFG.
type CFGEdge struct {
	Source     int    `json:"source"`
	SourceType string `json:"source_type,omitempty"`
	Target     int    `json:"target"`
	TargetType string `json:"target_type,omitempty"`
	Kind       string `json:"kind"`
}

func (s *Server) getCFG(db *sql.DB, name string, files []string, kind string, noTests bool, limit int) *CFGResponse {
	result := &CFGResponse{
		Results: []CFGResult{},
	}

	// Check if CFG tables exist
	var tableExists int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cfg_blocks'").Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return result
	}

	// Build query for finding functions
	query := `SELECT id, name, kind, file, line FROM nodes WHERE name LIKE ? AND kind IN ('function', 'method')`
	args := []interface{}{"%" + name + "%"}

	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}

	for _, f := range files {
		query += " AND file LIKE ?"
		args = append(args, "%"+f+"%")
	}

	if noTests {
		query += " AND file NOT LIKE '%_test%' AND file NOT LIKE '%.spec.%' AND file NOT LIKE '%.test.%'"
	}

	query += " LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var nodeID int64
		var nodeName, nodeKind, nodeFile string
		var nodeLine int
		if err := rows.Scan(&nodeID, &nodeName, &nodeKind, &nodeFile, &nodeLine); err != nil {
			continue
		}

		blocks, edges := s.getCFGForNode(db, nodeID)
		result.Results = append(result.Results, CFGResult{
			Name:   nodeName,
			Kind:   nodeKind,
			File:   nodeFile,
			Line:   nodeLine,
			Blocks: blocks,
			Edges:  edges,
		})
		result.Results[len(result.Results)-1].Summary.BlockCount = len(blocks)
		result.Results[len(result.Results)-1].Summary.EdgeCount = len(edges)
	}

	result.Count = len(result.Results)
	return result
}

func (s *Server) getCFGForNode(db *sql.DB, nodeID int64) ([]CFGBlock, []CFGEdge) {
	// Get blocks
	blockRows, err := db.Query(`
		SELECT id, block_type, start_line, end_line
		FROM cfg_blocks WHERE node_id = ?
		ORDER BY start_line
	`, nodeID)
	if err != nil {
		return nil, nil
	}
	defer blockRows.Close()

	blocks := []CFGBlock{}
	blockIDToIndex := make(map[int64]int)
	index := 0
	for blockRows.Next() {
		var id int64
		var blockType string
		var startLine, endLine int
		if err := blockRows.Scan(&id, &blockType, &startLine, &endLine); err != nil {
			continue
		}
		blocks = append(blocks, CFGBlock{
			Index:     index,
			Type:      blockType,
			StartLine: startLine,
			EndLine:   endLine,
		})
		blockIDToIndex[id] = index
		index++
	}

	// Get edges
	edgeRows, err := db.Query(`
		SELECT e.source_block_id, e.target_block_id, e.edge_type
		FROM cfg_edges e
		JOIN cfg_blocks b1 ON e.source_block_id = b1.id
		JOIN cfg_blocks b2 ON e.target_block_id = b2.id
		WHERE b1.node_id = ? AND b2.node_id = ?
	`, nodeID, nodeID)
	if err != nil {
		return blocks, nil
	}
	defer edgeRows.Close()

	edges := []CFGEdge{}
	for edgeRows.Next() {
		var sourceID, targetID int64
		var edgeType string
		if err := edgeRows.Scan(&sourceID, &targetID, &edgeType); err != nil {
			continue
		}
		edges = append(edges, CFGEdge{
			Source: blockIDToIndex[sourceID],
			Target: blockIDToIndex[targetID],
			Kind:   edgeType,
		})
	}

	return blocks, edges
}

// Watch handlers

// handleWatchStart handles watch start requests.
func (s *Server) handleWatchStart(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req WatchStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Validate root directory
	if req.RootDir == "" {
		req.RootDir = "."
	}
	absRoot, err := filepath.Abs(req.RootDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid root directory: %v", err))
		return
	}

	// Check if directory exists
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Directory does not exist: %s", absRoot))
		return
	}

	// Check if already watching this directory
	s.watchMgr.mu.RLock()
	if info, exists := s.watchMgr.watchers[absRoot]; exists && info.Status == "running" {
		s.watchMgr.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":     "already_watching",
			"root_dir":   absRoot,
			"start_time": info.StartTime,
		})
		return
	}
	s.watchMgr.mu.RUnlock()

	// Get supported extensions from registry
	registry := extractors.DefaultRegistry
	var extensions []string
	for _, lang := range registry.ListLanguages() {
		extensions = append(extensions, lang.Extensions...)
	}

	// Create watcher config
	config := graph.DefaultWatcherConfig()

	// Create watcher
	watcher, err := graph.NewWatcher(absRoot, extensions, config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WATCH_ERROR", fmt.Sprintf("Failed to create watcher: %v", err))
		return
	}

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create watch info
	info := &WatchInfo{
		RootDir:   absRoot,
		Watcher:   watcher,
		Cancel:    cancel,
		StartTime: time.Now(),
		Status:    "running",
	}

	// Store watcher
	s.watchMgr.mu.Lock()
	s.watchMgr.watchers[absRoot] = info
	s.watchMgr.mu.Unlock()

	// Start watching in background
	go func() {
		if err := watcher.Start(ctx); err != nil {
			s.watchMgr.mu.Lock()
			if existing, ok := s.watchMgr.watchers[absRoot]; ok {
				existing.Status = "error"
			}
			s.watchMgr.mu.Unlock()
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":     "started",
		"root_dir":   absRoot,
		"start_time": info.StartTime,
	})
}

// handleWatchStop handles watch stop requests.
func (s *Server) handleWatchStop(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req WatchStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	absRoot := req.RootDir
	if absRoot == "" {
		absRoot = "."
	}
	absRoot, err := filepath.Abs(absRoot)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid root directory: %v", err))
		return
	}

	s.watchMgr.mu.Lock()
	defer s.watchMgr.mu.Unlock()

	info, exists := s.watchMgr.watchers[absRoot]
	if !exists {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.noWatcherFound"))
		return
	}

	// Cancel context and stop watcher
	if info.Cancel != nil {
		info.Cancel()
	}
	if info.Watcher != nil {
		info.Watcher.Stop()
	}
	info.Status = "stopped"

	delete(s.watchMgr.watchers, absRoot)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "stopped",
		"root_dir": absRoot,
	})
}

// handleWatchStatus handles watch status requests.
func (s *Server) handleWatchStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	rootDir := r.URL.Query().Get("root_dir")

	s.watchMgr.mu.RLock()
	defer s.watchMgr.mu.RUnlock()

	if rootDir != "" {
		absRoot, err := filepath.Abs(rootDir)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid root directory: %v", err))
			return
		}

		info, exists := s.watchMgr.watchers[absRoot]
		if !exists {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status":   "not_watching",
				"root_dir": absRoot,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":     info.Status,
			"root_dir":   info.RootDir,
			"start_time": info.StartTime,
		})
		return
	}

	// Return status of all watchers
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": len(s.watchMgr.watchers),
	})
}

// handleWatchList handles watch list requests.
func (s *Server) handleWatchList(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	s.watchMgr.mu.RLock()
	defer s.watchMgr.mu.RUnlock()

	watchers := make([]map[string]interface{}, 0, len(s.watchMgr.watchers))
	for _, info := range s.watchMgr.watchers {
		watchers = append(watchers, map[string]interface{}{
			"root_dir":   info.RootDir,
			"status":     info.Status,
			"start_time": info.StartTime,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"watchers": watchers,
		"count":    len(watchers),
	})
}

// WatchStartRequest represents a watch start request.
type WatchStartRequest struct {
	RootDir string `json:"root_dir"`
}

// WatchStopRequest represents a watch stop request.
type WatchStopRequest struct {
	RootDir string `json:"root_dir"`
}

// ProjectWatchRequest represents a project watch request.
type ProjectWatchRequest struct {
	ProjectID int64 `json:"project_id"`
}

// ProjectWatchStatusResponse represents project watch status.
type ProjectWatchStatusResponse struct {
	ProjectID     string `json:"project_id"`
	WatchEnabled  bool   `json:"watch_enabled"`
	WatchStatus   string `json:"watch_status"`
	IsRunning     bool   `json:"is_running"`
	StartTime     string `json:"start_time,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	ChangesQueued int    `json:"changes_queued"`
}

// Project handlers

// handleListProjects handles list projects requests.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projects, err := s.globalRepo.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to list projects: %v", err))
		return
	}

	// Ensure projects is never null for JSON response
	if projects == nil {
		projects = []*types.Project{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": projects,
		"count":    len(projects),
	})
}

// handleCreateProject handles create project requests.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.projectNameRequired"))
		return
	}

	if req.RootPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.rootPathRequired"))
		return
	}

	// Check if project with same name already exists
	allProjects, _ := s.globalRepo.ListProjects()
	for _, p := range allProjects {
		if p.Name == req.Name {
			writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.projectNameExists"))
			return
		}
	}

	// Generate UUID for new project
	newProjectID := generateProjectID()
	project, err := s.globalRepo.CreateProject(newProjectID, req.Name, req.RootPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to create project: %v", err))
		return
	}

	// Pre-warm the project database (creates the .db file and runs project migrations)
	if _, err := s.dbMgr.ProjectDB(project.ID); err != nil {
		fmt.Printf("Warning: failed to initialize project DB for %s: %v\n", project.ID, err)
	}

	writeJSON(w, http.StatusCreated, project)
}

// handleNewProject handles new project creation requests.
// It creates a new project directory with language-specific template files.
func (s *Server) handleNewProject(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req NewProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Validate required fields
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.projectNameRequired"))
		return
	}
	if req.Location == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.locationRequired"))
		return
	}

	// Sanitize project name (remove potentially dangerous characters)
	projectName := filepath.Base(req.Name)
	if projectName == "." || projectName == ".." {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectName"))
		return
	}

	// Resolve absolute path for location
	absLocation, err := filepath.Abs(req.Location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid location path: %v", err))
		return
	}

	// Check if location directory exists
	if _, err := os.Stat(absLocation); os.IsNotExist(err) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Location directory does not exist: %s", absLocation))
		return
	}

	// Build project directory path
	projectDir := filepath.Join(absLocation, projectName)

	// Check if project directory already exists
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Directory already exists: %s", projectDir))
		return
	}

	// Check if project with same name already exists in database
	allProjects, _ := s.globalRepo.ListProjects()
	for _, p := range allProjects {
		if p.Name == req.Name {
			writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.projectNameExists"))
			return
		}
	}

	// Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_ERROR", fmt.Sprintf("Failed to create project directory: %v", err))
		return
	}

	// Track created files for rollback on error
	var createdFiles []string
	needRollback := true
	defer func() {
		if needRollback {
			// Remove created files and directory on error
			for _, f := range createdFiles {
				os.Remove(f)
			}
			os.Remove(projectDir)
		}
	}()

	// Initialize language-specific files
	if req.Language != "" {
		if err := InitLanguageFiles(projectDir, req.Language, req.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "INIT_ERROR", fmt.Sprintf("Failed to initialize project files: %v", err))
			return
		}
	}

	// Initialize git repository if requested
	if req.InitGit {
		cmd := exec.Command("git", "init")
		cmd.Dir = projectDir
		if output, err := cmd.CombinedOutput(); err != nil {
			// Git init failure is not fatal, just log a warning
			fmt.Printf("Warning: failed to initialize git repository: %v\n%s\n", err, output)
		}
	}

	// Generate project ID
	newProjectID := generateProjectID()

	// Create project record in database
	project, err := s.globalRepo.CreateProject(newProjectID, req.Name, projectDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to create project record: %v", err))
		return
	}

	// Pre-warm the project database
	if _, err := s.dbMgr.ProjectDB(project.ID); err != nil {
		fmt.Printf("Warning: failed to initialize project DB for %s: %v\n", project.ID, err)
	}

	// Success - no rollback needed
	needRollback = false

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":          project.ID,
		"name":        project.Name,
		"root_path":   project.RootPath,
		"language":    req.Language,
		"git_init":    req.InitGit,
		"created_at":  project.CreatedAt,
	})
}

// handleGetProject handles get project requests.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	project, err := s.globalRepo.GetProject(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, project)
}

// handleDeleteProject handles delete project requests.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Check if project exists
	_, err := s.globalRepo.GetProject(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project: %v", err))
		return
	}

	// Stop watcher if running for this project
	s.watchMgr.mu.Lock()
	for rootDir, watchInfo := range s.watchMgr.watchers {
		if watchInfo.ProjectID == id {
			// Cancel the watcher context
			if watchInfo.Cancel != nil {
				watchInfo.Cancel()
			}
			// Stop the watcher
			if watchInfo.Watcher != nil {
				watchInfo.Watcher.Stop()
			}
			// Remove from map
			delete(s.watchMgr.watchers, rootDir)
			break
		}
	}
	s.watchMgr.mu.Unlock()

	// Delete project record from main database
	if err := s.globalRepo.DeleteProject(id); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to delete project: %v", err))
		return
	}

	// Delete project's physical database file (physical isolation)
	if err := s.dbMgr.DeleteProjectDB(id); err != nil {
		fmt.Printf("Warning: failed to delete project DB file for %s: %v\n", id, err)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "deleted",
		"message": fmt.Sprintf("Project %s deleted", id),
	})
}

// handleProjectStats handles project stats requests.
func (s *Server) handleProjectStats(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Use project-specific repository
	projectRepo, err := s.projectRepo(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	stats, err := projectRepo.GetStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project stats: %v", err))
		return
	}

	nodesByKind := make(map[string]int)
	for k, v := range stats.NodesByKind {
		nodesByKind[string(k)] = int(v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id":  id,
		"node_count":  stats.TotalNodes,
		"edge_count":  stats.TotalEdges,
		"file_count":  stats.TotalFiles,
		"nodes_by_kind": nodesByKind,
	})
}

// handleProjectBuildStatus handles requests to check if a project is currently building.
// GET /v1/projects/:id/build-status
//
// Returns the build status for the specified project, allowing the frontend to
// restore the correct UI state after page refresh or project switch.
//
// Response:
//
//	{ "is_building": true,  "task_id": "...", "progress": 45, "phase": "parse", "message": "..." }
//	{ "is_building": false }
func (s *Server) handleProjectBuildStatus(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Search for a running build task associated with this project
	tasks := s.taskMgr.ListTasks()
	for _, t := range tasks {
		if t.Type != "build" {
			continue
		}
		status := t.ToStatus()
		if status.Status != task.StatusRunning && status.Status != task.StatusPending {
			continue
		}
		// Check if task is associated with this project via labels
		if status.Labels != nil {
			if pid, ok := status.Labels["project_id"]; ok && pid == id {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"is_building": true,
					"task_id":     status.ID,
					"progress":    status.Progress,
					"phase":       status.Message,
					"message":     status.Message,
				})
				return
			}
		}
	}

	// No running build task found — check incremental_in_progress metadata as fallback
	projectRepo, err := s.projectRepo(id)
	if err == nil {
		if flag, _ := projectRepo.GetMetadata("incremental_in_progress"); flag != "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"is_building": true,
				"progress":    0,
				"phase":       "",
				"message":     "Incremental build in progress",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"is_building": false,
	})
}

// CreateProjectRequest represents a create project request.
type CreateProjectRequest struct {
	Name        string `json:"name"`
	RootPath    string `json:"root_path"`
	WatchEnable bool   `json:"watch_enable"`
}

// NewProjectRequest represents a new project creation request.
type NewProjectRequest struct {
	Name     string `json:"name"`      // Project name
	Location string `json:"location"`  // Parent directory where project will be created
	Language string `json:"language"`  // Language template (go, javascript, python, etc.)
	InitGit  bool   `json:"init_git"`  // Whether to initialize git repository
}

// handleProjectWatchStart handles project-level watch start requests.
func (s *Server) handleProjectWatchStart(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Get project from database
	project, err := s.globalRepo.GetProject(projectID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project: %v", err))
		return
	}

	absRoot := project.RootPath
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Project directory does not exist: %s", absRoot))
		return
	}

	// Check if already watching this project
	s.watchMgr.mu.RLock()
	if info, exists := s.watchMgr.watchers[absRoot]; exists && info.Status == "running" {
		s.watchMgr.mu.RUnlock()
		// Update database status
		s.globalRepo.UpdateProjectWatchStatus(projectID, "running")
	s.globalRepo.SetProjectWatchEnabled(projectID, true)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":     "already_watching",
			"project_id": projectID,
			"root_dir":   absRoot,
			"start_time": info.StartTime,
		})
		return
	}
	s.watchMgr.mu.RUnlock()

	// Get supported extensions from registry
	registry := extractors.DefaultRegistry
	var extensions []string
	for _, lang := range registry.ListLanguages() {
		extensions = append(extensions, lang.Extensions...)
	}

	// Create watcher config with incremental build callback
	config := graph.DefaultWatcherConfig()
	config.OnChange = func(filePath string, changeType graph.ChangeType) {
		// Record change in database
		s.recordFileChange(projectID, filePath, string(changeType))

		// Broadcast file change event for all files (so frontend can refresh file panel)
		s.eventBroker.BroadcastFileChange(projectID, filePath, string(changeType))

		// Only trigger incremental build for code files
		if extractors.DefaultRegistry.IsSupported(filePath) {
			s.triggerIncrementalBuild(projectID, filePath, string(changeType), project)
		}
	}

	// Create watcher
	watcher, err := graph.NewWatcher(absRoot, extensions, config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WATCH_ERROR", fmt.Sprintf("Failed to create watcher: %v", err))
		return
	}

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create watch info
	info := &WatchInfo{
		RootDir:   absRoot,
		Watcher:   watcher,
		Cancel:    cancel,
		StartTime: time.Now(),
		Status:    "running",
		ProjectID: projectID,
	}

	// Store watcher
	s.watchMgr.mu.Lock()
	s.watchMgr.watchers[absRoot] = info
	s.watchMgr.mu.Unlock()

	// Update database status
	s.globalRepo.UpdateProjectWatchStatus(projectID, "running")
	s.globalRepo.SetProjectWatchEnabled(projectID, true)

	// Start watching in background
	go func() {
		if err := watcher.Start(ctx); err != nil {
			s.watchMgr.mu.Lock()
			if existing, ok := s.watchMgr.watchers[absRoot]; ok {
				existing.Status = "error"
			}
			s.watchMgr.mu.Unlock()
			s.globalRepo.UpdateProjectWatchStatus(projectID, "error")
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":     "started",
		"project_id": projectID,
		"root_dir":   absRoot,
		"start_time": info.StartTime,
	})
}

// handleProjectWatchStop handles project-level watch stop requests.
func (s *Server) handleProjectWatchStop(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Get project from database
	project, err := s.globalRepo.GetProject(projectID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project: %v", err))
		return
	}

	absRoot := project.RootPath

	s.watchMgr.mu.Lock()
	defer s.watchMgr.mu.Unlock()

	info, exists := s.watchMgr.watchers[absRoot]
	if !exists {
		// Update database status even if no active watcher
		s.globalRepo.UpdateProjectWatchStatus(projectID, "stopped")
	s.globalRepo.SetProjectWatchEnabled(projectID, false)
		writeJSON(w, http.StatusOK, map[string]string{
			"status":     "not_watching",
			"project_id": projectID,
		})
		return
	}

	// Cancel context and stop watcher
	if info.Cancel != nil {
		info.Cancel()
	}
	if info.Watcher != nil {
		info.Watcher.Stop()
	}
	info.Status = "stopped"

	delete(s.watchMgr.watchers, absRoot)

	// Update database status
	s.globalRepo.UpdateProjectWatchStatus(projectID, "stopped")
	s.globalRepo.SetProjectWatchEnabled(projectID, false)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "stopped",
		"project_id": projectID,
		"root_dir":   absRoot,
	})
}

// handleProjectWatchStatus handles project-level watch status requests.
func (s *Server) handleProjectWatchStatus(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidProjectId"))
		return
	}

	// Get project from database
	project, err := s.globalRepo.GetProject(projectID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get project: %v", err))
		return
	}

	absRoot := project.RootPath

	// Check active watcher
	s.watchMgr.mu.RLock()
	info, isRunning := s.watchMgr.watchers[absRoot]
	s.watchMgr.mu.RUnlock()

	// Get queued changes count
	changesQueued := s.getQueuedChangesCount(projectID)

	response := ProjectWatchStatusResponse{
		ProjectID:     projectID,
		WatchEnabled:  project.WatchEnabled,
		WatchStatus:   project.WatchStatus,
		IsRunning:     isRunning && info.Status == "running",
		ChangesQueued: changesQueued,
	}

	if isRunning {
		response.StartTime = info.StartTime.Format(time.RFC3339)
		if info.Status == "error" {
			response.LastError = "Watcher encountered an error"
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// handleRestoreWatches restores all watches that were enabled before server shutdown.
func (s *Server) handleRestoreWatches(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Get all projects with watch enabled
	projects, err := s.globalRepo.GetProjectsWithWatchEnabled()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("Failed to get projects: %v", err))
		return
	}

	restored := []string{}
	failed := []map[string]interface{}{}

	for _, project := range projects {
		// Check if directory still exists
		if _, err := os.Stat(project.RootPath); os.IsNotExist(err) {
			failed = append(failed, map[string]interface{}{
				"project_id": project.ID,
				"error":      "Directory no longer exists",
			})
			continue
		}

		// Start watcher for this project
		// Directly call the start logic
		if err := s.startProjectWatch(project.ID, project.RootPath); err != nil {
			failed = append(failed, map[string]interface{}{
				"project_id": project.ID,
				"error":      err.Error(),
			})
			continue
		}

		restored = append(restored, project.ID)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"restored": restored,
		"failed":   failed,
		"count":    len(restored),
	})
}

// startProjectWatch starts watching a project (helper for restore).
func (s *Server) startProjectWatch(projectID string, rootPath string) error {
	absRoot := rootPath
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", absRoot)
	}

	// Check if already watching
	s.watchMgr.mu.RLock()
	if info, exists := s.watchMgr.watchers[absRoot]; exists && info.Status == "running" {
		s.watchMgr.mu.RUnlock()
		return nil
	}
	s.watchMgr.mu.RUnlock()

	// Get project for incremental build callback
	project, err := s.globalRepo.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Get supported extensions
	registry := extractors.DefaultRegistry
	var extensions []string
	for _, lang := range registry.ListLanguages() {
		extensions = append(extensions, lang.Extensions...)
	}

	// Create watcher config
	config := graph.DefaultWatcherConfig()
	config.OnChange = func(filePath string, changeType graph.ChangeType) {
		s.recordFileChange(projectID, filePath, string(changeType))

		// Broadcast file change event for all files (so frontend can refresh file panel)
		s.eventBroker.BroadcastFileChange(projectID, filePath, string(changeType))

		// Only trigger incremental build for code files
		if extractors.DefaultRegistry.IsSupported(filePath) {
			s.triggerIncrementalBuild(projectID, filePath, string(changeType), project)
		}
	}

	// Create watcher
	watcher, err := graph.NewWatcher(absRoot, extensions, config)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Create watch info
	info := &WatchInfo{
		RootDir:   absRoot,
		Watcher:   watcher,
		Cancel:    cancel,
		StartTime: time.Now(),
		Status:    "running",
		ProjectID: projectID,
	}

	// Store watcher
	s.watchMgr.mu.Lock()
	s.watchMgr.watchers[absRoot] = info
	s.watchMgr.mu.Unlock()

	// Update database
	s.globalRepo.UpdateProjectWatchStatus(projectID, "running")
	s.globalRepo.SetProjectWatchEnabled(projectID, true)

	// Start watching
	go func() {
		if err := watcher.Start(ctx); err != nil {
			s.watchMgr.mu.Lock()
			if existing, ok := s.watchMgr.watchers[absRoot]; ok {
				existing.Status = "error"
			}
			s.watchMgr.mu.Unlock()
			s.globalRepo.UpdateProjectWatchStatus(projectID, "error")
		}
	}()

	return nil
}

// recordFileChange records a file change in the database.
func (s *Server) recordFileChange(projectID string, filePath, changeType string) {
	// Insert change record into database
	_, err := s.db.Exec(`
		INSERT INTO change_history (project_id, file_path, change_type, change_time, processed)
		VALUES (?, ?, ?, ?, 0)
	`, projectID, filePath, changeType, time.Now().Format(time.RFC3339))
	if err != nil {
		fmt.Printf("Failed to record file change: %v\n", err)
	}
}

// triggerIncrementalBuild triggers an incremental build for a changed file.
func (s *Server) triggerIncrementalBuild(projectID string, filePath, changeType string, project *types.Project) {
	// Create task for incremental build
	t := s.taskMgr.CreateTask("incremental_build")

	// Note: BroadcastFileChange is already called in the onChange callback,
	// so we don't need to call it again here.

	go func() {
		ctx := t.Context()
		t.SetStatus(task.StatusRunning)
		t.SetProgress(10, 100, fmt.Sprintf("Processing %s: %s", changeType, filePath))

		// Broadcast build progress
		s.eventBroker.BroadcastBuildProgress(t.ID, 10, fmt.Sprintf("Processing %s: %s", changeType, filePath), projectID, "incremental")

		opts := &types.BuildOptions{
			RootDir:     project.RootPath,
			FullBuild:   false,
			ProjectID:   projectID,
			ProjectName: project.Name,
		}

		// Use project-specific DB for incremental build
		pipelineRepo := s.repo
		if projectID != "" {
			if pr, err := s.projectRepo(projectID); err == nil {
				pipelineRepo = pr
			}
		}

		pipeline := graph.NewPipelineWithGlobal(pipelineRepo, s.globalRepo, opts)
		pipeline.SetOnProgress(func(phase string, percent int, message string) {
			s.eventBroker.BroadcastBuildProgress(t.ID, percent, message, projectID, phase)
		})
		result, err := pipeline.Build(ctx)
		if err != nil {
			t.SetError(err.Error())
			s.globalRepo.UpdateProjectWatchStatus(projectID, "error")
			// Broadcast build error
			s.eventBroker.BroadcastBuildError(t.ID, projectID, err.Error())
			return
		}

		// Mark change as processed
		_, err = s.db.Exec(`
			UPDATE change_history SET processed = 1
			WHERE project_id = ? AND file_path = ? AND processed = 0
		`, projectID, filePath)
		if err != nil {
			fmt.Printf("Failed to mark change as processed: %v\n", err)
		}

		t.SetResult(map[string]interface{}{
			"files_parsed":  result.FilesParsed,
			"nodes_created": result.NodesCreated,
			"edges_created": result.EdgesCreated,
			"file_path":     filePath,
			"change_type":   changeType,
		})

		// Broadcast build complete
		s.eventBroker.BroadcastBuildComplete(t.ID, projectID, result.FilesParsed, result.NodesCreated, result.EdgesCreated, result.ChangedFiles, result.RemovedFiles, result.ChangedFileOldNodeIDs, result.ChangedFileOldEdgeIDs)
	}()
}

// getQueuedChangesCount returns the count of unprocessed changes for a project.
func (s *Server) getQueuedChangesCount(projectID string) int {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM change_history
		WHERE project_id = ? AND processed = 0
	`, projectID).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}


// generateProjectID generates a unique project ID using timestamp and random suffix.
func generateProjectID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
