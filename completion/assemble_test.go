package completion

import (
	"context"
	"errors"
	"testing"
)

func sendChunks(chunks ...StreamChunk) <-chan StreamChunk {
	ch := make(chan StreamChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func TestAssembleStream_TextOnly(t *testing.T) {
	var captured []string
	chunks := sendChunks(
		StreamChunk{Delta: "Hello"},
		StreamChunk{Delta: ", "},
		StreamChunk{Delta: "world"},
		StreamChunk{Delta: "!"},
		StreamChunk{FinishReason: "stop", Done: true},
	)

	got, err := AssembleStream(context.Background(), chunks, func(d string) {
		captured = append(captured, d)
	})
	if err != nil {
		t.Fatalf("AssembleStream error: %v", err)
	}
	if got.Message.Content != "Hello, world!" {
		t.Errorf("content = %q, want %q", got.Message.Content, "Hello, world!")
	}
	if got.Message.Role != RoleAssistant {
		t.Errorf("role = %q, want %q", got.Message.Role, RoleAssistant)
	}
	if got.FinishReason != "stop" {
		t.Errorf("finish reason = %q, want stop", got.FinishReason)
	}
	if len(got.Message.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(got.Message.ToolCalls))
	}
	if len(captured) != 4 {
		t.Errorf("expected 4 onDelta calls, got %d", len(captured))
	}
}

func TestAssembleStream_NilCallback(t *testing.T) {
	chunks := sendChunks(
		StreamChunk{Delta: "ok"},
		StreamChunk{Done: true},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.Content != "ok" {
		t.Errorf("content = %q", got.Message.Content)
	}
}

func TestAssembleStream_SingleToolCall(t *testing.T) {
	chunks := sendChunks(
		// Provider opens the tool call envelope with id+name on first delta.
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, ID: "call_abc", Type: "function",
			Function: FunctionCallDelta{Name: "lookup"},
		}}},
		// Then streams arguments piecewise.
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, Function: FunctionCallDelta{Arguments: `{"q":`},
		}}},
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, Function: FunctionCallDelta{Arguments: `"hello"}`},
		}}},
		StreamChunk{FinishReason: "tool_calls", Done: true},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.Message.ToolCalls))
	}
	tc := got.Message.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("id = %q, want call_abc", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("type = %q, want function", tc.Type)
	}
	if tc.Function.Name != "lookup" {
		t.Errorf("name = %q, want lookup", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"q":"hello"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
	if got.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %q", got.FinishReason)
	}
}

func TestAssembleStream_ParallelToolCalls(t *testing.T) {
	// Two tool calls, deltas interleaved across indices.
	chunks := sendChunks(
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, ID: "call_a", Type: "function",
			Function: FunctionCallDelta{Name: "foo", Arguments: `{"x":1`},
		}}},
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 1, ID: "call_b", Type: "function",
			Function: FunctionCallDelta{Name: "bar", Arguments: `{"y":`},
		}}},
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, Function: FunctionCallDelta{Arguments: `}`},
		}}},
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 1, Function: FunctionCallDelta{Arguments: `2}`},
		}}},
		StreamChunk{Done: true},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Message.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(got.Message.ToolCalls))
	}
	if got.Message.ToolCalls[0].ID != "call_a" || got.Message.ToolCalls[0].Function.Arguments != `{"x":1}` {
		t.Errorf("tool 0: %+v", got.Message.ToolCalls[0])
	}
	if got.Message.ToolCalls[1].ID != "call_b" || got.Message.ToolCalls[1].Function.Arguments != `{"y":2}` {
		t.Errorf("tool 1: %+v", got.Message.ToolCalls[1])
	}
}

func TestAssembleStream_MixedTextAndToolCalls(t *testing.T) {
	chunks := sendChunks(
		StreamChunk{Delta: "Let me check"},
		StreamChunk{Delta: " that for you."},
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, ID: "c1", Type: "function",
			Function: FunctionCallDelta{Name: "lookup", Arguments: `{}`},
		}}},
		StreamChunk{Done: true},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.Content != "Let me check that for you." {
		t.Errorf("content = %q", got.Message.Content)
	}
	if len(got.Message.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(got.Message.ToolCalls))
	}
}

func TestAssembleStream_ChunkError(t *testing.T) {
	wantErr := errors.New("boom")
	chunks := sendChunks(
		StreamChunk{Delta: "partial"},
		StreamChunk{Err: wantErr, Done: true},
	)
	_, err := AssembleStream(context.Background(), chunks, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestAssembleStream_ContextCancel(t *testing.T) {
	ch := make(chan StreamChunk) // never sends
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AssembleStream(ctx, ch, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAssembleStream_ChannelClosedWithoutDone(t *testing.T) {
	// Stream closes cleanly without an explicit Done chunk — assembler
	// should still finalize with whatever it has.
	chunks := sendChunks(
		StreamChunk{Delta: "abc"},
		StreamChunk{FinishReason: "stop"},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.Content != "abc" {
		t.Errorf("content = %q", got.Message.Content)
	}
	if got.FinishReason != "stop" {
		t.Errorf("finish reason = %q", got.FinishReason)
	}
}

func TestAssembleStream_ToolCallIDFallback(t *testing.T) {
	// Misbehaving stream: tool delta arrives without an ID. Assembler
	// should synthesize a stable fallback rather than emit empty ID.
	chunks := sendChunks(
		StreamChunk{ToolCallDeltas: []ToolCallDelta{{
			Index: 0, Function: FunctionCallDelta{Name: "x", Arguments: `{}`},
		}}},
		StreamChunk{Done: true},
	)
	got, err := AssembleStream(context.Background(), chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.ToolCalls[0].ID == "" {
		t.Errorf("expected synthesized ID, got empty string")
	}
	if got.Message.ToolCalls[0].Type != "function" {
		t.Errorf("expected type=function fallback, got %q", got.Message.ToolCalls[0].Type)
	}
}
