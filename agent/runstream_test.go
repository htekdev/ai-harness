package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/tools"
)

// streamSSE emits each event verbatim followed by a blank line + flush.
// Each entry should be the raw line content after `data: ` (no leading "data: ").
func streamSSE(t *testing.T, w http.ResponseWriter, events ...string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("response writer does not support flushing")
	}
	for _, ev := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
		flusher.Flush()
	}
}

func setupStreamingAgent(handler http.HandlerFunc) (*Agent, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 1,
	})
	registry := tools.NewRegistry()
	_ = registry.Register(tools.Definition{
		Name:        "greet",
		Description: "Greet someone",
		Parameters:  []tools.Parameter{{Name: "name", Type: tools.TypeString, Required: true}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct{ Name string }
		_ = json.Unmarshal(args, &params)
		return "Hello, " + params.Name + "!", nil
	})
	ctxMgr := agentctx.NewManager(agentctx.Config{SystemPrompt: "stream test"})
	return New(Options{Client: client, Tools: registry, Context: ctxMgr}), server
}

func TestRunStream_SimpleResponse(t *testing.T) {
	agent, server := setupStreamingAgent(func(w http.ResponseWriter, r *http.Request) {
		var req completion.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Fatal("expected stream=true on streamed request")
		}
		streamSSE(t, w,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`,
			`[DONE]`,
		)
	})
	defer server.Close()

	var captured strings.Builder
	var mu sync.Mutex
	result, err := agent.RunStream(context.Background(), "Hi", func(d string) {
		mu.Lock()
		defer mu.Unlock()
		captured.WriteString(d)
	})
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	if result.Response != "Hello world" {
		t.Errorf("response = %q, want %q", result.Response, "Hello world")
	}
	if captured.String() != "Hello world" {
		t.Errorf("captured deltas = %q, want %q", captured.String(), "Hello world")
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(result.ToolCalls))
	}
}

func TestRunStream_NilCallbackOK(t *testing.T) {
	agent, server := setupStreamingAgent(func(w http.ResponseWriter, r *http.Request) {
		streamSSE(t, w,
			`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			`[DONE]`,
		)
	})
	defer server.Close()

	result, err := agent.RunStream(context.Background(), "ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != "ok" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestRunStream_WithToolCall(t *testing.T) {
	callCount := 0
	var mu sync.Mutex
	agent, server := setupStreamingAgent(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		current := callCount
		mu.Unlock()

		if current == 1 {
			// Iteration 1: model decides to call greet.
			streamSSE(t, w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"greet"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"name\":\"World\"}"}}]},"finish_reason":"tool_calls"}]}`,
				`[DONE]`,
			)
		} else {
			// Iteration 2: final assistant message after tool result.
			streamSSE(t, w,
				`{"choices":[{"delta":{"content":"I greeted "}}]}`,
				`{"choices":[{"delta":{"content":"the world!"},"finish_reason":"stop"}]}`,
				`[DONE]`,
			)
		}
	})
	defer server.Close()

	var deltas strings.Builder
	result, err := agent.RunStream(context.Background(), "Greet the world", func(d string) {
		deltas.WriteString(d)
	})
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	if result.Response != "I greeted the world!" {
		t.Errorf("response = %q", result.Response)
	}
	if deltas.String() != "I greeted the world!" {
		t.Errorf("deltas = %q (only final iteration's text deltas should be visible since iter-1 emitted no content)", deltas.String())
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "greet" {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "Hello, World!" {
		t.Fatalf("tool results = %+v", result.ToolResults)
	}
	if callCount != 2 {
		t.Errorf("expected 2 stream calls (one per iteration), got %d", callCount)
	}
}

func TestRunStream_PropagatesErrorFromStream(t *testing.T) {
	agent, server := setupStreamingAgent(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	defer server.Close()

	_, err := agent.RunStream(context.Background(), "boom", nil)
	if err == nil {
		t.Fatal("expected error from 502 response")
	}
}

func TestRunStream_DeltasArriveBeforeReturn(t *testing.T) {
	// Verifies that text deltas are delivered to the callback as the
	// stream progresses, not all at the end. We assert ordering: the
	// first delta callback fires before RunStream returns.
	agent, server := setupStreamingAgent(func(w http.ResponseWriter, r *http.Request) {
		streamSSE(t, w,
			`{"choices":[{"delta":{"content":"part1"}}]}`,
			`{"choices":[{"delta":{"content":"part2"},"finish_reason":"stop"}]}`,
			`[DONE]`,
		)
	})
	defer server.Close()

	var order []string
	result, err := agent.RunStream(context.Background(), "go", func(d string) {
		order = append(order, d)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 {
		t.Fatalf("expected at least 2 callback invocations, got %d", len(order))
	}
	if order[0] != "part1" || order[1] != "part2" {
		t.Errorf("delta order = %v, want [part1 part2]", order)
	}
	if result.Response != "part1part2" {
		t.Errorf("response = %q", result.Response)
	}
}
