// Package client provides a client for communicating with the axons daemon.
package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWatchStart(t *testing.T) {
	tests := []struct {
		name       string
		rootDir    string
		response   WatchStartResponse
		wantErr    bool
	}{
		{
			name:    "start success",
			rootDir: "/test/path",
			response: WatchStartResponse{
				Status:  "started",
				RootDir: "/test/path",
			},
			wantErr: false,
		},
		{
			name:    "already watching",
			rootDir: "/test/path",
			response: WatchStartResponse{
				Status:  "already_watching",
				RootDir: "/test/path",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}
				if r.URL.Path != "/v1/watch/start" {
					t.Errorf("Expected /v1/watch/start path, got %s", r.URL.Path)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			// Create client with test server URL
			c := &Client{
				httpClient: server.Client(),
				baseURL:    server.URL,
			}

			resp, err := c.WatchStart(tt.rootDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("WatchStart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && resp.Status != tt.response.Status {
				t.Errorf("WatchStart() status = %v, want %v", resp.Status, tt.response.Status)
			}
		})
	}
}

func TestWatchStop(t *testing.T) {
	tests := []struct {
		name       string
		rootDir    string
		response   WatchStopResponse
		wantErr    bool
	}{
		{
			name:    "stop success",
			rootDir: "/test/path",
			response: WatchStopResponse{
				Status:  "stopped",
				RootDir: "/test/path",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}
				if r.URL.Path != "/v1/watch/stop" {
					t.Errorf("Expected /v1/watch/stop path, got %s", r.URL.Path)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			c := &Client{
				httpClient: server.Client(),
				baseURL:    server.URL,
			}

			resp, err := c.WatchStop(tt.rootDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("WatchStop() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && resp.Status != tt.response.Status {
				t.Errorf("WatchStop() status = %v, want %v", resp.Status, tt.response.Status)
			}
		})
	}
}

func TestWatchStatus(t *testing.T) {
	tests := []struct {
		name       string
		rootDir    string
		response   WatchStatusResponse
		wantErr    bool
	}{
		{
			name:    "status running",
			rootDir: "/test/path",
			response: WatchStatusResponse{
				Status:  "running",
				RootDir: "/test/path",
			},
			wantErr: false,
		},
		{
			name:    "status not watching",
			rootDir: "/test/path",
			response: WatchStatusResponse{
				Status:  "not_watching",
				RootDir: "/test/path",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("Expected GET request, got %s", r.Method)
				}
				if r.URL.Path != "/v1/watch/status" {
					t.Errorf("Expected /v1/watch/status path, got %s", r.URL.Path)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			c := &Client{
				httpClient: server.Client(),
				baseURL:    server.URL,
			}

			resp, err := c.WatchStatus(tt.rootDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("WatchStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && resp.Status != tt.response.Status {
				t.Errorf("WatchStatus() status = %v, want %v", resp.Status, tt.response.Status)
			}
		})
	}
}

func TestWatchList(t *testing.T) {
	response := WatchListResponse{
		Count: 2,
		Watchers: []WatchInfo{
			{RootDir: "/path1", Status: "running"},
			{RootDir: "/path2", Status: "running"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/watch/list" {
			t.Errorf("Expected /v1/watch/list path, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}

	resp, err := c.WatchList()
	if err != nil {
		t.Errorf("WatchList() error = %v", err)
		return
	}
	if resp.Count != 2 {
		t.Errorf("WatchList() count = %v, want 2", resp.Count)
	}
	if len(resp.Watchers) != 2 {
		t.Errorf("WatchList() watchers count = %v, want 2", len(resp.Watchers))
	}
}