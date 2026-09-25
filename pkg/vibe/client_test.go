package vibe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
)

type mockSocket struct {
	mu   sync.Mutex
	sent []*socket.UsageFlowSocketMessage
	// sentAsync records only the SendAsync calls (reserves / get_credits); sentFF records
	// only the fire-and-forget Send calls (settles). sent records both, in order.
	sentFF  []*socket.UsageFlowSocketMessage
	reply   func(*socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse
	sendErr error // when set, Send (fire-and-forget) fails without recording the message
}

func (m *mockSocket) SendAsync(msg *socket.UsageFlowSocketMessage) (*socket.UsageFlowSocketResponse, error) {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	if m.reply != nil {
		return m.reply(msg), nil
	}
	return &socket.UsageFlowSocketResponse{Type: "ok"}, nil
}

// Send is the fire-and-forget path used for settles: it returns once the write "succeeds" (or
// the injected sendErr fires) without waiting for a reply.
func (m *mockSocket) Send(msg *socket.UsageFlowSocketMessage) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.sentFF = append(m.sentFF, msg)
	m.mu.Unlock()
	return nil
}

func (m *mockSocket) Destroy() {}

func (m *mockSocket) types() []string {
	var t []string
	for _, s := range m.sent {
		t = append(t, s.Type)
	}
	return t
}

func openAIServer(t *testing.T, hits *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"gpt-4o-mini-2024","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
}

func TestChatReserveDispatchSettle(t *testing.T) {
	hits := 0
	srv := openAIServer(t, &hits)
	defer srv.Close()
	sock := &mockSocket{}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)

	res, err := c.Chat(context.Background(), ChatRequest{
		Identity: "cust", Provider: ProviderOpenAI, Model: "gpt-4o-mini", Workflow: "wf",
		Messages:         []Message{{Role: "user", Content: "hello"}},
		CustomerMetadata: map[string]any{"plan": "pro", "bad": map[string]any{"x": 1}},
	})
	require.NoError(t, err)
	assert.Equal(t, "hi", res.Content)
	assert.Equal(t, "gpt-4o-mini-2024", res.Model)
	assert.Equal(t, "gpt-4o-mini", res.RequestedModel)
	assert.Equal(t, []string{"request_for_allocation", "use_allocation"}, sock.types())

	reserve := sock.sent[0].Payload.(map[string]any)
	assert.Equal(t, "cust", reserve["alias"])
	assert.Equal(t, map[string]any{"plan": "pro"}, reserve["customerMetadata"])
	assert.Equal(t, map[string]any{"workflowId": "wf", "model": "gpt-4o-mini", "requestedModel": "gpt-4o-mini"}, reserve["allocationMetadata"])
	assert.Greater(t, reserve["amount"].(float64), float64(1024)) // est input + default cap

	settle := sock.sent[1].Payload.(map[string]any)
	assert.Equal(t, float64(15), settle["amount"])
	assert.Equal(t, false, settle["waitForConfirmation"])
	assert.Equal(t, "vibe:chat:openai:gpt-4o-mini-2024", settle["metadata"].(map[string]any)["url"])
}

func TestChatDenialSkipsProvider(t *testing.T) {
	hits := 0
	srv := openAIServer(t, &hits)
	defer srv.Close()
	sock := &mockSocket{reply: func(*socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		return &socket.UsageFlowSocketResponse{Type: "error", Message: "over quota", Error: "quota_exceeded"}
	}}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)

	_, err := c.Chat(context.Background(), ChatRequest{Identity: "c", Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "x"}}})
	var rej *RejectionError
	require.True(t, errors.As(err, &rej))
	assert.Equal(t, "quota_exceeded", rej.Reason)
	assert.Zero(t, hits)
	// A denied reserve never dispatches, so no settle is sent.
	assert.Equal(t, []string{"request_for_allocation"}, sock.types())
}

