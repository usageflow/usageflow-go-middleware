// Package vibe is the Go port of @usageflow/vibe: a framework-agnostic client that wraps
// Anthropic and OpenAI behind one interface, gating every call on a UsageFlow
// request_for_allocation reservation and settling real usage with use_allocation.
//
// Beta: this package's API may change between minor versions.
package vibe

import (
	"fmt"
	"time"
)

// Provider is an upstream LLM provider.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
)

// Message is one chat turn. Role is "user", "assistant" or "system".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest describes a metered chat call.
type ChatRequest struct {
	// Identity is the principal (customer/tenant) the call is metered against — the ledger alias.
	Identity string
	Provider Provider
	Model    string
	Messages []Message
	// MaxTokens is the enforced output cap and part of the reservation ceiling (default 1024).
	MaxTokens   int
	Temperature *float64
	// Workflow is an optional Vibe policy slug, sent as allocationMetadata.workflowId.
	Workflow string
	// Metadata is free-form trace annotation, merged into the request body metadata.
	Metadata map[string]any
	// CustomerMetadata is durable context about Identity. Only string/number/bool values are kept.
	CustomerMetadata map[string]any
	// Tools / ToolChoice are passed through untouched to the provider.
	Tools      []any
	ToolChoice any
}

// Usage is real token usage reported by the provider.
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ToolCall is a normalized tool invocation requested by the model.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// PolicyDecision is a matched Vibe policy tier echoed back on the reservation response.
type PolicyDecision struct {
	PolicyID  string `json:"policyId"`
	TierIndex int    `json:"tierIndex"`
	Action    struct {
		Type   string         `json:"type"` // ROUTE_MODEL | DEGRADE
		Params map[string]any `json:"params,omitempty"`
	} `json:"action"`
}

// ChatResult is the outcome of Chat / the final value of Stream.
type ChatResult struct {
	Provider Provider `json:"provider"`
	// Model is what actually ran (policy-routed model or the provider's echoed model).
	Model string `json:"model"`
	// RequestedModel is what the caller asked for, before any policy routing.
	RequestedModel string          `json:"requestedModel"`
	Content        string          `json:"content"`
	Usage          Usage           `json:"usage"`
	ToolCalls      []ToolCall      `json:"toolCalls,omitempty"`
	VibePolicy     *PolicyDecision `json:"vibePolicy,omitempty"`
	Raw            map[string]any  `json:"raw"`
}

// EmbedRequest describes a metered OpenAI embeddings call.
type EmbedRequest struct {
	Identity         string
	Input            []string
	Model            string
	Workflow         string
	Metadata         map[string]any
	CustomerMetadata map[string]any
}

// EmbedResult is the outcome of Embed.
type EmbedResult struct {
	Model          string          `json:"model"`
	RequestedModel string          `json:"requestedModel"`
	Embeddings     [][]float64     `json:"embeddings"`
	Usage          Usage           `json:"usage"`
	VibePolicy     *PolicyDecision `json:"vibePolicy,omitempty"`
}

// WithdrawRequest is a manual ledger adjustment (also used for Credit).
type WithdrawRequest struct {
	Identity        string
	Amount          float64
	Unit            string // descriptive only, default "credits"
	IdempotencyKey  string
	Reason          string
	SourceEventType string
	// Workflow is an optional Vibe policy slug (allocationMetadata.workflowId).
	Workflow         string
	CustomerMetadata map[string]any
	// HoldFor is how long a WithdrawAsync/CreditAsync hold stays open (default 24h). Close it
	// before then: a hold that expires unclosed is released without charging. Ignored by
	// Withdraw/Credit, which settle immediately.
	HoldFor time.Duration
}

// CaptureResult is returned by WithdrawAsync: the reservation is approved but not settled.
// Pass CaptureID to Close.
type CaptureResult struct {
	CaptureID      string  `json:"captureId"`
	Identity       string  `json:"identity"`
	Amount         float64 `json:"amount"` // signed: negative for CreditAsync
	IdempotencyKey string  `json:"idempotencyKey"`
	// ExpiresAt (epoch ms) is when the hold lapses; Close before then.
	ExpiresAt int64 `json:"expiresAt"`
}

// CloseRequest settles a capture. Amount nil means the reserved amount.
type CloseRequest struct {
	CaptureID string
	Amount    *float64
	// Identity is required only when Close runs in a different process than WithdrawAsync.
	Identity         string
	Workflow         string
	CustomerMetadata map[string]any
}

// WithdrawResult is the outcome of Withdraw / Credit.
type WithdrawResult struct {
	Identity       string  `json:"identity"`
	Amount         float64 `json:"amount"`
	IdempotencyKey string  `json:"idempotencyKey"`
	EventID        string  `json:"eventId"`
}

// RejectionError is returned when UsageFlow denies the call (quota/policy) — the provider
// call never happens.
type RejectionError struct {
	Message string
	Reason  string
}

func (e *RejectionError) Error() string {
	if e.Reason != "" && e.Reason != e.Message {
		return fmt.Sprintf("usageflow vibe: %s (%s)", e.Message, e.Reason)
	}
	return "usageflow vibe: " + e.Message
}

// Options configures a Client.
type Options struct {
	APIKey string // UsageFlow API key (falls back to USAGEFLOW_API_KEY)
	// AnthropicAPIKey / OpenAIAPIKey override ANTHROPIC_API_KEY / OPENAI_API_KEY; resolved lazily.
	AnthropicAPIKey string
	OpenAIAPIKey    string
	PoolSize        int
	WSURL           string // falls back to USAGEFLOW_WS_URL
	// Base URLs are for tests and proxies.
	AnthropicBaseURL string
	OpenAIBaseURL    string
}
