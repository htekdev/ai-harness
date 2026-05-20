package evals

import (
	"sync"
	"sync/atomic"
)

// CostTracker tracks token usage and estimates cost.
type CostTracker struct {
	mu          sync.Mutex
	totalTokens int64
}

// Pricing constants for gpt-4o-mini (per 1M tokens).
const (
	// InputPricePerMillion is the cost per million input tokens.
	InputPricePerMillion = 0.15
	// OutputPricePerMillion is the cost per million output tokens.
	OutputPricePerMillion = 0.60
	// BlendedPricePerMillion is a conservative estimate blending input/output.
	BlendedPricePerMillion = 0.40
)

// Add records token usage.
func (ct *CostTracker) Add(tokens int) {
	atomic.AddInt64(&ct.totalTokens, int64(tokens))
}

// TotalTokens returns the cumulative token count.
func (ct *CostTracker) TotalTokens() int {
	return int(atomic.LoadInt64(&ct.totalTokens))
}

// EstimatedUSD returns the estimated cost in USD using blended pricing.
func (ct *CostTracker) EstimatedUSD() float64 {
	tokens := atomic.LoadInt64(&ct.totalTokens)
	return float64(tokens) * BlendedPricePerMillion / 1_000_000
}

// Reset clears the tracker.
func (ct *CostTracker) Reset() {
	atomic.StoreInt64(&ct.totalTokens, 0)
}
