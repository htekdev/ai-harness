package completion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(ClientConfig{
		BaseURL: "https://api.example.com",
		APIKey:  "test-key",
	})

	if c.config.Model != "gpt-4o" {
		t.Fatalf("expected default model 'gpt-4o', got %q", c.config.Model)
	}
	if c.config.MaxRetries != 3 {
		t.Fatalf("expected default max retries 3, got %d", c.config.MaxRetries)
	}
	if c.config.Timeout != 60*time.Second {
		t.Fatalf("expected default timeout 60s, got %v", c.config.Timeout)
	}
}

func TestCompleteSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}

		resp := Response{
			ID:    "chatcmpl-123",
			Model: "gpt-4o",
			Choices: []Choice{
				{
					Index:        0,
					Message:      Message{Role: RoleAssistant, Content: "Hello!"},
					FinishReason: "stop",
				},
			},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Fatalf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

func TestCompleteWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			ID:    "chatcmpl-456",
			Model: "gpt-4o",
			Choices: []Choice{
				{
					Index: 0,
					Message: Message{
						Role: RoleAssistant,
						ToolCalls: []ToolCall{
							{
								ID:   "call_abc",
								Type: "function",
								Function: FunctionCall{
									Name:      "read_file",
									Arguments: `{"path":"/tmp/test.txt"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key"})
	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Read the file"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatal("expected 1 tool call")
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("unexpected tool name: %s", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
}

func TestCompleteRetryOn500(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
			return
		}
		resp := Response{
			Choices: []Choice{{Message: Message{Role: RoleAssistant, Content: "recovered"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 5})
	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "recovered" {
		t.Fatalf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestCompleteNoRetryOn400(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 3})
	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no retry on 400), got %d", attempts)
	}
}

func TestCompleteContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", Timeout: 10 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
}

func TestRetryableError(t *testing.T) {
	err := &RetryableError{Err: fmt.Errorf("network timeout")}
	if err.Error() != "network timeout" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
	if !isRetryable(err) {
		t.Fatal("expected error to be retryable")
	}
	if isRetryable(fmt.Errorf("normal error")) {
		t.Fatal("expected normal error to not be retryable")
	}
}
