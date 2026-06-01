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

func setupWatchTestServer(t *testing.T) *Server {
	// Create temp database
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

func TestHandleWatchStart(t *testing.T) {
	server := setupWatchTestServer(t)
	defer server.dbMgr.Close()

	// Use unique temp directories for each test
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	tests := []struct {
		name       string
		body       WatchStartRequest
		wantStatus int
	}{
		{
			name:       "start watch temp directory 1",
			body:       WatchStartRequest{RootDir: tmpDir1},
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "start watch temp directory 2",
			body:       WatchStartRequest{RootDir: tmpDir2},
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "start watch non-existent directory",
			body:       WatchStartRequest{RootDir: "/nonexistent/path"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/watch/start", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			
			rr := httptest.NewRecorder()
			
			server.handleWatchStart(rr, req, httprouter.Params{})
			
			if rr.Code != tt.wantStatus {
				t.Errorf("handleWatchStart() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandleWatchStop(t *testing.T) {
	server := setupWatchTestServer(t)
	defer server.dbMgr.Close()

	// Use unique temp directory
	tmpDir := t.TempDir()

	// First start a watcher
	startBody := WatchStartRequest{RootDir: tmpDir}
	startBytes, _ := json.Marshal(startBody)
	startReq := httptest.NewRequest("POST", "/v1/watch/start", bytes.NewReader(startBytes))
	startReq.Header.Set("Content-Type", "application/json")
	startRR := httptest.NewRecorder()
	server.handleWatchStart(startRR, startReq, httprouter.Params{})

	tests := []struct {
		name       string
		body       WatchStopRequest
		wantStatus int
	}{
		{
			name:       "stop watch",
			body:       WatchStopRequest{RootDir: tmpDir},
			wantStatus: http.StatusOK,
		},
		{
			name:       "stop non-existent watch",
			body:       WatchStopRequest{RootDir: "/nonexistent"},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/v1/watch/stop", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			
			rr := httptest.NewRecorder()
			
			server.handleWatchStop(rr, req, httprouter.Params{})
			
			if rr.Code != tt.wantStatus {
				t.Errorf("handleWatchStop() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandleWatchStatus(t *testing.T) {
	server := setupWatchTestServer(t)
	defer server.dbMgr.Close()

	tests := []struct {
		name       string
		rootDir    string
		wantStatus int
	}{
		{
			name:       "status without root dir",
			rootDir:    "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "status with non-existent root dir",
			rootDir:    "/nonexistent",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/v1/watch/status"
			if tt.rootDir != "" {
				url += "?root_dir=" + tt.rootDir
			}
			req := httptest.NewRequest("GET", url, nil)
			
			rr := httptest.NewRecorder()
			
			server.handleWatchStatus(rr, req, httprouter.Params{})
			
			if rr.Code != tt.wantStatus {
				t.Errorf("handleWatchStatus() status = %v, want %v, body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandleWatchList(t *testing.T) {
	server := setupWatchTestServer(t)
	defer server.dbMgr.Close()

	req := httptest.NewRequest("GET", "/v1/watch/list", nil)
	rr := httptest.NewRecorder()

	server.handleWatchList(rr, req, httprouter.Params{})

	if rr.Code != http.StatusOK {
		t.Errorf("handleWatchList() status = %v, want %v", rr.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}

	if _, ok := resp["count"]; !ok {
		t.Error("Response missing 'count' field")
	}
}

func TestWatchStartAlreadyWatching(t *testing.T) {
	server := setupWatchTestServer(t)
	defer server.dbMgr.Close()

	// Use unique temp directory
	tmpDir := t.TempDir()

	// Start first watcher
	startBody := WatchStartRequest{RootDir: tmpDir}
	startBytes, _ := json.Marshal(startBody)
	req1 := httptest.NewRequest("POST", "/v1/watch/start", bytes.NewReader(startBytes))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	server.handleWatchStart(rr1, req1, httprouter.Params{})

	if rr1.Code != http.StatusAccepted {
		t.Fatalf("First start failed: %v", rr1.Code)
	}

	// Try to start again - should return already_watching
	req2 := httptest.NewRequest("POST", "/v1/watch/start", bytes.NewReader(startBytes))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	server.handleWatchStart(rr2, req2, httprouter.Params{})

	if rr2.Code != http.StatusOK {
		t.Errorf("Second start status = %v, want %v", rr2.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.Unmarshal(rr2.Body.Bytes(), &resp)
	if resp["status"] != "already_watching" {
		t.Errorf("Expected status 'already_watching', got %v", resp["status"])
	}
}