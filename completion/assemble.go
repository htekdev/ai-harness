package completion

import (
	"context"
	"fmt"
	"strings"
)

// AssembledStream is the final reconstructed result of consuming a
// CompleteStream channel: a single assistant Message (content + tool_calls)
// plus the model's finish reason. Token usage is intentionally absent —
// most providers do not emit usage on streaming responses, and callers
// should rely on per-iteration accounting elsewhere if they need it.
type AssembledStream struct {
	Message      Message
	FinishReason string
}

// DeltaCallback is invoked for every non-empty text delta emitted during
// streaming. It runs synchronously on the assembler goroutine, so callers
// must not block for long periods.
type DeltaCallback func(delta string)

// AssembleStream drains chunks from CompleteStream, calls onDelta for each
// text delta, and reconstructs a single assistant Message (content +
// tool_calls). It returns when the stream emits Done, when the channel is
// closed, or when ctx is cancelled.
//
// Tool-call deltas are merged by index into a sparse array, then compacted
// into Message.ToolCalls in index order. Argument fragments concatenate
// in arrival order; ID/Type/Name update only when the delta provides them
// (empty strings never overwrite established values).
func AssembleStream(ctx context.Context, chunks <-chan StreamChunk, onDelta DeltaCallback) (*AssembledStream, error) {
	var content strings.Builder
	finishReason := ""
	// Sparse map keyed by stream index — providers may emit out-of-order.
	toolBuf := map[int]*toolCallBuilder{}
	maxIdx := -1

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-chunks:
			if !ok {
				return finalize(&content, finishReason, toolBuf, maxIdx), nil
			}
			if chunk.Err != nil {
				return nil, chunk.Err
			}
			if chunk.Delta != "" {
				content.WriteString(chunk.Delta)
				if onDelta != nil {
					onDelta(chunk.Delta)
				}
			}
			for _, td := range chunk.ToolCallDeltas {
				idx := td.Index
				if idx > maxIdx {
					maxIdx = idx
				}
				b, exists := toolBuf[idx]
				if !exists {
					b = &toolCallBuilder{}
					toolBuf[idx] = b
				}
				if td.ID != "" {
					b.id = td.ID
				}
				if td.Type != "" {
					b.kind = td.Type
				}
				if td.Function.Name != "" {
					b.name = td.Function.Name
				}
				if td.Function.Arguments != "" {
					b.args.WriteString(td.Function.Arguments)
				}
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
			if chunk.Done {
				return finalize(&content, finishReason, toolBuf, maxIdx), nil
			}
		}
	}
}

type toolCallBuilder struct {
	id   string
	kind string
	name string
	args strings.Builder
}

func (b *toolCallBuilder) build(fallbackIdx int) ToolCall {
	id := b.id
	if id == "" {
		// Defensive: synthesize a stable id so downstream tool-result
		// pairing has something to anchor on. Real providers always
		// emit an ID; this only triggers on misbehaving streams.
		id = fmt.Sprintf("call_idx_%d", fallbackIdx)
	}
	kind := b.kind
	if kind == "" {
		kind = "function"
	}
	return ToolCall{
		ID:   id,
		Type: kind,
		Function: FunctionCall{
			Name:      b.name,
			Arguments: b.args.String(),
		},
	}
}

func finalize(content *strings.Builder, finishReason string, toolBuf map[int]*toolCallBuilder, maxIdx int) *AssembledStream {
	msg := Message{
		Role:    RoleAssistant,
		Content: content.String(),
	}
	if maxIdx >= 0 {
		calls := make([]ToolCall, 0, len(toolBuf))
		for i := 0; i <= maxIdx; i++ {
			b, ok := toolBuf[i]
			if !ok {
				continue
			}
			calls = append(calls, b.build(i))
		}
		msg.ToolCalls = calls
	}
	return &AssembledStream{Message: msg, FinishReason: finishReason}
}