// TestChatSucceedsWhenSettleSendFails: the settle is fire-and-forget. If it can't even be
// sent, Chat still succeeds — the provider call already happened and its result is real.
func TestChatSucceedsWhenSettleSendFails(t *testing.T) {
	hits := 0
	srv := openAIServer(t, &hits)
	defer srv.Close()
	sock := &mockSocket{sendErr: errors.New("boom")}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)

	res, err := c.Chat(context.Background(), ChatRequest{
		Identity: "c", Model: "gpt-4o-mini", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "hi", res.Content)
	// The reserve went through; the settle attempt failed to send and wasn't recorded.
	assert.Equal(t, []string{"request_for_allocation"}, sock.types())
	assert.Empty(t, sock.sentFF)
}

// TestWithdrawCreditCloseErrorWhenSettleSendFails: for money moves, a failed settle SEND
// (couldn't reach UsageFlow) must be returned as an error rather than logged.
func TestWithdrawCreditCloseErrorWhenSettleSendFails(t *testing.T) {
	sendErr := errors.New("boom")
	sock := &mockSocket{sendErr: sendErr}
	c := newWithTransport(Options{}, sock)
	ctx := context.Background()

	_, err := c.Withdraw(ctx, WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k1"})
	require.Error(t, err)

	_, err = c.Credit(ctx, WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k2"})
	require.Error(t, err)

	// WithdrawAsync's reserve uses SendAsync, unaffected by sendErr; only the later Close
	// settle (fire-and-forget) fails to send.
	cap, err := c.WithdrawAsync(ctx, WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k3"})
	require.NoError(t, err)
	_, err = c.Close(ctx, CloseRequest{CaptureID: cap.CaptureID})
	require.Error(t, err)

	// The capture is only dropped once the settle send succeeds, so it's still there to retry.
	c.mu.Lock()
	_, held := c.captures[cap.CaptureID]
	c.mu.Unlock()
	assert.True(t, held)
}

func TestPolicyRoutesModelAcrossProviders(t *testing.T) {
	hits := 0
	srv := openAIServer(t, &hits)
	defer srv.Close()
	sock := &mockSocket{reply: func(m *socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		if m.Type == "request_for_allocation" {
			return &socket.UsageFlowSocketResponse{Type: "ok", Payload: map[string]any{
				"vibePolicy": map[string]any{"policyId": "p1", "tierIndex": 2,
					"action": map[string]any{"type": "DEGRADE", "params": map[string]any{"model": "gpt-4o-mini"}}},
			}}
		}
		return &socket.UsageFlowSocketResponse{Type: "ok"}
	}}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)

	res, err := c.Chat(context.Background(), ChatRequest{
		Identity: "c", Provider: ProviderAnthropic, Model: "claude-sonnet-5",
		Messages: []Message{{Role: "user", Content: "x"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, hits) // rerouted to OpenAI
	assert.Equal(t, ProviderOpenAI, res.Provider)
	assert.Equal(t, "claude-sonnet-5", res.RequestedModel)
	require.NotNil(t, res.VibePolicy)
	assert.Equal(t, "p1", res.VibePolicy.PolicyID)
}

func TestStreamAnthropicSettlesAfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`{"type":"message_start","message":{"model":"claude-sonnet-5","usage":{"input_tokens":7,"output_tokens":1}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
			`{"type":"message_delta","usage":{"output_tokens":4}}`,
		} {
			fmt.Fprintf(w, "event: x\ndata: %s\n\n", ev)
		}
	}))
	defer srv.Close()
	sock := &mockSocket{}
	c := newWithTransport(Options{AnthropicAPIKey: "k", AnthropicBaseURL: srv.URL}, sock)

	s, err := c.Stream(context.Background(), ChatRequest{Identity: "c", Model: "claude-sonnet-5", Messages: []Message{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	got := ""
	for d := range s.Text {
		got += d
	}
	res, err := s.Result()
	require.NoError(t, err)
	assert.Equal(t, "Hello", got)
	assert.Equal(t, Usage{InputTokens: 7, OutputTokens: 4}, res.Usage)
	assert.Equal(t, []string{"request_for_allocation", "use_allocation"}, sock.types())
	assert.Equal(t, float64(11), sock.sent[1].Payload.(map[string]any)["amount"])
}

func TestWithdrawAndCredit(t *testing.T) {
	sock := &mockSocket{}
	c := newWithTransport(Options{}, sock)
	res, err := c.Withdraw(context.Background(), WithdrawRequest{Identity: "c", Amount: 500, IdempotencyKey: "k1"})
	require.NoError(t, err)
	assert.NotEmpty(t, res.EventID)
	_, err = c.Credit(context.Background(), WithdrawRequest{Identity: "c", Amount: 500, IdempotencyKey: "k2"})
	require.NoError(t, err)
	assert.Equal(t, float64(500), sock.sent[0].Payload.(map[string]any)["amount"])
	assert.Equal(t, float64(-500), sock.sent[2].Payload.(map[string]any)["amount"])

	_, err = c.Withdraw(context.Background(), WithdrawRequest{Identity: "c", Amount: 1})
	assert.Error(t, err) // idempotency key required
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model":"text-embedding-3-small","data":[{"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3}}`)
	}))
	defer srv.Close()
	sock := &mockSocket{}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)
	res, err := c.Embed(context.Background(), EmbedRequest{Identity: "c", Model: "text-embedding-3-small", Input: []string{"hi"}})
	require.NoError(t, err)
	assert.Equal(t, [][]float64{{0.1, 0.2}}, res.Embeddings)
	assert.Equal(t, float64(3), sock.sent[1].Payload.(map[string]any)["amount"])
}

