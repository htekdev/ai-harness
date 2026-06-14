// Package completion provides an OpenAI-compatible completion client
// for the AI harness. Supports GitHub Copilot API and any compatible endpoint.
package completion

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Role represents a message role in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the function name and arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// FunctionCallDelta contains partial tool-call fields emitted during streaming.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCallDelta represents a partial tool call emitted during streaming.
type ToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function FunctionCallDelta `json:"function,omitempty"`
}

// StreamChunk is a single streamed completion update.
type StreamChunk struct {
	Delta          string          `json:"delta,omitempty"`
	ToolCallDeltas []ToolCallDelta `json:"tool_call_deltas,omitempty"`
	FinishReason   string          `json:"finish_reason,omitempty"`
	Done           bool            `json:"done"`
	Err            error           `json:"-"`
}

// Request is the completion API request payload.
type Request struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []map[string]any `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

// Response is the completion API response payload.
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage contains token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ClientConfig holds the configuration for the completion client.
type ClientConfig struct {
	// BaseURL is the API endpoint (e.g., "https://api.githubcopilot.com" or "https://api.openai.com/v1")
	BaseURL string
	// APIKey is the authentication token
	APIKey string
	// Model is the default model to use
	Model string
	// MaxRetries is the maximum number of retry attempts. Ignored when
	// RetryPolicy is set; kept for backward compatibility.
	MaxRetries int
	// RetryPolicy fully configures retry behavior (max attempts, initial
	// backoff, max backoff, multiplier). When nil, a policy is synthesized
	// from MaxRetries with default backoff parameters.
	RetryPolicy *RetryPolicy
	// Timeout is the HTTP request timeout
	Timeout time.Duration
}

// Client is an OpenAI-compatible completion client.
type Client struct {
	config     ClientConfig
	httpClient *http.Client
}

type streamResponse struct {
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Content   string                `json:"content"`
	ToolCalls []streamToolCallDelta `json:"tool_calls"`
}

type streamToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function FunctionCallDelta `json:"function"`
}

// NewClient creates a new completion client with the given configuration.
func NewClient(config ClientConfig) *Client {
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}
	if config.Model == "" {
		config.Model = "gpt-4o"
	}
	if config.RetryPolicy == nil {
		p := DefaultRetryPolicy()
		p.MaxRetries = config.MaxRetries
		config.RetryPolicy = &p
	} else {
		// Normalize zero-value backoff fields.
		norm := config.RetryPolicy.withDefaults()
		// Honor explicit MaxRetries on the policy (including 0 = no retries).
		norm.MaxRetries = config.RetryPolicy.MaxRetries
		config.RetryPolicy = &norm
		// Mirror onto MaxRetries for legacy readers.
		config.MaxRetries = norm.MaxRetries
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Complete sends a completion request and returns the response.
// It retries on transient errors with exponential backoff.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	req = c.prepareRequest(req)
	req.Messages = SanitizeMessages(req.Messages)

	if err := ValidateMessages(req.Messages); err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= c.config.RetryPolicy.MaxRetries; attempt++ {
		if err := c.waitForRetry(ctx, attempt); err != nil {
			return nil, err
		}

		resp, err := c.doRequest(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// CompleteStream opens a streaming completion request and emits SSE chunks.
func (c *Client) CompleteStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	req = c.prepareRequest(req)
	req.Stream = true
	req.Messages = SanitizeMessages(req.Messages)

	if err := ValidateMessages(req.Messages); err != nil {
		return nil, err
	}

	var (
		httpResp *http.Response
		lastErr  error
	)

	for attempt := 0; attempt <= c.config.RetryPolicy.MaxRetries; attempt++ {
		if err := c.waitForRetry(ctx, attempt); err != nil {
			return nil, err
		}

		var err error
		httpResp, err = c.doStreamRequest(ctx, req)
		if err == nil {
			break
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}

	if httpResp == nil {
		return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
	}

	chunks := make(chan StreamChunk)
	go c.consumeStream(ctx, httpResp, chunks)
	return chunks, nil
}

func (c *Client) prepareRequest(req Request) Request {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	return req
}

func (c *Client) waitForRetry(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}

	backoff := c.config.RetryPolicy.Backoff(attempt)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}

func (c *Client) doRequest(ctx context.Context, req Request) (*Response, error) {
	httpResp, err := c.doStreamRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

func (c *Client) doStreamRequest(ctx context.Context, req Request) (*http.Response, error) {
	httpReq, err := c.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &RetryableError{Err: err}
	}

	if httpResp.StatusCode >= 400 {
		defer httpResp.Body.Close()
		respBody, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("read error response: %w", readErr)
		}
		return nil, classifyStatusError(httpResp.StatusCode, respBody)
	}

	return httpResp, nil
}

func (c *Client) newRequest(ctx context.Context, req Request) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	return httpReq, nil
}

func classifyStatusError(statusCode int, body []byte) error {
	if statusCode >= 500 {
		return &RetryableError{Err: fmt.Errorf("server error %d: %s", statusCode, string(body))}
	}
	if statusCode == 429 {
		return &RetryableError{Err: fmt.Errorf("rate limited: %s", string(body))}
	}
	return fmt.Errorf("API error %d: %s", statusCode, string(body))
}

func (c *Client) consumeStream(ctx context.Context, httpResp *http.Response, chunks chan<- StreamChunk) {
	defer httpResp.Body.Close()
	defer close(chunks)

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventData []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, err := processStreamEvent(ctx, chunks, eventData)
			eventData = nil
			if err != nil {
				sendStreamChunk(ctx, chunks, StreamChunk{Err: err, Done: true})
				return
			}
			if done {
				return
			}
			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		sendStreamChunk(ctx, chunks, StreamChunk{Err: fmt.Errorf("read stream: %w", err), Done: true})
		return
	}

	if len(eventData) > 0 {
		done, err := processStreamEvent(ctx, chunks, eventData)
		if err != nil {
			sendStreamChunk(ctx, chunks, StreamChunk{Err: err, Done: true})
			return
		}
		if done {
			return
		}
	}

	sendStreamChunk(ctx, chunks, StreamChunk{Done: true})
}

func processStreamEvent(ctx context.Context, chunks chan<- StreamChunk, eventData []string) (bool, error) {
	if len(eventData) == 0 {
		return false, nil
	}

	payload := strings.Join(eventData, "\n")
	if payload == "[DONE]" {
		sendStreamChunk(ctx, chunks, StreamChunk{Done: true})
		return true, nil
	}

	var resp streamResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		return false, fmt.Errorf("unmarshal stream chunk: %w", err)
	}

	for _, choice := range resp.Choices {
		chunk := StreamChunk{
			Delta:        choice.Delta.Content,
			FinishReason: choice.FinishReason,
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			chunk.ToolCallDeltas = append(chunk.ToolCallDeltas, ToolCallDelta{
				Index: toolCall.Index,
				ID:    toolCall.ID,
				Type:  toolCall.Type,
				Function: FunctionCallDelta{
					Name:      toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				},
			})
		}
		if chunk.Delta == "" && len(chunk.ToolCallDeltas) == 0 && chunk.FinishReason == "" {
			continue
		}
		if !sendStreamChunk(ctx, chunks, chunk) {
			return true, ctx.Err()
		}
	}

	return false, nil
}

func sendStreamChunk(ctx context.Context, chunks chan<- StreamChunk, chunk StreamChunk) bool {
	select {
	case <-ctx.Done():
		return false
	case chunks <- chunk:
		return true
	}
}

// RetryableError indicates an error that should be retried.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// isRetryable checks if an error should trigger a retry.
func isRetryable(err error) bool {
	_, ok := err.(*RetryableError)
	return ok
}
