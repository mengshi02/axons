// Package client provides a client for communicating with the axons daemon.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mengshi02/axons/internal/task"
)

// Client is a client for communicating with the axons daemon.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// New creates a new client.
func New(socketPath string) *Client {
	return &Client{
		baseURL: "http://unix",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

// doRequest performs an HTTP request and returns the response body.
func (c *Client) doRequest(method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error   string `json:"error"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
		}
		return nil, fmt.Errorf("%s: %s", errResp.Code, errResp.Message)
	}

	return respBody, nil
}

// Health checks the daemon health.
func (c *Client) Health() error {
	_, err := c.doRequest("GET", "/api/v1/health", nil)
	return err
}

// StatusResponse represents the daemon status response.
type StatusResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Uptime    string            `json:"uptime"`
	TaskCount int               `json:"task_count"`
	Tasks     []task.TaskStatus `json:"tasks,omitempty"`
}

// Status gets the daemon status.
func (c *Client) Status() (*StatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/status", nil)
	if err != nil {
		return nil, err
	}

	var resp StatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// TaskStatus gets the status of a specific task.
func (c *Client) TaskStatus(taskID string) (*task.TaskStatus, error) {
	body, err := c.doRequest("GET", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}

	var resp task.TaskStatus
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// CancelTask cancels a task.
func (c *Client) CancelTask(taskID string) error {
	_, err := c.doRequest("DELETE", "/api/v1/tasks/"+taskID, nil)
	return err
}

// ListTasks lists all tasks.
func (c *Client) ListTasks() ([]task.TaskStatus, error) {
	body, err := c.doRequest("GET", "/api/v1/tasks", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tasks []task.TaskStatus `json:"tasks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp.Tasks, nil
}

// ExecuteCodeGraphRequest represents a code graph execution request.
type ExecuteCodeGraphRequest struct {
	RepoPath string `json:"repo_path"`
}

// ExecuteCodeGraphResponse represents a code graph execution response.
type ExecuteCodeGraphResponse struct {
	TaskID string `json:"task_id"`
}

// ExecuteCodeGraph starts a code graph building task.
func (c *Client) ExecuteCodeGraph(repoPath string) (*ExecuteCodeGraphResponse, error) {
	req := ExecuteCodeGraphRequest{RepoPath: repoPath}
	body, err := c.doRequest("POST", "/api/v1/codegraph/execute", req)
	if err != nil {
		return nil, err
	}

	var resp ExecuteCodeGraphResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SearchRequest represents a search request.
type SearchRequest struct {
	Query       string  `json:"query"`
	Mode        string  `json:"mode,omitempty"`        // hybrid, semantic, keyword
	Limit       int     `json:"limit,omitempty"`
	MinScore    float32 `json:"min_score,omitempty"`   // Minimum similarity score (0-1)
	Kind        string  `json:"kind,omitempty"`        // Filter by symbol kind: function, method, class
	File        string  `json:"file,omitempty"`        // Filter by file path pattern
	NoTests     bool    `json:"no_tests,omitempty"`    // Exclude test files
	Repository  string  `json:"repository,omitempty"`
	ContentType string  `json:"content_type,omitempty"`
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
	RRFScore      float64 `json:"rrf_score,omitempty"`   // RRF fusion score for hybrid search
	BM25Score     float64 `json:"bm25_score,omitempty"`  // BM25 score for keyword/hybrid search
	Repository    string  `json:"repository,omitempty"`
	FilePath      string  `json:"file_path,omitempty"`
	ContentType   string  `json:"content_type,omitempty"`
	Content       string  `json:"content,omitempty"`
}

// SearchResponse represents a search response.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
	Message string         `json:"message,omitempty"` // Optional message (e.g., fallback info)
}

// Search performs a search with the given request.
func (c *Client) Search(query string, limit int) (*SearchResponse, error) {
	req := SearchRequest{Query: query, Limit: limit}
	body, err := c.doRequest("POST", "/api/v1/search", req)
	if err != nil {
		return nil, err
	}

	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SearchWithContext performs a search with context and advanced options.
func (c *Client) SearchWithContext(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/search", req)
	if err != nil {
		return nil, err
	}

	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// BuildRequest represents a build request.
type BuildRequest struct {
	RootDir         string   `json:"root_dir"`
	FullBuild       bool     `json:"full_build"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	IncludeDataflow bool     `json:"include_dataflow"`
	IncludeAST      bool     `json:"include_ast"`
}

// BuildResponse represents a build response.
type BuildResponse struct {
	TaskID      string `json:"task_id,omitempty"`
	FilesParsed int    `json:"files_parsed"`
	NodesCreated int   `json:"nodes_created"`
	EdgesCreated int   `json:"edges_created"`
	Duration    string `json:"duration"`
}

// Build starts a code graph build.
func (c *Client) Build(req *BuildRequest) (*BuildResponse, error) {
	body, err := c.doRequest("POST", "/v1/build", req)
	if err != nil {
		return nil, err
	}

	var resp BuildResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// QueryRequest represents a query request.
type QueryRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	File     string `json:"file,omitempty"`
	Callers  bool   `json:"callers"`
	Callees  bool   `json:"callees"`
	NoTests  bool   `json:"no_tests"`
	Limit    int    `json:"limit,omitempty"`
}

// NodeInfo represents a code node.
type NodeInfo struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	EndLine        int    `json:"end_line,omitempty"`
	QualifiedName  string `json:"qualified_name,omitempty"`
	Exported       bool   `json:"exported"`
	Visibility     string `json:"visibility,omitempty"`
}

// QueryResponse represents a query response.
type QueryResponse struct {
	Nodes   []NodeInfo `json:"nodes"`
	Callers []NodeInfo `json:"callers,omitempty"`
	Callees []NodeInfo `json:"callees,omitempty"`
}

// Query queries the code graph.
func (c *Client) Query(req *QueryRequest) (*QueryResponse, error) {
	body, err := c.doRequest("POST", "/v1/query", req)
	if err != nil {
		return nil, err
	}

	var resp QueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// EmbedRequest represents an embed request.
type EmbedRequest struct {
	RootDir   string `json:"root_dir"`
	Provider  string `json:"provider"`
	Model     string `json:"model,omitempty"`
	Strategy  string `json:"strategy"`
	BatchSize int    `json:"batch_size,omitempty"`
}

// EmbedResponse represents an embed response.
type EmbedResponse struct {
	TaskID     string `json:"task_id,omitempty"`
	Symbols    int    `json:"symbols"`
	Embeddings int    `json:"embeddings"`
	Dimension  int    `json:"dimension"`
	Duration   string `json:"duration"`
}

// Embed starts an embedding build.
func (c *Client) Embed(req *EmbedRequest) (*EmbedResponse, error) {
	body, err := c.doRequest("POST", "/v1/embed", req)
	if err != nil {
		return nil, err
	}

	var resp EmbedResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// StatsResponse represents a stats response.
type StatsResponse struct {
	TotalNodes   int            `json:"total_nodes"`
	TotalEdges   int            `json:"total_edges"`
	NodesByKind  map[string]int `json:"nodes_by_kind"`
	EdgesByType  map[string]int `json:"edges_by_type"`
	Files        int            `json:"files"`
	LastBuilt    string         `json:"last_built,omitempty"`
}

// Stats gets graph statistics.
func (c *Client) Stats() (*StatsResponse, error) {
	body, err := c.doRequest("GET", "/v1/stats", nil)
	if err != nil {
		return nil, err
	}

	var resp StatsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// WaitForTask waits for a task to complete.
func (c *Client) WaitForTask(taskID string, interval time.Duration, callback func(*task.TaskStatus)) (*task.TaskStatus, error) {
	if interval == 0 {
		interval = 500 * time.Millisecond
	}

	for {
		status, err := c.TaskStatus(taskID)
		if err != nil {
			return nil, err
		}

		if callback != nil {
			callback(status)
		}

		switch status.Status {
		case task.StatusComplete:
			return status, nil
		case task.StatusError:
			return status, fmt.Errorf("task failed: %s", status.Error)
		case task.StatusCanceled:
			return status, fmt.Errorf("task canceled")
		}

		time.Sleep(interval)
	}
}

// IsRunning checks if the daemon is running.
func (c *Client) IsRunning() bool {
	return c.Health() == nil
}

// AuditRequest represents an audit request.
type AuditRequest struct {
	MaxCycles     int `json:"max_cycles,omitempty"`
	MaxComplexity int `json:"max_complexity,omitempty"`
}

// AuditResponse represents an audit response.
type AuditResponse struct {
	Summary        AuditSummary    `json:"summary"`
	Cycles         []CycleInfo     `json:"cycles,omitempty"`
	DeadCode       []DeadCodeInfo  `json:"dead_code,omitempty"`
	HighComplexity []ComplexInfo   `json:"high_complexity,omitempty"`
	EntryPoints    []string        `json:"entry_points,omitempty"`
	Issues         int             `json:"issues"`
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

// Audit runs a code audit.
func (c *Client) Audit(req *AuditRequest) (*AuditResponse, error) {
	body, err := c.doRequest("POST", "/v1/audit", req)
	if err != nil {
		return nil, err
	}

	var resp AuditResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// CheckRequest represents a check request.
type CheckRequest struct {
	MaxComplexity  int  `json:"max_complexity,omitempty"`
	FailOnDeadCode bool `json:"fail_on_dead_code"`
	FailOnComplex  bool `json:"fail_on_complex"`
	NoNewCycles    bool `json:"no_new_cycles"`
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

// Check runs CI checks.
func (c *Client) Check(req *CheckRequest) (*CheckResponse, error) {
	body, err := c.doRequest("POST", "/v1/check", req)
	if err != nil {
		return nil, err
	}

	var resp CheckResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// ComplexityRequest represents a complexity request.
type ComplexityRequest struct {
	Threshold int    `json:"threshold,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	File      string `json:"file,omitempty"`
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

// Complexity analyzes code complexity.
func (c *Client) Complexity(req *ComplexityRequest) (*ComplexityResponse, error) {
	body, err := c.doRequest("POST", "/v1/complexity", req)
	if err != nil {
		return nil, err
	}

	var resp ComplexityResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// PathRequest represents a path finding request.
type PathRequest struct {
	From     string `json:"from"`
	To       string `json:"to"`
	MaxDepth int    `json:"max_depth,omitempty"`
	FindAll  bool   `json:"find_all"`
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

// Path finds call paths between two symbols.
func (c *Client) Path(req *PathRequest) (*PathResponse, error) {
	body, err := c.doRequest("POST", "/v1/path", req)
	if err != nil {
		return nil, err
	}

	var resp PathResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SequenceRequest represents a sequence diagram request.
type SequenceRequest struct {
	Name        string   `json:"name"`
	Depth       int      `json:"depth,omitempty"`
	FileFilters []string `json:"file_filters,omitempty"`
	KindFilter  string   `json:"kind_filter,omitempty"`
	NoTests     bool     `json:"no_tests"`
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

// Sequence generates a sequence diagram.
func (c *Client) Sequence(req *SequenceRequest) (*SequenceResponse, error) {
	body, err := c.doRequest("POST", "/v1/sequence", req)
	if err != nil {
		return nil, err
	}

	var resp SequenceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// ExportRequest represents an export request.
type ExportRequest struct {
	Format string `json:"format"`
	Filter string `json:"filter,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ExportResponse represents an export response.
type ExportResponse struct {
	Format string      `json:"format"`
	Nodes  []NodeInfo  `json:"nodes"`
	Edges  []EdgeInfo  `json:"edges"`
	Raw    string      `json:"raw,omitempty"`
}

// EdgeInfo represents an edge in export.
type EdgeInfo struct {
	SourceID int64  `json:"source_id"`
	TargetID int64  `json:"target_id"`
	Kind     string `json:"kind"`
}

// Export exports the code graph.
func (c *Client) Export(req *ExportRequest) (*ExportResponse, error) {
	body, err := c.doRequest("POST", "/v1/export", req)
	if err != nil {
		return nil, err
	}

	var resp ExportResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// Registry operations

// RegistryRepo represents a registered repository.
type RegistryRepo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	DBPath         string `json:"db_path"`
	AddedAt        string `json:"added_at"`
	LastAccessedAt string `json:"last_accessed_at"`
}

// ListRepos lists all registered repositories.
func (c *Client) ListRepos() ([]RegistryRepo, error) {
	body, err := c.doRequest("GET", "/v1/repos", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Repos []RegistryRepo `json:"repos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp.Repos, nil
}

// RegisterRepoRequest represents a register repo request.
type RegisterRepoRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// RegisterRepo registers a repository.
func (c *Client) RegisterRepo(req *RegisterRepoRequest) (*RegistryRepo, error) {
	body, err := c.doRequest("POST", "/v1/repos", req)
	if err != nil {
		return nil, err
	}

	var resp RegistryRepo
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// UnregisterRepo removes a repository from the registry.
func (c *Client) UnregisterRepo(name string) error {
	_, err := c.doRequest("DELETE", "/v1/repos/"+name, nil)
	return err
}

// ResolveRepo resolves a repo name to its database path.
func (c *Client) ResolveRepo(name string) (*RegistryRepo, error) {
	body, err := c.doRequest("GET", "/v1/repos/"+name, nil)
	if err != nil {
		return nil, err
	}

	var resp RegistryRepo
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// PruneReposRequest represents a prune repos request.
type PruneReposRequest struct {
	TTL     int      `json:"ttl"`
	Exclude []string `json:"exclude,omitempty"`
	DryRun  bool     `json:"dry_run"`
}

// PruneRepoResult represents a pruned repository.
type PruneRepoResult struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// PruneRepos prunes stale registry entries.
func (c *Client) PruneRepos(req *PruneReposRequest) ([]PruneRepoResult, error) {
	body, err := c.doRequest("POST", "/v1/repos/prune", req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Pruned []PruneRepoResult `json:"pruned"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp.Pruned, nil
}

// CFG operations

// CFGRequest represents a CFG request.
type CFGRequest struct {
	Name    string   `json:"name"`
	File    []string `json:"file,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	NoTests bool     `json:"no_tests"`
	Limit   int      `json:"limit,omitempty"`
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

// CFGResponse represents a CFG response.
type CFGResponse struct {
	Count   int         `json:"count"`
	Results []CFGResult `json:"results"`
}

// CFG retrieves control flow graph for a function.
func (c *Client) CFG(req *CFGRequest) (*CFGResponse, error) {
	body, err := c.doRequest("POST", "/v1/cfg", req)
	if err != nil {
		return nil, err
	}

	var resp CFGResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// CFGFormatted retrieves CFG in a specific format (dot, mermaid, json).
func (c *Client) CFGFormatted(req *CFGRequest, format string) (string, error) {
	body, err := c.doRequest("GET", "/v1/cfg/formatted?name="+req.Name+"&format="+format, nil)
	if err != nil {
		return "", err
	}

	return string(body), nil
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

// Dataflow analyzes data flow for a function.
func (c *Client) Dataflow(req *DataflowRequest) (*DataflowResponse, error) {
	body, err := c.doRequest("POST", "/v1/dataflow", req)
	if err != nil {
		return nil, err
	}

	var resp DataflowResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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

// DiffImpact analyzes impact of changes.
func (c *Client) DiffImpact(req *DiffImpactRequest) (*DiffImpactResponse, error) {
	body, err := c.doRequest("POST", "/v1/diff-impact", req)
	if err != nil {
		return nil, err
	}

	var resp DiffImpactResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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
	CodeownersFile string          `json:"codeowners_file,omitempty"`
	Files          []FileOwners    `json:"files,omitempty"`
	Symbols        []SymbolOwners  `json:"symbols,omitempty"`
	Boundaries     []BoundaryEdge  `json:"boundaries,omitempty"`
	Summary        OwnersSummary   `json:"summary"`
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
	TotalFiles       int            `json:"total_files"`
	TotalSymbols     int            `json:"total_symbols"`
	ByOwner          map[string]int `json:"by_owner"`
	BoundaryEdges    int            `json:"boundary_edges"`
	UnownedFiles     int            `json:"unowned_files"`
	UnownedSymbols   int            `json:"unowned_symbols"`
}

// Owners analyzes code ownership.
func (c *Client) Owners(req *OwnersRequest) (*OwnersResponse, error) {
	body, err := c.doRequest("POST", "/v1/owners", req)
	if err != nil {
		return nil, err
	}

	var resp OwnersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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

// Triage analyzes and prioritizes code review.
func (c *Client) Triage(req *TriageRequest) (*TriageResponse, error) {
	body, err := c.doRequest("POST", "/v1/triage", req)
	if err != nil {
		return nil, err
	}

	var resp TriageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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

// CoChange analyzes git history for files that change together.
func (c *Client) CoChange(req *CoChangeRequest) (*CoChangeResponse, error) {
	body, err := c.doRequest("POST", "/v1/cochange", req)
	if err != nil {
		return nil, err
	}

	var resp CoChangeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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

// SnapshotList lists all snapshots.
func (c *Client) SnapshotList() (*SnapshotListResponse, error) {
	body, err := c.doRequest("GET", "/v1/snapshot/list", nil)
	if err != nil {
		return nil, err
	}

	var resp SnapshotListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SnapshotSave saves a snapshot.
func (c *Client) SnapshotSave(name string) (*SnapshotResponse, error) {
	body, err := c.doRequest("POST", "/v1/snapshot/save", &SnapshotRequest{Name: name})
	if err != nil {
		return nil, err
	}

	var resp SnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SnapshotRestore restores a snapshot.
func (c *Client) SnapshotRestore(name string) (*SnapshotResponse, error) {
	body, err := c.doRequest("POST", "/v1/snapshot/restore", &SnapshotRequest{Name: name})
	if err != nil {
		return nil, err
	}

	var resp SnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SnapshotDelete deletes a snapshot.
func (c *Client) SnapshotDelete(name string) (*SnapshotResponse, error) {
	body, err := c.doRequest("POST", "/v1/snapshot/delete", &SnapshotRequest{Name: name})
	if err != nil {
		return nil, err
	}

	var resp SnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
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

// BranchCompare compares code structure between two branches.
func (c *Client) BranchCompare(req *BranchCompareRequest) (*BranchCompareResponse, error) {
	body, err := c.doRequest("POST", "/v1/branch-compare", req)
	if err != nil {
		return nil, err
	}

	var resp BranchCompareResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// WatchStartRequest represents a watch start request.
type WatchStartRequest struct {
	RootDir string `json:"root_dir"`
}

// WatchStartResponse represents a watch start response.
type WatchStartResponse struct {
	Status    string    `json:"status"`
	RootDir   string    `json:"root_dir"`
	StartTime time.Time `json:"start_time,omitempty"`
}

// WatchStopRequest represents a watch stop request.
type WatchStopRequest struct {
	RootDir string `json:"root_dir"`
}

// WatchStopResponse represents a watch stop response.
type WatchStopResponse struct {
	Status  string `json:"status"`
	RootDir string `json:"root_dir"`
}

// WatchStatusResponse represents a watch status response.
type WatchStatusResponse struct {
	Status    string    `json:"status"`
	RootDir   string    `json:"root_dir"`
	StartTime time.Time `json:"start_time,omitempty"`
}

// WatchListResponse represents a watch list response.
type WatchListResponse struct {
	Watchers []WatchInfo `json:"watchers"`
	Count    int         `json:"count"`
}

// WatchInfo represents info about a watcher.
type WatchInfo struct {
	RootDir   string    `json:"root_dir"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
}

// WatchStart starts watching a directory for file changes.
func (c *Client) WatchStart(rootDir string) (*WatchStartResponse, error) {
	req := WatchStartRequest{RootDir: rootDir}
	body, err := c.doRequest("POST", "/v1/watch/start", req)
	if err != nil {
		return nil, err
	}

	var resp WatchStartResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// WatchStop stops watching a directory.
func (c *Client) WatchStop(rootDir string) (*WatchStopResponse, error) {
	req := WatchStopRequest{RootDir: rootDir}
	body, err := c.doRequest("POST", "/v1/watch/stop", req)
	if err != nil {
		return nil, err
	}

	var resp WatchStopResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// WatchStatus gets the status of a watcher.
func (c *Client) WatchStatus(rootDir string) (*WatchStatusResponse, error) {
	path := "/v1/watch/status"
	if rootDir != "" {
		path += "?root_dir=" + rootDir
	}
	body, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp WatchStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// WatchList lists all active watchers.
func (c *Client) WatchList() (*WatchListResponse, error) {
	body, err := c.doRequest("GET", "/v1/watch/list", nil)
	if err != nil {
		return nil, err
	}

	var resp WatchListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}