func TestReasoningModelBody(t *testing.T) {
	temp := 0.5
	b := openAIBody(&ChatRequest{Model: "gpt-5", Temperature: &temp}, 100, false)
	assert.Contains(t, b, "max_completion_tokens")
	assert.NotContains(t, b, "max_tokens")
	assert.NotContains(t, b, "temperature")
	b = openAIBody(&ChatRequest{Model: "gpt-4o", Temperature: &temp}, 100, false)
	assert.Contains(t, b, "max_tokens")
	assert.Contains(t, b, "temperature")
}

func TestWithdrawAsyncThenClose(t *testing.T) {
	sock := &mockSocket{reply: func(m *socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		return &socket.UsageFlowSocketResponse{Type: "ok", Payload: map[string]any{"allocationId": "alloc-9"}}
	}}
	c := newWithTransport(Options{}, sock)
	ctx := context.Background()

	cap, err := c.WithdrawAsync(ctx, WithdrawRequest{Identity: "cust", Amount: 100, IdempotencyKey: "i1", Workflow: "wf-1"})
	require.NoError(t, err)
	assert.Equal(t, "alloc-9", cap.CaptureID)
	assert.Equal(t, []string{"request_for_allocation"}, sock.types())
	assert.Equal(t, map[string]any{"workflowId": "wf-1"}, sock.sent[0].Payload.(map[string]any)["allocationMetadata"])

	final := 60.0
	res, err := c.Close(ctx, CloseRequest{CaptureID: "alloc-9", Amount: &final})
	require.NoError(t, err)
	assert.Equal(t, "alloc-9", res.EventID)
	settle := sock.sent[1].Payload.(map[string]any)
	assert.Equal(t, "use_allocation", sock.sent[1].Type)
	assert.Equal(t, float64(60), settle["amount"])
	assert.Equal(t, "cust", settle["alias"])
	assert.Equal(t, false, settle["waitForConfirmation"])

	_, err = c.Close(ctx, CloseRequest{CaptureID: "alloc-9"})
	assert.Error(t, err) // closes only once

	// Foreign-process close needs identity; CreditAsync reserves negative.
	_, err = c.Close(ctx, CloseRequest{CaptureID: "remote", Identity: "cust", Amount: &final})
	require.NoError(t, err)
	cc, err := c.CreditAsync(ctx, WithdrawRequest{Identity: "cust", Amount: 7, IdempotencyKey: "i2"})
	require.NoError(t, err)
	assert.Equal(t, float64(-7), cc.Amount)
}

