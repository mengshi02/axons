package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIEmbedder implements Embedder using OpenAI-compatible API.
// Supports OpenAI, Ollama, Azure OpenAI, and other compatible services.
type OpenAIEmbedder struct {
	// Configuration
	apiKey     string
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client

	// Rate limiting
	rateLimit time.Duration
	lastCall  time.Time
	batchSize int
}

// OpenAIConfig contains configuration for OpenAI-compatible embedder.
type OpenAIConfig struct {
	// APIKey is the API key (optional for local Ollama).
	APIKey string `json:"api_key"`

	// BaseURL is the API endpoint.
	// OpenAI: "https://api.openai.com/v1"
	// Ollama: "http://localhost:11434/v1"
	// Azure: "https://YOUR_RESOURCE.openai.azure.com/openai/deployments/YOUR_DEPLOYMENT"
	BaseURL string `json:"base_url"`

	// Model is the embedding model name.
	// OpenAI: "text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"
	// Ollama: "nomic-embed-text", "mxbai-embed-large", "all-minilm"
	Model string `json:"model"`

	// Dimension is the embedding dimension (0 for model default).
	Dimension int `json:"dimension"`

	// Timeout is the HTTP request timeout.
	Timeout time.Duration `json:"timeout"`

	// BatchSize is the number of texts per API call.
	BatchSize int `json:"batch_size"`

	// RateLimit is the minimum time between API calls.
	RateLimit time.Duration `json:"rate_limit"`
}

// openAIEmbeddingRequest represents the API request.
type openAIEmbeddingRequest struct {
	Input interface{} `json:"input"`
	Model string      `json:"model"`
}

// openAIEmbeddingResponse represents the OpenAI API response format.
// {"object":"list","data":[{"index":N,"embedding":[...]}]}
type openAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Code    interface{} `json:"code"`
	} `json:"error"`
}

// ollamaEmbeddingItem represents a single item in the Ollama API response.
// Ollama returns a top-level array: [{"index":N,"embedding":[[...]]}]
// where embedding is a 2D array ([][]float32), outer dim is batch index.
type ollamaEmbeddingItem struct {
	Index     int         `json:"index"`
	Embedding [][]float32 `json:"embedding"`
}

// Default configurations for common providers.
var (
	// DefaultOpenAIConfig is the default OpenAI configuration.
	DefaultOpenAIConfig = OpenAIConfig{
		BaseURL:   "https://api.openai.com/v1",
		Model:     "text-embedding-3-small",
		Dimension: 1536,
		Timeout:   30 * time.Second,
		BatchSize: 100,
		RateLimit: 0,
	}

	// DefaultOllamaConfig is the default Ollama configuration.
	DefaultOllamaConfig = OpenAIConfig{
		BaseURL:   "http://localhost:11434/v1",
		Model:     "nomic-embed-text",
		Dimension: 768,
		Timeout:   60 * time.Second,
		BatchSize: 50,
		RateLimit: 0,
	}
)

// Model dimensions for common models.
var modelDimensions = map[string]int{
	// OpenAI models
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,

	// Ollama models
	"nomic-embed-text":       768,
	"mxbai-embed-large":      1024,
	"all-minilm":             384,
	"snowflake-arctic-embed": 1024,

	// Cohere models
	"embed-english-v3.0":      1024,
	"embed-multilingual-v3.0": 1024,
}

// NewOpenAIEmbedder creates a new OpenAI-compatible embedder.
func NewOpenAIEmbedder(config OpenAIConfig) *OpenAIEmbedder {
	// Set defaults
	if config.BaseURL == "" {
		config.BaseURL = DefaultOpenAIConfig.BaseURL
	}
	if config.Model == "" {
		config.Model = DefaultOpenAIConfig.Model
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultOpenAIConfig.Timeout
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultOpenAIConfig.BatchSize
	}

	// Determine dimension from model if not specified
	if config.Dimension == 0 {
		if dim, ok := modelDimensions[config.Model]; ok {
			config.Dimension = dim
		} else {
			config.Dimension = 1536 // Default fallback
		}
	}

	return &OpenAIEmbedder{
		apiKey:     config.APIKey,
		baseURL:    config.BaseURL,
		model:      config.Model,
		dimension:  config.Dimension,
		httpClient: &http.Client{Timeout: config.Timeout},
		rateLimit:  config.RateLimit,
		batchSize:  config.BatchSize,
	}
}

