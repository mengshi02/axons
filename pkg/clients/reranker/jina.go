package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// JinaReranker implements Reranker using Jina Rerank API.
type JinaReranker struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// JinaConfig contains configuration for Jina reranker.
type JinaConfig struct {
	APIKey  string        `json:"api_key"`
	BaseURL string        `json:"base_url"`
	Model   string        `json:"model"`
	Timeout time.Duration `json:"timeout"`
	TopN    int           `json:"top_n"`
}

// jinaRerankRequest represents the API request.
type jinaRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

// jinaRerankResponse represents the API response.
type jinaRerankResponse struct {
	Model string `json:"model"`
	Usage struct {
		TotalTokens  int `json:"total_tokens"`
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float32 `json:"relevance_score"`
		Document       struct {
			Text string `json:"text"`
		} `json:"document"`
	} `json:"results"`
	Detail string `json:"detail,omitempty"`
}

// DefaultJinaConfig is the default Jina configuration.
var DefaultJinaConfig = JinaConfig{
	BaseURL: "https://api.jina.ai/v1",
	Model:   "jina-reranker-v2-base-multilingual",
	Timeout: 30 * time.Second,
	TopN:    20,
}

// NewJinaReranker creates a new Jina reranker.
func NewJinaReranker(config JinaConfig) *JinaReranker {
	if config.BaseURL == "" {
		config.BaseURL = DefaultJinaConfig.BaseURL
	}
	if config.Model == "" {
		config.Model = DefaultJinaConfig.Model
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultJinaConfig.Timeout
	}

	return &JinaReranker{
		apiKey:     config.APIKey,
		baseURL:    config.BaseURL,
		model:      config.Model,
		httpClient: &http.Client{Timeout: config.Timeout},
	}
}

// Rerank reranks documents against a query.
func (r *JinaReranker) Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	// Build request
	reqBody := jinaRerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	url := r.baseURL + "/rerank"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	// Send request
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Parse response
	var rerankResp jinaRerankResponse
	if err := json.Unmarshal(respBody, &rerankResp); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}

	// Check for error
	if rerankResp.Detail != "" {
		return nil, fmt.Errorf("API error: %s", rerankResp.Detail)
	}

	// Convert results
	results := make([]RerankResult, len(rerankResp.Results))
	for i, res := range rerankResp.Results {
		results[i] = RerankResult{
			Index:    res.Index,
			Score:    res.RelevanceScore,
			Document: res.Document.Text,
		}
	}

	return results, nil
}

// RerankWithIDs reranks documents with their IDs.
func (r *JinaReranker) RerankWithIDs(ctx context.Context, query string, docs []RerankDocument) ([]RerankResult, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// Extract documents
	documents := make([]string, len(docs))
	for i, d := range docs {
		documents[i] = d.Text
	}

	// Rerank
	results, err := r.Rerank(ctx, query, documents)
	if err != nil {
		return nil, err
	}

	// Add IDs to results
	for i := range results {
		idx := results[i].Index
		if idx >= 0 && idx < len(docs) {
			results[i].ID = docs[idx].ID
		}
	}

	return results, nil
}

// SetAPIKey sets the API key.
func (r *JinaReranker) SetAPIKey(apiKey string) {
	r.apiKey = apiKey
}

// SetModel sets the model.
func (r *JinaReranker) SetModel(model string) {
	r.model = model
}