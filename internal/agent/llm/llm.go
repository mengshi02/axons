// Package llm provides LLM client implementations for the agent.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mengshi02/axons/internal/logger"
)

// Client is the LLM client interface
type Client interface {
	// Call invokes LLM (with tool support)
	Call(ctx context.Context, req *Request) (*Response, error)

	// SupportsTools returns whether tool calling is supported
	SupportsTools() bool
}

// Request represents LLM request
type Request struct {
	Messages  []Message
	Tools     []ToolDefinition
	MaxTokens int
}

// Message represents LLM message
type Message struct {
	Role      string         // system, user, assistant, tool
	Content   string         // Content
	Name      string         // Tool name (when role=tool)
	ToolCalls []ToolCall     // Tool calls (when role=assistant)
	ToolID    string         // Tool call ID (when role=tool)
	Images    []string       // base64 dataUrl list (multimodal user message)
}

// ToolCall represents a tool call
type ToolCall struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"` // function
	Function  FunctionCall   `json:"function"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition represents tool definition (schema for LLM)
type ToolDefinition struct {
	Type     string           `json:"type"` // function
	Function FunctionDefSpec  `json:"function"`
}

// FunctionDefSpec represents function definition specification
type FunctionDefSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Response represents LLM response
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls"`
	Usage     Usage      `json:"usage"`
}

// Usage represents token usage statistics
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ParseToolCallArguments parses tool call arguments
func ParseToolCallArguments(tc ToolCall) (map[string]any, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool call arguments: %w", err)
	}
	return args, nil
}

// ErrorType represents the type of API error
type ErrorType string

const (
	// ErrorTypeRateLimit indicates a rate limit error (429)
	ErrorTypeRateLimit ErrorType = "rate_limit"
	// ErrorTypeAuth indicates an authentication/authorization error (401/403)
	ErrorTypeAuth ErrorType = "auth_error"
	// ErrorTypeServer indicates a server-side error (5xx)
	ErrorTypeServer ErrorType = "server_error"
	// ErrorTypeUnknown indicates an unknown error
	ErrorTypeUnknown ErrorType = "unknown"
)

// APIError represents a structured API error with type information
type APIError struct {
	StatusCode int
	Type       ErrorType
	Message    string
	Body       string
	RetryAfter int // seconds to wait before retrying (from Retry-After header or response body)
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// IsRetryable returns whether the error is transient and can be retried
func (e *APIError) IsRetryable() bool {
	return e.Type == ErrorTypeRateLimit || e.Type == ErrorTypeServer
}

// classifyError determines the error type from HTTP status code and response body
func classifyError(statusCode int, body string) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Body:       body,
	}

	switch {
	case statusCode == 429:
		apiErr.Type = ErrorTypeRateLimit
		apiErr.Message = "请求频率超限，请稍后重试"
		// Try to extract retry-after hint from common response formats
		var parsed struct {
			Error struct {
				Code    interface{} `json:"code"`
				Message string      `json:"message"`
				Status  string      `json:"status"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &parsed) == nil {
			if parsed.Error.Message != "" {
				apiErr.Message = parsed.Error.Message
			}
			// Some APIs return numeric code like 429
			if code, ok := parsed.Error.Code.(float64); ok && code == 429 {
				apiErr.RetryAfter = 10 // default 10 seconds for rate limit
			}
			if parsed.Error.Status == "RATE_LIMIT" {
				apiErr.RetryAfter = 10
			}
		}
	case statusCode == 401 || statusCode == 403:
		apiErr.Type = ErrorTypeAuth
		apiErr.Message = "认证失败，请检查 API Key"
	case statusCode >= 500:
		apiErr.Type = ErrorTypeServer
		apiErr.Message = "服务器内部错误，请稍后重试"
	default:
		apiErr.Type = ErrorTypeUnknown
		apiErr.Message = fmt.Sprintf("API 错误 (状态码 %d)", statusCode)
	}

	return apiErr
}

// ParseAPIError tries to extract an APIError from an error value.
// Returns nil if the error is not an APIError.
func ParseAPIError(err error) *APIError {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr
	}
	return nil
}

// httpClient is the common HTTP client
type httpClient struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

func newHTTPClient(apiKey, baseURL string) *httpClient {
	return &httpClient{
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

func (c *httpClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	logger.S().Debugw("[LLM] Preparing HTTP request", "method", method, "path", path)
	
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			logger.S().Errorw("[LLM] Failed to marshal request body", "error", err)
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
		logger.S().Debugw("[LLM] Request body marshaled", "size", len(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		logger.S().Errorw("[LLM] Failed to create HTTP request", "error", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	logger.S().Debugw("[LLM] Sending HTTP request", "url", c.baseURL+path)
	startTime := time.Now()
	
	resp, err := c.client.Do(req)
	if err != nil {
		logger.S().Errorw("[LLM] HTTP request failed",
			"error", err,
			"duration", time.Since(startTime).String())
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.S().Debugw("[LLM] HTTP response received",
		"status_code", resp.StatusCode,
		"duration", time.Since(startTime).String())

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.S().Errorw("[LLM] Failed to read response body", "error", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		logger.S().Errorw("[LLM] API returned error status",
			"status_code", resp.StatusCode,
			"response", string(respBody))
		return nil, classifyError(resp.StatusCode, string(respBody))
	}

	logger.S().Debugw("[LLM] Request completed successfully",
		"response_size", len(respBody),
		"total_duration", time.Since(startTime).String())
	return respBody, nil
}

// doAnthropicRequest sends request using Anthropic authentication header (x-api-key)
func (c *httpClient) doAnthropicRequest(ctx context.Context, body any) ([]byte, error) {
	logger.S().Debugw("[LLM] Preparing Anthropic request")
	
	data, err := json.Marshal(body)
	if err != nil {
		logger.S().Errorw("[LLM] Failed to marshal Anthropic request body", "error", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		logger.S().Errorw("[LLM] Failed to create Anthropic HTTP request", "error", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	logger.S().Debugw("[LLM] Sending Anthropic API request", "url", c.baseURL+"/v1/messages")
	startTime := time.Now()
	
	resp, err := c.client.Do(req)
	if err != nil {
		logger.S().Errorw("[LLM] Anthropic HTTP request failed",
			"error", err,
			"duration", time.Since(startTime).String())
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.S().Debugw("[LLM] Anthropic response received",
		"status_code", resp.StatusCode,
		"duration", time.Since(startTime).String())

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.S().Errorw("[LLM] Failed to read Anthropic response body", "error", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		logger.S().Errorw("[LLM] Anthropic API returned error status",
			"status_code", resp.StatusCode,
			"response", string(respBody))
		return nil, classifyError(resp.StatusCode, string(respBody))
	}

	logger.S().Debugw("[LLM] Anthropic request completed successfully",
		"response_size", len(respBody),
		"total_duration", time.Since(startTime).String())
	return respBody, nil
}