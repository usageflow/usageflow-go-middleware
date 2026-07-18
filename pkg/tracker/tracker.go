// Package tracker holds per-request call-chain state for UsageFlow Go agents.
//
// Preferred UX: build with `usageflow go build` so BeginCall/End hooks are
// injected automatically. Gin middleware attaches request context and
// sends report_call_chain. Track/Wrap remain available as an advanced escape hatch.
package tracker

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// TokenUsage mirrors LLM usage fields used by JS/Python agents.
type TokenUsage struct {
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
}

// FunctionCallRecord is one recorded call in a request chain (camelCase wire format).
type FunctionCallRecord struct {
	FuncName           string                 `json:"funcName"`
	FilePath           string                 `json:"filePath"`
	ModuleName         string                 `json:"moduleName,omitempty"`
	ParamsSchema       map[string]interface{} `json:"paramsSchema,omitempty"`
	ResultSchema       interface{}            `json:"resultSchema,omitempty"`
	Usage              *TokenUsage            `json:"usage,omitempty"`
	DurationMs         int64                  `json:"durationMs,omitempty"`
	UsageflowRequestId string                 `json:"usageflowRequestId,omitempty"`
	AIModel            string                 `json:"aiModel,omitempty"`
	UserPrompt         string                 `json:"userPrompt,omitempty"`
}

// RequestContext identifies the HTTP route that started tracking.
type RequestContext struct {
	Method string
	URL    string
}

// TrackingContext holds the per-request call chain.
type TrackingContext struct {
	mu                 sync.Mutex
	CallChain          []FunctionCallRecord
	RequestContext     *RequestContext
	UsageflowRequestId string
	AllocationID       string
	LedgerID           string
	RequestMetadata    map[string]interface{}
	liveRecords        []liveRecord
}

type contextKey struct{}

var (
	enabled atomic.Bool
)

func init() {
	// Explicit Track/Wrap is opt-in; default on until server/env disables discovery.
	enabled.Store(!envDiscoveryDisabled())
}

func envDiscoveryDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("USAGEFLOW_DISCOVERY_DISABLED")))
	return v == "1" || v == "true" || v == "yes"
}

// Enable turns on call-chain recording for Track/Wrap.
func Enable() {
	enabled.Store(true)
}

// Disable turns off recording; Track/Wrap become pass-through.
func Disable() {
	enabled.Store(false)
}

// IsEnabled reports whether tracking is currently active.
func IsEnabled() bool {
	if envDiscoveryDisabled() {
		return false
	}
	return enabled.Load()
}

// SetEnabled sets the discovery flag (typically from server discoveryDisabled).
func SetEnabled(on bool) {
	enabled.Store(on)
}

// WithTracking attaches a new TrackingContext to ctx.
func WithTracking(ctx context.Context, reqCtx *RequestContext, usageflowRequestID string) (context.Context, *TrackingContext) {
	store := &TrackingContext{
		CallChain:          make([]FunctionCallRecord, 0, 8),
		RequestContext:     reqCtx,
		UsageflowRequestId: usageflowRequestID,
	}
	return context.WithValue(ctx, contextKey{}, store), store
}

// FromContext returns the request TrackingContext, if any.
func FromContext(ctx context.Context) *TrackingContext {
	if ctx == nil {
		return nil
	}
	store, _ := ctx.Value(contextKey{}).(*TrackingContext)
	return store
}

// SetUsageflowRequestID updates the correlation id on an active store.
func SetUsageflowRequestID(ctx context.Context, id string) {
	store := FromContext(ctx)
	if store == nil {
		return
	}
	store.mu.Lock()
	store.UsageflowRequestId = id
	store.mu.Unlock()
}

// GetCallChain returns a copy of the current request call chain.
func GetCallChain(ctx context.Context) []FunctionCallRecord {
	store := FromContext(ctx)
	if store == nil {
		return nil
	}
	return store.Snapshot()
}

// RequestID returns the correlation id for this request.
func (s *TrackingContext) RequestID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UsageflowRequestId
}

// Snapshot returns a copy of the call chain.
func (s *TrackingContext) Snapshot() []FunctionCallRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLiveRecordsLocked()
	if len(s.CallChain) == 0 {
		return nil
	}
	out := make([]FunctionCallRecord, len(s.CallChain))
	copy(out, s.CallChain)
	return out
}

// SetRequestAllocation stores HTTP allocation context for function-level metering metadata.
func (s *TrackingContext) SetRequestAllocation(allocationID, ledgerID string, metadata map[string]interface{}) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AllocationID = allocationID
	s.LedgerID = ledgerID
	s.RequestMetadata = metadata
}

// GetRequestAllocation returns the HTTP allocation context, if any.
func (s *TrackingContext) GetRequestAllocation() (allocationID, ledgerID string, metadata map[string]interface{}) {
	if s == nil {
		return "", "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AllocationID, s.LedgerID, s.RequestMetadata
}

func (s *TrackingContext) syncLiveRecordsLocked() {
	for _, live := range s.liveRecords {
		if live.ptr == nil || live.idx < 0 || live.idx >= len(s.CallChain) {
			continue
		}
		s.CallChain[live.idx] = *live.ptr
	}
}

// Append adds a record; never panics to the caller.
func (s *TrackingContext) Append(record FunctionCallRecord) {
	if s == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.UsageflowRequestId == "" && s.UsageflowRequestId != "" {
		record.UsageflowRequestId = s.UsageflowRequestId
	}
	s.CallChain = append(s.CallChain, record)
}
