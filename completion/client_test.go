package completion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			Choices: []Choice{{
				Index:        0,
				Message:      Message{Role: RoleAssistant, Content: "Hello!"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	resp, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "Hi"}}})
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
			Choices: []Choice{{
				Index: 0,
				Message: Message{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{{
						ID:   "call_abc",
						Type: "function",
						Function: FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"test.txt"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key"})
	resp, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "Read the file"}}})
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
			_, _ = w.Write([]byte("internal error"))
			return
		}
		resp := Response{Choices: []Choice{{Message: Message{Role: RoleAssistant, Content: "recovered"}, FinishReason: "stop"}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 5})
	resp, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "test"}}})
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

func TestCompleteRetryOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		json.NewEncoder(w).Encode(Response{Choices: []Choice{{Message: Message{Role: RoleAssistant, Content: "ok"}, FinishReason: "stop"}}})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 1})
	resp, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "retry"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestCompleteMaxRetriesExceeded(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("try later"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 1})
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "retry"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestCompleteModelOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "override-model" {
			t.Fatalf("expected override model, got %q", req.Model)
		}
		json.NewEncoder(w).Encode(Response{Choices: []Choice{{Message: Message{Role: RoleAssistant, Content: "override"}, FinishReason: "stop"}}})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", Model: "default-model"})
	_, err := client.Complete(context.Background(), Request{Model: "override-model", Messages: []Message{{Role: RoleUser, Content: "test"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteNoRetryOn400(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 3})
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "test"}}})
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

	_, err := client.Complete(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "test"}}})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
}

func TestCompleteStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !req.Stream {
			t.Fatal("expected stream=true")
		}
		if req.Model != "default-model" {
			t.Fatalf("expected default model, got %q", req.Model)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"message\\\":\"}}]}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\",\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"hi\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", Model: "default-model"})
	stream, err := client.CompleteStream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "Hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}
	if chunks[0].Delta != "Hel" {
		t.Fatalf("unexpected first delta: %+v", chunks[0])
	}
	if len(chunks[1].ToolCallDeltas) != 1 {
		t.Fatalf("expected tool delta in second chunk, got %+v", chunks[1])
	}
	if chunks[1].ToolCallDeltas[0].Function.Name != "echo" {
		t.Fatalf("unexpected tool name: %+v", chunks[1].ToolCallDeltas[0])
	}
	if chunks[2].Delta != "lo" {
		t.Fatalf("unexpected third delta: %+v", chunks[2])
	}
	if chunks[2].ToolCallDeltas[0].Function.Arguments != `"hi"}` {
		t.Fatalf("unexpected argument delta: %+v", chunks[2].ToolCallDeltas[0])
	}
	if chunks[2].FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q", chunks[2].FinishReason)
	}
	if !chunks[3].Done {
		t.Fatalf("expected final done chunk, got %+v", chunks[3])
	}
}

func TestCompleteStreamModelOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "stream-override" {
			t.Fatalf("expected override model, got %q", req.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", Model: "default-model"})
	stream, err := client.CompleteStream(context.Background(), Request{Model: "stream-override", Messages: []Message{{Role: RoleUser, Content: "test"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunk := <-stream
	if !chunk.Done {
		t.Fatalf("expected done chunk, got %+v", chunk)
	}
}

func TestCompleteStreamRetryOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("retry later"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 1})
	stream, err := client.CompleteStream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "stream"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunk := <-stream
	if !chunk.Done {
		t.Fatalf("expected done chunk, got %+v", chunk)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestCompleteStreamMaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("retry later"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key", MaxRetries: 1})
	_, err := client.CompleteStream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "stream"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteMarshalError(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "https://api.example.com", APIKey: "key", MaxRetries: 1})
	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "bad"}},
		Tools:    []map[string]any{{"bad": make(chan int)}},
	})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestCompleteInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key"})
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "bad response"}}})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestCompleteStreamInvalidChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {bad json}\n\n")
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "key"})
	stream, err := client.CompleteStream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "test"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunk := <-stream
	if chunk.Err == nil || !chunk.Done {
		t.Fatalf("expected error chunk, got %+v", chunk)
	}
}

func TestProcessStreamEventAndSendHelpers(t *testing.T) {
	chunks := make(chan StreamChunk, 2)
	if done, err := processStreamEvent(context.Background(), chunks, nil); done || err != nil {
		t.Fatalf("expected empty event to be ignored, got done=%v err=%v", done, err)
	}
	if done, err := processStreamEvent(context.Background(), chunks, []string{"[DONE]"}); !done || err != nil {
		t.Fatalf("expected done event, got done=%v err=%v", done, err)
	}
	if chunk := <-chunks; !chunk.Done {
		t.Fatalf("expected done chunk, got %+v", chunk)
	}
	if done, err := processStreamEvent(context.Background(), chunks, []string{`{"choices":[{"delta":{}}]}`}); done || err != nil {
		t.Fatalf("expected empty delta chunk to be ignored, got done=%v err=%v", done, err)
	}
	if _, err := processStreamEvent(context.Background(), chunks, []string{"{bad json}"}); err == nil {
		t.Fatal("expected invalid JSON error")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if sendStreamChunk(cancelled, make(chan StreamChunk), StreamChunk{}) {
		t.Fatal("expected cancelled send to fail")
	}
}

type errorReadCloser struct {
	data []byte
	read bool
}

func (r *errorReadCloser) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("boom")
}

func (r *errorReadCloser) Close() error {
	return nil
}

func TestConsumeStreamEOFWithoutTrailingBlankLine(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(": keepalive\ndata: {\"choices\":[{\"delta\":{\"content\":\"tail\"}}]}"))}
	chunks := make(chan StreamChunk, 4)
	(&Client{}).consumeStream(context.Background(), resp, chunks)

	var got []StreamChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Delta != "tail" || !got[1].Done {
		t.Fatalf("unexpected chunks: %+v", got)
	}
}

func TestConsumeStreamEOFDoneSentinel(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: [DONE]"))}
	chunks := make(chan StreamChunk, 2)
	(&Client{}).consumeStream(context.Background(), resp, chunks)
	chunk := <-chunks
	if !chunk.Done {
		t.Fatalf("expected done chunk, got %+v", chunk)
	}
}

func TestConsumeStreamScannerError(t *testing.T) {
	resp := &http.Response{Body: &errorReadCloser{data: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")}}
	chunks := make(chan StreamChunk, 3)
	(&Client{}).consumeStream(context.Background(), resp, chunks)

	var got []StreamChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Delta != "x" {
		t.Fatalf("unexpected first chunk: %+v", got[0])
	}
	if got[1].Err == nil || !got[1].Done {
		t.Fatalf("expected scanner error chunk, got %+v", got[1])
	}
}

func TestRetryableError(t *testing.T) {
	err := &RetryableError{Err: fmt.Errorf("network timeout")}
	if err.Error() != "network timeout" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
	if err.Unwrap() == nil {
		t.Fatal("expected unwrap to return underlying error")
	}
	if !isRetryable(err) {
		t.Fatal("expected error to be retryable")
	}
	if isRetryable(fmt.Errorf("normal error")) {
		t.Fatal("expected normal error to not be retryable")
	}
}
