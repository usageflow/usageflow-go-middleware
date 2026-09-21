package vibe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
)

// ErrCreditsUnsupported is returned by Credits when the UsageFlow server predates the
// get_credits message.
var ErrCreditsUnsupported = errors.New("usageflow vibe: this UsageFlow server does not support get_credits")

// CreditsRequest selects whose credits to read.
type CreditsRequest struct {
	// Identity is the principal you meter (the same Identity you pass to Chat / Withdraw).
	Identity string
	// Workflow limits the answer to one workflow (Vibe policy slug). Empty returns every
	// active workflow that applies to your application.
	Workflow string
	// CustomerMetadata (e.g. plan) is used to pick a policy branch when the identity has
	// no stored metadata yet. Optional.
	CustomerMetadata map[string]any
}

// AccountCredits is the account's plan request cap for the current billing period.
type AccountCredits struct {
	PlanID        string `json:"planId"`
	Used          int64  `json:"used"`
	Limit         *int64 `json:"limit"`     // nil = unlimited
	Remaining     *int64 `json:"remaining"` // nil = unlimited
	PeriodStartAt int64  `json:"periodStartAt"`
	PeriodEndAt   int64  `json:"periodEndAt"`
	Exceeded      bool   `json:"exceeded"`
}

// IdentityCredits is what an identity has consumed. Usage is counted per identity, not
// per workflow: every workflow's limit is measured against this one number.
type IdentityCredits struct {
	Identity string  `json:"identity"`
	Used     float64 `json:"used"`
	// Known is false when the identity has never made a request.
	Known bool `json:"known"`
}

// TierProgress is a tier the identity has not reached yet.
type TierProgress struct {
	TierIndex      int     `json:"tierIndex"`
	ThresholdField string  `json:"thresholdField"`
	ThresholdValue float64 `json:"thresholdValue"`
	CurrentValue   float64 `json:"currentValue"`
	Remaining      float64 `json:"remaining"`
	Action         struct {
		Type   string         `json:"type"`
		Params map[string]any `json:"params,omitempty"`
	} `json:"action"`
}

// WorkflowCredits is one workflow's limit measured against the identity's usage.
type WorkflowCredits struct {
	Workflow   string `json:"workflow"`
	PolicyID   string `json:"policyId"`
	PolicyName string `json:"policyName"`
	Status     string `json:"status"`
	// Limit is the usage at which the workflow blocks; nil when it never blocks.
	Limit     *float64 `json:"limit"`
	Used      float64  `json:"used"`
	Remaining *float64 `json:"remaining"` // nil when Limit is nil
	Blocked   bool     `json:"blocked"`
	// ResetInterval is how often usage renews (e.g. "1d"); empty means never.
	ResetInterval string        `json:"resetInterval,omitempty"`
	NextTier      *TierProgress `json:"nextTier"`
}

// CreditsResult is what Credits returns.
type CreditsResult struct {
	Account   *AccountCredits   `json:"account"` // nil when the server has no billing info
	Identity  IdentityCredits   `json:"identity"`
	Workflows []WorkflowCredits `json:"workflows"`
}

// Credits returns the account's plan credits, the identity's usage, and — per workflow —
// how many credits are left before it blocks. It is read-only: nothing is reserved or
// charged. Call it from your backend and show the numbers to your users.
func (c *Client) Credits(_ context.Context, req CreditsRequest) (*CreditsResult, error) {
	if strings.TrimSpace(req.Identity) == "" {
		return nil, errors.New("usageflow vibe: Identity is required")
	}
	payload := map[string]any{"identity": req.Identity}
	if req.Workflow != "" {
		payload["workflow"] = req.Workflow
	}
	if cm := sanitizeMetadata(req.CustomerMetadata); cm != nil {
		payload["customerMetadata"] = cm
	}

	resp, err := c.sock.SendAsync(&socket.UsageFlowSocketMessage{Type: "get_credits", Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("usageflow vibe: get_credits: %w", err)
	}
	if resp.Type == "error" {
		msg := resp.Error
		if msg == "" {
			msg = resp.Message
		}
		if strings.Contains(msg, "No handler registered") {
			return nil, ErrCreditsUnsupported
		}
		return nil, fmt.Errorf("usageflow vibe: get_credits: %s", msg)
	}

	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		return nil, err
	}
	out := &CreditsResult{}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("usageflow vibe: get_credits: unexpected response: %w", err)
	}
	if out.Workflows == nil {
		out.Workflows = []WorkflowCredits{}
	}
	return out, nil
}
