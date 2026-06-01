package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// OpenAIClient is the OpenAI API client
type OpenAIClient struct {
	client  *httpClient
	model   string
}

// NewOpenAIClient creates an OpenAI client
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIClient{
		client:  newHTTPClient(apiKey, "https://api.openai.com/v1"),
		model:   model,
	}
}

// NewOpenAIClientWithBaseURL creates an OpenAI client with custom baseURL
func NewOpenAIClientWithBaseURL(apiKey, model, baseURL string) *OpenAIClient {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIClient{
		client:  newHTTPClient(apiKey, baseURL),
		model:   model,
	}
}

// Call invokes OpenAI API
func (c *OpenAIClient) Call(ctx context.Context, req *Request) (*Response, error) {
	// Build OpenAI request
	openaiReq := c.buildRequest(req)

	// Call API
	respBody, err := c.client.doRequest(ctx, "POST", "/chat/completions", openaiReq)
	if err != nil {
		return nil, err
	}

	// Parse response
	return c.parseResponse(respBody)
}

// SupportsTools 返回 true，OpenAI 支持工具调用
func (c *OpenAIClient) SupportsTools() bool {
	return true
}

// buildRequest builds OpenAI request
func (c *OpenAIClient) buildRequest(req *Request) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))

	for _, msg := range req.Messages {
		m := map[string]any{
			"role": msg.Role,
		}

		// Multimodal: build content array when images are present
		if len(msg.Images) > 0 && msg.Role == "user" {
			contentParts := make([]map[string]any, 0, 1+len(msg.Images))
			if msg.Content != "" {
				contentParts = append(contentParts, map[string]any{
					"type": "text",
					"text": msg.Content,
				})
			}
			for _, imgDataUrl := range msg.Images {
				// Ensure we have a proper data URL; pass as-is to OpenAI image_url
				url := imgDataUrl
				if len(url) > 0 {
					contentParts = append(contentParts, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": url,
						},
					})
				}
			}
			m["content"] = contentParts
		} else if msg.Content != "" {
			m["content"] = msg.Content
		}

		if msg.Name != "" {
			m["name"] = msg.Name
		}

		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCalls[i] = map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
			}
			m["tool_calls"] = toolCalls
		}

		if msg.ToolID != "" {
			m["tool_call_id"] = msg.ToolID
		}

		messages = append(messages, m)
	}

	body := map[string]any{
		"model":    c.model,
		"messages": messages,
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, tool := range req.Tools {
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				},
			}
		}
		body["tools"] = tools
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}

	return body
}

// openAIResponse represents OpenAI API response structure
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// parseResponse parses OpenAI response
func (c *OpenAIClient) parseResponse(body []byte) (*Response, error) {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("OpenAI API error: %s", resp.Error.Message)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	result := &Response{
		Content: choice.Message.Content,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			result.ToolCalls[i] = ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	// Fallback: some models (e.g. Qwen via DashScope) output tool calls as XML in content
	// instead of using the standard function calling API.
	if len(result.ToolCalls) == 0 && strings.Contains(result.Content, "<tool_call>") {
		xmlCalls, remaining := extractXMLToolCalls(result.Content)
		if len(xmlCalls) > 0 {
			result.ToolCalls = xmlCalls
			result.Content = remaining
		}
	}

	return result, nil
}

// xmlToolCallRe matches <tool_call>...</tool_call> blocks (including multiline)
var xmlToolCallRe = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)

// extractXMLToolCalls parses <tool_call>{"name":...,"arguments":...}</tool_call> from content.
// Returns parsed ToolCalls and the remaining text after stripping the XML blocks.
func extractXMLToolCalls(content string) ([]ToolCall, string) {
	matches := xmlToolCallRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, content
	}

	var calls []ToolCall
	for i, loc := range matches {
		inner := strings.TrimSpace(content[loc[2]:loc[3]])
		var parsed struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(inner), &parsed); err != nil {
			continue
		}
		argsStr := string(parsed.Arguments)
		if argsStr == "" || argsStr == "null" {
			argsStr = "{}"
		}
		calls = append(calls, ToolCall{
			ID:   fmt.Sprintf("xml_tc_%d", i),
			Type: "function",
			Function: FunctionCall{
				Name:      parsed.Name,
				Arguments: argsStr,
			},
		})
	}

	// Strip all <tool_call>...</tool_call> blocks from content
	remaining := strings.TrimSpace(xmlToolCallRe.ReplaceAllString(content, ""))
	return calls, remaining
}