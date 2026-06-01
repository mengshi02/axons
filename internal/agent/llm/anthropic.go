package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// AnthropicClient is the Anthropic Messages API client
type AnthropicClient struct {
	client *httpClient
	model  string
}

// NewAnthropicClient creates an Anthropic client
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	c := newHTTPClient(apiKey, "https://api.anthropic.com")
	// Anthropic uses x-api-key instead of Authorization: Bearer
	c.apiKey = apiKey
	return &AnthropicClient{client: c, model: model}
}

// NewAnthropicClientWithBaseURL creates an Anthropic client with custom baseURL
func NewAnthropicClientWithBaseURL(apiKey, model, baseURL string) *AnthropicClient {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &AnthropicClient{
		client: newHTTPClient(apiKey, baseURL),
		model:  model,
	}
}

// SupportsTools returns true, Anthropic supports tool calling
func (c *AnthropicClient) SupportsTools() bool {
	return true
}

// Call invokes Anthropic Messages API
func (c *AnthropicClient) Call(ctx context.Context, req *Request) (*Response, error) {
	body := c.buildRequest(req)
	respBody, err := c.client.doAnthropicRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	return c.parseResponse(respBody)
}

// buildRequest builds Anthropic request body
func (c *AnthropicClient) buildRequest(req *Request) map[string]any {
	// Separate system prompt from regular messages
	var systemPrompt string
	messages := make([]map[string]any, 0, len(req.Messages))

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}

		m := map[string]any{}

		switch msg.Role {
		case "user":
			// Multimodal: images
			if len(msg.Images) > 0 {
				parts := make([]map[string]any, 0, 1+len(msg.Images))
				if msg.Content != "" {
					parts = append(parts, map[string]any{
						"type": "text",
						"text": msg.Content,
					})
				}
				for _, imgDataURL := range msg.Images {
					// Anthropic supports base64 data URL, format: data:<media_type>;base64,<data>
					mediaType, data := splitDataURL(imgDataURL)
					if data != "" {
						parts = append(parts, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": mediaType,
								"data":       data,
							},
						})
					} else {
						// URL form
						parts = append(parts, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type": "url",
								"url":  imgDataURL,
							},
						})
					}
				}
				m["role"] = "user"
				m["content"] = parts
			} else {
				m["role"] = "user"
				m["content"] = msg.Content
			}

		case "assistant":
			// Tool call message
			if len(msg.ToolCalls) > 0 {
				parts := make([]map[string]any, 0, len(msg.ToolCalls)+1)
				if msg.Content != "" {
					parts = append(parts, map[string]any{
						"type": "text",
						"text": msg.Content,
					})
				}
				for _, tc := range msg.ToolCalls {
					var inputMap map[string]any
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
					if inputMap == nil {
						inputMap = map[string]any{}
					}
					parts = append(parts, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": inputMap,
					})
				}
				m["role"] = "assistant"
				m["content"] = parts
			} else {
				m["role"] = "assistant"
				m["content"] = msg.Content
			}

		case "tool":
			// Tool result message: Anthropic requires wrapping in user message's tool_result block
			m["role"] = "user"
			m["content"] = []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": msg.ToolID,
					"content":     msg.Content,
				},
			}
		}

		if len(m) > 0 {
			messages = append(messages, m)
		}
	}

	body := map[string]any{
		"model":    c.model,
		"messages": messages,
	}

	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else {
		body["max_tokens"] = 4096
	}

	// Tool definitions
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": t.Function.Parameters,
			}
		}
		body["tools"] = tools
	}

	return body
}

// anthropicResponse represents Anthropic API response structure
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// parseResponse parses Anthropic response
func (c *AnthropicClient) parseResponse(body []byte) (*Response, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse anthropic response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("anthropic API error: %s", resp.Error.Message)
	}

	result := &Response{
		Usage: Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			argsBytes := []byte(block.Input)
			if len(argsBytes) == 0 {
				argsBytes = []byte("{}")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(argsBytes),
				},
			})
		}
	}

	return result, nil
}

// splitDataURL parses data URL and returns (mediaType, base64Data)
// e.g. "data:image/png;base64,iVBORw0..." → ("image/png", "iVBORw0...")
func splitDataURL(dataURL string) (mediaType, data string) {
	const prefix = "data:"
	if len(dataURL) < len(prefix) || dataURL[:len(prefix)] != prefix {
		return "", ""
	}
	rest := dataURL[len(prefix):]
	// 找 ;base64,
	for i := 0; i < len(rest); i++ {
		if rest[i] == ';' {
			mediaType = rest[:i]
			suffix := rest[i+1:]
			const b64prefix = "base64,"
			if len(suffix) > len(b64prefix) && suffix[:len(b64prefix)] == b64prefix {
				data = suffix[len(b64prefix):]
			}
			return
		}
	}
	return "", ""
}