// NewOllamaEmbedder creates a new Ollama embedder.
func NewOllamaEmbedder(baseURL, model string) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	if model == "" {
		model = "nomic-embed-text"
	}

	config := OpenAIConfig{
		BaseURL:   baseURL,
		Model:     model,
		Timeout:   60 * time.Second,
		BatchSize: 50,
	}

	// Get dimension from model
	if dim, ok := modelDimensions[model]; ok {
		config.Dimension = dim
	}

	return NewOpenAIEmbedder(config)
}

// Embed generates embeddings for the given texts.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Process in batches
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += e.batchSize {
		end := i + e.batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)

		// Rate limiting
		if e.rateLimit > 0 && i+e.batchSize < len(texts) {
			time.Sleep(e.rateLimit)
		}
	}

	return allEmbeddings, nil
}

// EmbeddingDimension returns the dimension of the embeddings.
func (e *OpenAIEmbedder) EmbeddingDimension() int {
	return e.dimension
}

// ModelName returns the name of the embedding model.
func (e *OpenAIEmbedder) ModelName() string {
	return e.model
}

// embedBatch sends a single batch of texts to the API.
func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// Build request
	reqBody := openAIEmbeddingRequest{
		Input: texts,
		Model: e.model,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	url := e.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set authorization header
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	// Send request
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Check for empty response body
	if len(respBody) == 0 {
		return nil, fmt.Errorf("empty response from server (HTTP %d), check URL and service availability", resp.StatusCode)
	}

	// Check for non-OK HTTP status before parsing
	if resp.StatusCode != http.StatusOK {
		// Try to extract error message from response body
		var errResp openAIEmbeddingResponse
		if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Error != nil {
			return nil, fmt.Errorf("API error (HTTP %d): %s (type: %s, code: %v)",
				resp.StatusCode, errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		}
		return nil, fmt.Errorf("unexpected HTTP %d from %s (body: %s)", resp.StatusCode, e.baseURL+"/embeddings", string(respBody))
	}

	// Parse response: support both OpenAI format ({"data":[...]}) and Ollama format ([{...}])
	var embeddings [][]float32

	// Try Ollama format first: top-level JSON array
	if len(respBody) > 0 && respBody[0] == '[' {
		var ollamaItems []ollamaEmbeddingItem
		if err := json.Unmarshal(respBody, &ollamaItems); err != nil {
			return nil, fmt.Errorf("parse response (ollama format): %w (body: %s)", err, string(respBody))
		}
		if len(ollamaItems) != len(texts) {
			return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(ollamaItems))
		}
		embeddings = make([][]float32, len(texts))
		for _, item := range ollamaItems {
			if item.Index < 0 || item.Index >= len(texts) {
				return nil, fmt.Errorf("invalid index %d in response", item.Index)
			}
			if len(item.Embedding) == 0 {
				return nil, fmt.Errorf("empty embedding at index %d", item.Index)
			}
			// Ollama wraps the vector in an outer array: [[v1, v2, ...]]
			// Take the first (and only) inner slice as the actual vector.
			embeddings[item.Index] = item.Embedding[0]
		}
	} else {
		// OpenAI format: {"object":"list","data":[{"index":N,"embedding":[...]}]}
		var embeddingResp openAIEmbeddingResponse
		if err := json.Unmarshal(respBody, &embeddingResp); err != nil {
			return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
		}
		if embeddingResp.Error != nil {
			return nil, fmt.Errorf("API error: %s (type: %s, code: %v)",
				embeddingResp.Error.Message,
				embeddingResp.Error.Type,
				embeddingResp.Error.Code,
			)
		}
		if len(embeddingResp.Data) != len(texts) {
			return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embeddingResp.Data))
		}
		embeddings = make([][]float32, len(texts))
		for _, data := range embeddingResp.Data {
			if data.Index < 0 || data.Index >= len(texts) {
				return nil, fmt.Errorf("invalid index %d in response", data.Index)
			}
			embeddings[data.Index] = data.Embedding
		}
	}

	// Update dimension from response if needed
	if e.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
		e.dimension = len(embeddings[0])
	}

	return embeddings, nil
}

// SetAPIKey sets the API key.
func (e *OpenAIEmbedder) SetAPIKey(apiKey string) {
	e.apiKey = apiKey
}

// SetBaseURL sets the base URL.
func (e *OpenAIEmbedder) SetBaseURL(baseURL string) {
	e.baseURL = baseURL
}

// SetModel sets the model.
func (e *OpenAIEmbedder) SetModel(model string) {
	e.model = model
	if dim, ok := modelDimensions[model]; ok {
		e.dimension = dim
	}
}