func TestReserveAndSettleShareUsageflowRequestID(t *testing.T) {
	sock := &mockSocket{}
	c := newWithTransport(Options{}, sock)
	cp, err := c.WithdrawAsync(context.Background(), WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k"})
	require.NoError(t, err)
	_, err = c.Close(context.Background(), CloseRequest{CaptureID: cp.CaptureID})
	require.NoError(t, err)
	reserveMeta := sock.sent[0].Payload.(map[string]any)["metadata"].(map[string]any)
	settleMeta := sock.sent[1].Payload.(map[string]any)["metadata"].(map[string]any)
	assert.NotEmpty(t, reserveMeta["usageflowRequestId"])
	assert.Equal(t, reserveMeta["usageflowRequestId"], settleMeta["usageflowRequestId"])

	// Closing a capture held by another process still stamps an ID (the capture ID).
	_, err = c.Close(context.Background(), CloseRequest{CaptureID: "remote-1", Identity: "c"})
	require.NoError(t, err)
	remote := sock.sent[2].Payload.(map[string]any)["metadata"].(map[string]any)
	assert.Equal(t, "remote-1", remote["usageflowRequestId"])
}

func TestCredits(t *testing.T) {
	sock := &mockSocket{reply: func(m *socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		return &socket.UsageFlowSocketResponse{Type: "success", Payload: map[string]any{
			"account":  map[string]any{"planId": "free", "used": 10, "limit": 1000, "remaining": 990, "exceeded": false},
			"identity": map[string]any{"identity": "203.0.113.1", "used": 4, "known": true},
			"workflows": []any{map[string]any{
				"workflow": "free-landing-page", "policyId": "vpol_1", "limit": 5, "used": 4, "remaining": 1,
				"blocked": false, "resetInterval": "1d",
				"nextTier": map[string]any{"tierIndex": 0, "thresholdValue": 5, "remaining": 1, "action": map[string]any{"type": "BLOCK"}},
			}},
		}}
	}}
	c := newWithTransport(Options{}, sock)

	res, err := c.Credits(context.Background(), CreditsRequest{
		Identity: "203.0.113.1", Workflow: "free-landing-page",
		CustomerMetadata: map[string]any{"plan": "free", "bad": map[string]any{"x": 1}},
	})
	require.NoError(t, err)

	sent := sock.sent[0]
	assert.Equal(t, "get_credits", sent.Type)
	p := sent.Payload.(map[string]any)
	assert.Equal(t, "203.0.113.1", p["identity"])
	assert.Equal(t, "free-landing-page", p["workflow"])
	assert.Equal(t, map[string]any{"plan": "free"}, p["customerMetadata"])

	require.NotNil(t, res.Account)
	assert.Equal(t, int64(990), *res.Account.Remaining)
	assert.Equal(t, 4.0, res.Identity.Used)
	require.Len(t, res.Workflows, 1)
	w := res.Workflows[0]
	assert.Equal(t, 1.0, *w.Remaining)
	assert.Equal(t, "1d", w.ResetInterval)
	assert.Equal(t, "BLOCK", w.NextTier.Action.Type)
}

func TestCreditsErrors(t *testing.T) {
	c := newWithTransport(Options{}, &mockSocket{})
	_, err := c.Credits(context.Background(), CreditsRequest{})
	assert.Error(t, err) // identity required

	old := newWithTransport(Options{}, &mockSocket{reply: func(*socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		return &socket.UsageFlowSocketResponse{Type: "error", Error: "No handler registered for type: get_credits"}
	}})
	_, err = old.Credits(context.Background(), CreditsRequest{Identity: "u"})
	assert.ErrorIs(t, err, ErrCreditsUnsupported)

	denied := newWithTransport(Options{}, &mockSocket{reply: func(*socket.UsageFlowSocketMessage) *socket.UsageFlowSocketResponse {
		return &socket.UsageFlowSocketResponse{Type: "error", Error: "workflow not found"}
	}})
	_, err = denied.Credits(context.Background(), CreditsRequest{Identity: "u", Workflow: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not found")
	assert.NotErrorIs(t, err, ErrCreditsUnsupported)
}
