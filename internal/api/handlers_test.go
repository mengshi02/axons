// Package api provides HTTP API handlers for axons daemon.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/db"
	"github.com/mengshi02/axons/internal/task"
)

func setupTestServer(t *testing.T) *Server {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	mgr, err := db.NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Run project migrations on main DB for testing
	// In production, each project has its own DB, but for tests we use main DB
	if err := db.Migrate(mgr.MainDB()); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path: dbPath,
		},
	}

	taskMgr := task.NewManager(0)
	return NewServer(cfg, taskMgr, mgr)
}

// TestHandleHealth tests the health endpoint
func TestHandleHealth(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %v, want %v", rr.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", resp["status"])
	}
}

// TestHandleStats tests the stats endpoint
func TestHandleStats(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/v1/stats", nil)
	rr := httptest.NewRecorder()

	server.handleStats(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleStats() status = %v, want %v", rr.Code, http.StatusOK)
	}
}

// TestHandleListFiles tests the list files endpoint
func TestHandleListFiles(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/v1/files", nil)
	rr := httptest.NewRecorder()

	server.handleListFiles(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleListFiles() status = %v, want %v", rr.Code, http.StatusOK)
	}
}

// TestHandleBuild tests the build endpoint
func TestHandleBuild(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		body       BuildRequest
		wantStatus int
	}{
		{
			name: "build with valid path",
			body: BuildRequest{
				RootDir: tmpDir,
			},
			wantStatus: http.StatusAccepted,
		},
		{
			name: "build with empty path defaults to current",
			body: BuildRequest{
				RootDir: ".",
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/build", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleBuild(rr, req, httprouter.Params{})

			if rr.Code != tt.wantStatus {
				t.Errorf("handleBuild() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

// TestHandleQuery tests the query endpoint
func TestHandleQuery(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	tests := []struct {
		name       string
		body       QueryRequest
		wantStatus int
	}{
		{
			name: "query with name",
			body: QueryRequest{
				Name:  "main",
				Limit: 10,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "query with callers",
			body: QueryRequest{
				Name:    "main",
				Callers: true,
				Limit:   10,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "query with callees",
			body: QueryRequest{
				Name:    "main",
				Callees: true,
				Limit:   10,
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/query", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleQuery(rr, req, httprouter.Params{})

			if rr.Code != tt.wantStatus {
				t.Errorf("handleQuery() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

// TestHandleSearch tests the search endpoint
func TestHandleSearch(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	tests := []struct {
		name       string
		body       SearchRequest
		wantStatus int
	}{
		{
			name: "search with query",
			body: SearchRequest{
				Query: "main",
				Limit: 10,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "search with default limit",
			body: SearchRequest{
				Query: "test",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/search", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleSearch(rr, req, httprouter.Params{})

			if rr.Code != tt.wantStatus {
				t.Errorf("handleSearch() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

// TestHandleAudit tests the audit endpoint
func TestHandleAudit(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	tests := []struct {
		name       string
		body       AuditRequest
		wantStatus int
	}{
		{
			name:       "audit with defaults",
			body:       AuditRequest{},
			wantStatus: http.StatusOK,
		},
		{
			name: "audit with custom thresholds",
			body: AuditRequest{
				MaxCycles:     5,
				MaxComplexity: 10,
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/audit", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleAudit(rr, req, httprouter.Params{})

			if rr.Code != tt.wantStatus {
				t.Errorf("handleAudit() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}

			var resp AuditResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Errorf("Failed to parse response: %v", err)
			}
		})
	}
}

// TestHandleCheck tests the check endpoint
func TestHandleCheck(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := CheckRequest{
		MaxComplexity:  15,
		FailOnDeadCode: false,
		FailOnComplex:  false,
		NoNewCycles:    false,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/check", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleCheck(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleCheck() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleComplexity tests the complexity endpoint
func TestHandleComplexity(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := ComplexityRequest{
		Threshold: 15,
		Limit:     20,
		File:      "",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/complexity", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleComplexity(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleComplexity() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandlePath tests the path endpoint
func TestHandlePath(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := PathRequest{
		From:     "main",
		To:       "helper",
		MaxDepth: 5,
		FindAll:  false,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/path", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handlePath(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handlePath() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleExport tests the export endpoint
func TestHandleExport(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	tests := []struct {
		name       string
		body       ExportRequest
		wantStatus int
	}{
		{
			name: "export as json",
			body: ExportRequest{
				Format: "json",
				Limit:  100,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "export as dot",
			body: ExportRequest{
				Format: "dot",
				Limit:  50,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "export as mermaid",
			body: ExportRequest{
				Format: "mermaid",
				Limit:  50,
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/export", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleExport(rr, req, httprouter.Params{})

			if rr.Code != tt.wantStatus {
				t.Errorf("handleExport() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

// TestHandleEmbed tests the embed endpoint
func TestHandleEmbed(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	// Configure embedding provider in settings
	repo := server.repo
	if err := repo.SetSetting("embedding_provider", "ollama"); err != nil {
		t.Fatalf("Failed to set embedding provider: %v", err)
	}
	if err := repo.SetSetting("embedding_model", "nomic-embed-text"); err != nil {
		t.Fatalf("Failed to set embedding model: %v", err)
	}
	if err := repo.SetSetting("embedding_base_url", "http://localhost:11434"); err != nil {
		t.Fatalf("Failed to set embedding base URL: %v", err)
	}

	tmpDir := t.TempDir()

	body := EmbedRequest{
		RootDir:   tmpDir,
		Provider:  "ollama",
		Strategy:  "structured",
		BatchSize: 50,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleEmbed(rr, req, httprouter.Params{})

	if rr.Code != http.StatusAccepted {
		t.Errorf("handleEmbed() status = %v, want %v, body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}
}

// TestHandleDataflow tests the dataflow endpoint
func TestHandleDataflow(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := DataflowRequest{
		Name:   "main",
		File:   "",
		Detail: false,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/dataflow", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleDataflow(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleDataflow() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleOwners tests the owners endpoint
func TestHandleOwners(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := OwnersRequest{
		Owner:    "",
		Files:    []string{},
		Kind:     "",
		Boundary: false,
		NoTests:  false,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/owners", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleOwners(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleOwners() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleTriage tests the triage endpoint
func TestHandleTriage(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := TriageRequest{
		Files:  []string{"main.go"},
		Base:   "main",
		Top:    10,
		SortBy: "score",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/triage", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleTriage(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleTriage() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleCoChange tests the cochange endpoint
func TestHandleCoChange(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := CoChangeRequest{
		File:       "main.go",
		Since:      "1 month ago",
		MinSupport: 2,
		MinJaccard: 0.1,
		Limit:      10,
		NoTests:    true,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/cochange", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleCoChange(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleCoChange() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleSequence tests the sequence endpoint
func TestHandleSequence(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := SequenceRequest{
		Name:        "main",
		Depth:       3,
		FileFilters: []string{},
		KindFilter:  "",
		NoTests:     true,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/sequence", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleSequence(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleSequence() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleCFG tests the CFG endpoint
func TestHandleCFG(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := CFGRequest{
		Name:    "main",
		File:    []string{},
		Kind:    "",
		NoTests: true,
		Limit:   10,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/cfg", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleCFG(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleCFG() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleSemanticSearch tests the semantic search endpoint
func TestHandleSemanticSearch(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	body := SemanticSearchRequest{
		Query: "function that processes data",
		Limit: 10,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/semantic-search", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleSemanticSearch(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleSemanticSearch() status = %v, want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// TestHandleListTasks tests the list tasks endpoint
func TestHandleListTasks(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	rr := httptest.NewRecorder()

	server.handleListTasks(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleListTasks() status = %v, want %v", rr.Code, http.StatusOK)
	}
}

// TestHandleStatus tests the status endpoint
func TestHandleStatus(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleStatus() status = %v, want %v", rr.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}
	if resp["status"] != "running" {
		t.Errorf("Expected status 'running', got %v", resp["status"])
	}
}

// TestHandleListRepos tests the list repos endpoint
func TestHandleListRepos(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/v1/repos", nil)
	rr := httptest.NewRecorder()

	server.handleListRepos(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleListRepos() status = %v, want %v", rr.Code, http.StatusOK)
	}
}

// TestInvalidJSON tests handling of invalid JSON
func TestInvalidJSON(t *testing.T) {
	server := setupTestServer(t)
	defer server.dbMgr.Close()

	endpoints := []struct {
		name   string
		path   string
		method string
	}{
		{"build", "/v1/build", "POST"},
		{"query", "/v1/query", "POST"},
		{"search", "/v1/search", "POST"},
		{"audit", "/v1/audit", "POST"},
		{"check", "/v1/check", "POST"},
		{"complexity", "/v1/complexity", "POST"},
		{"path", "/v1/path", "POST"},
		{"export", "/v1/export", "POST"},
		{"embed", "/v1/embed", "POST"},
		{"dataflow", "/v1/dataflow", "POST"},
		{"owners", "/v1/owners", "POST"},
		{"triage", "/v1/triage", "POST"},
		{"cochange", "/v1/cochange", "POST"},
		{"sequence", "/v1/sequence", "POST"},
		{"cfg", "/v1/cfg", "POST"},
		{"semantic-search", "/v1/semantic-search", "POST"},
		{"watch/start", "/v1/watch/start", "POST"},
		{"watch/stop", "/v1/watch/stop", "POST"},
	}

	for _, ep := range endpoints {
		t.Run("invalid_json_"+ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, bytes.NewReader([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()

			// Route to appropriate handler based on path
			switch ep.path {
			case "/v1/build":
				server.handleBuild(rr, req, httprouter.Params{})
			case "/v1/query":
				server.handleQuery(rr, req, httprouter.Params{})
			case "/v1/search":
				server.handleSearch(rr, req, httprouter.Params{})
			case "/v1/audit":
				server.handleAudit(rr, req, httprouter.Params{})
			case "/v1/check":
				server.handleCheck(rr, req, httprouter.Params{})
			case "/v1/complexity":
				server.handleComplexity(rr, req, httprouter.Params{})
			case "/v1/path":
				server.handlePath(rr, req, httprouter.Params{})
			case "/v1/export":
				server.handleExport(rr, req, httprouter.Params{})
			case "/v1/embed":
				server.handleEmbed(rr, req, httprouter.Params{})
			case "/v1/dataflow":
				server.handleDataflow(rr, req, httprouter.Params{})
			case "/v1/owners":
				server.handleOwners(rr, req, httprouter.Params{})
			case "/v1/triage":
				server.handleTriage(rr, req, httprouter.Params{})
			case "/v1/cochange":
				server.handleCoChange(rr, req, httprouter.Params{})
			case "/v1/sequence":
				server.handleSequence(rr, req, httprouter.Params{})
			case "/v1/cfg":
				server.handleCFG(rr, req, httprouter.Params{})
			case "/v1/semantic-search":
				server.handleSemanticSearch(rr, req, httprouter.Params{})
			case "/v1/watch/start":
				server.handleWatchStart(rr, req, httprouter.Params{})
			case "/v1/watch/stop":
				server.handleWatchStop(rr, req, httprouter.Params{})
			}

			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s: expected status %v for invalid JSON, got %v", ep.name, http.StatusBadRequest, rr.Code)
			}
		})
	}
}
