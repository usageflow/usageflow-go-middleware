package vibe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
)

// defaultMaxOutputTokens is enforced when the caller doesn't set MaxTokens — it must be a
// real ceiling, since it is part of the reservation.
const defaultMaxOutputTokens = 1024

// How long UsageFlow holds a reservation before it expires unsettled. Without an explicit value
// the server uses 60 s, and an expired reservation is later closed at zero — so a settle that
// arrives after that is lost and the usage is never charged. Every reserve sends one of these.
const (
	callHold           = 10 * time.Minute // Chat, Stream, Embed, Withdraw, Credit
	defaultCaptureHold = 24 * time.Hour   // WithdrawAsync / CreditAsync unless HoldFor is set
)

// transport is the slice of the UsageFlow socket the client needs (mockable in tests).
type transport interface {
	SendAsync(*socket.UsageFlowSocketMessage) (*socket.UsageFlowSocketResponse, error)
	// Send is fire-and-forget: it returns once the write succeeds (or fails), without
	// waiting for a reply. Used for settles, which the ledger applies asynchronously.
	Send(*socket.UsageFlowSocketMessage) error
	Destroy()
}

// Client meters LLM calls through UsageFlow: reserve → dispatch → settle.
type Client struct {
	opts Options
	sock transport
	http *http.Client

	mu       sync.Mutex
	captures map[string]pendingCapture // held by WithdrawAsync until Close
}

type pendingCapture struct {
	r              *reservation
	idempotencyKey string
}

// New creates a Client and opens the pooled UsageFlow socket connection.
func New(opts Options) (*Client, error) {
	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("USAGEFLOW_API_KEY")
	}
	if opts.APIKey == "" {
		return nil, errors.New("usageflow vibe: APIKey is required (or set USAGEFLOW_API_KEY)")
	}
	if opts.WSURL == "" {
		opts.WSURL = os.Getenv("USAGEFLOW_WS_URL")
	}
	var pool []int
	if opts.PoolSize > 0 {
		pool = []int{opts.PoolSize}
	}
	mgr := socket.NewUsageFlowSocketManagerWithURL(opts.APIKey, opts.WSURL, pool...)
	// The pool swallows dial errors and retries in the background, so surface a bad
	// key/URL up front instead of failing later with "WebSocket not connected".
	if os.Getenv("USAGEFLOW_DISABLE_WS") != "1" && !mgr.IsConnected() {
		log.Printf("usageflow vibe: WARNING: not connected to UsageFlow socket (%s) — check APIKey and WSURL/USAGEFLOW_WS_URL; retrying in background",
			or(opts.WSURL, "wss://api.usageflow.io/ws"))
	}
	return newWithTransport(opts, mgr), nil
}

func newWithTransport(opts Options, t transport) *Client {
	if opts.AnthropicAPIKey == "" {
		opts.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if opts.OpenAIAPIKey == "" {
		opts.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	}
	if opts.AnthropicBaseURL == "" {
		opts.AnthropicBaseURL = defaultAnthropicURL
	}
	if opts.OpenAIBaseURL == "" {
		opts.OpenAIBaseURL = defaultOpenAIURL
	}
	return &Client{opts: opts, sock: t, http: &http.Client{Timeout: 5 * time.Minute}, captures: map[string]pendingCapture{}}
}

// Destroy tears down the socket pool.
func (c *Client) Destroy() { c.sock.Destroy() }

// sanitizeMetadata keeps only string/number/bool values (no nested objects); nil if none remain.
func sanitizeMetadata(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		switch v.(type) {
		case string, bool, int, int32, int64, float32, float64:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// estimateTokens is a conservative worst-case estimate: chars/3 + 4 per item.
func estimateTokens(items ...string) int {
	total := 0
	for _, s := range items {
		total += (len([]rune(s))+2)/3 + 4
	}
	return total
}

func mergeMap(base, extra map[string]any) map[string]any {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

type allocationResponse struct {
	AllocationID string          `json:"allocationId"`
	VibePolicy   *PolicyDecision `json:"vibePolicy"`
}

// send performs one socket round trip and maps an error response to *RejectionError.
func (c *Client) send(msgType string, payload map[string]any, fallback string) (*allocationResponse, error) {
	resp, err := c.sock.SendAsync(&socket.UsageFlowSocketMessage{Type: msgType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("usageflow vibe: %s: %w", msgType, err)
	}
	if resp.Type == "error" {
		msg := resp.Message
		if msg == "" {
			msg = resp.Error
		}
		if msg == "" {
			msg = fallback
		}
		return nil, &RejectionError{Message: msg, Reason: resp.Error}
	}
	out := &allocationResponse{}
	if resp.Payload != nil {
		b, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(b, out)
	}
	return out, nil
}

type reservation struct {
	payload      map[string]any
	allocationID string
	policy       *PolicyDecision
	// overrideModel is the model a ROUTE_MODEL/DEGRADE tier redirected to ("" if none).
	overrideModel string
}

// reserve sends request_for_allocation. Extra fields (allocationMetadata etc.) are added by the caller.
func (c *Client) reserve(identity string, amount float64, workflow, model string, metadata, customerMeta map[string]any, hold time.Duration) (*reservation, error) {
	allocationID := uuid.NewString()
	// One request ID per metered operation, on both the reserve and the settle (which copies
	// this metadata). The ledger counts calls by usageflowRequestId and the Console merges a
	// reserve and its close into one row by it; without it every event stands alone.
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, ok := metadata["usageflowRequestId"]; !ok {
		metadata["usageflowRequestId"] = allocationID
	}
	payload := map[string]any{
		"alias":        identity,
		"amount":       amount,
		"allocationId": allocationID,
		"metadata":     metadata,
		"duration":     hold.Milliseconds(),
	}
	if workflow != "" || model != "" {
		am := map[string]any{}
		if workflow != "" {
			am["workflowId"] = workflow
		}
		if model != "" {
			am["model"] = model
			am["requestedModel"] = model
		}
		payload["allocationMetadata"] = am
	}
	if cm := sanitizeMetadata(customerMeta); cm != nil {
		payload["customerMetadata"] = cm
	}
	out, err := c.send("request_for_allocation", payload, "Request rejected by UsageFlow policy")
	if err != nil {
		return nil, err
	}
	r := &reservation{payload: payload, allocationID: allocationID, policy: out.VibePolicy}
	if out.AllocationID != "" {
		r.allocationID = out.AllocationID
	}
	if r.policy != nil {
		// A matched tier's model param is authoritative, not a suggestion.
		if m, ok := r.policy.Action.Params["model"].(string); ok && m != "" {
			r.overrideModel = m
		}
	}
	return r, nil
}

// settle sends use_allocation fire-and-forget (waitForConfirmation: false) and does not wait
// for a reply — the ledger applies the settlement asynchronously. The returned error means
// only that the send itself could not be made (socket not connected / write failed); once the
// send succeeds, a server-side rejection of the settle can no longer be observed here.
func (c *Client) settle(r *reservation, amount float64, extraMeta map[string]any) error {
	payload := map[string]any{}
	for k, v := range r.payload {
		payload[k] = v
	}
	delete(payload, "duration") // reserve-only
	md := map[string]any{}
	for k, v := range r.payload["metadata"].(map[string]any) {
		md[k] = v
	}
	payload["metadata"] = mergeMap(md, extraMeta)
	payload["amount"] = amount
	payload["allocationId"] = r.allocationID
	payload["waitForConfirmation"] = false
	if err := c.sock.Send(&socket.UsageFlowSocketMessage{Type: "use_allocation", Payload: payload}); err != nil {
		return fmt.Errorf("usageflow vibe: use_allocation: %w", err)
	}
	return nil
}

func baseMetadata(method, url, provider, model string, body map[string]any) map[string]any {
	return map[string]any{
		"type":           "API_CALL",
		"method":         method,
		"url":            url,
		"rawUrl":         url,
		"model":          model,
		"requestedModel": model,
		"provider":       provider,
		"clientIP":       "internal",
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
		"headers":        map[string]any{},
		"queryParams":    nil,
		"pathParams":     nil,
		"body":           body,
	}
}

type chatReservation struct {
	*reservation
	effective *ChatRequest
	maxTokens int
}

func (c *Client) reserveChat(req *ChatRequest) (*chatReservation, error) {
	if req.Identity == "" || req.Model == "" || len(req.Messages) == 0 {
		return nil, errors.New("usageflow vibe: Identity, Model and Messages are required")
	}
	if req.Provider == "" {
		req.Provider = inferProvider(req.Model)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxOutputTokens
	}
	contents := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		contents[i] = m.Content
	}
	est := estimateTokens(contents...)
	url := fmt.Sprintf("vibe:chat:%s:%s", req.Provider, req.Model)
	body := mergeMap(map[string]any{
		"identity":             req.Identity,
		"messageCount":         len(req.Messages),
		"estimatedInputTokens": est,
		"effectiveMaxTokens":   maxTokens,
	}, req.Metadata)
	meta := baseMetadata("CHAT", url, string(req.Provider), req.Model, body)

	r, err := c.reserve(req.Identity, float64(est+maxTokens), req.Workflow, req.Model, meta, req.CustomerMetadata, callHold)
	if err != nil {
		return nil, err
	}
	eff := *req
	if r.overrideModel != "" {
		eff.Model = r.overrideModel
		eff.Provider = inferProvider(r.overrideModel)
	}
	return &chatReservation{reservation: r, effective: &eff, maxTokens: maxTokens}, nil
}

// settleChat settles with real usage, fire-and-forget. A failure to even send is logged, not
// returned: the provider call already happened and its result is real. Because the settle is
// fire-and-forget, a server-side rejection of it can no longer be observed here.
func (c *Client) settleChat(orig *ChatRequest, cr *chatReservation, res *ChatResult, start time.Time, method string) {
	res.RequestedModel = orig.Model
	res.VibePolicy = cr.policy
	actual := res.Usage.InputTokens + res.Usage.OutputTokens
	url := fmt.Sprintf("vibe:%s:%s:%s", method, cr.effective.Provider, res.Model)
	err := c.settle(cr.reservation, float64(actual), map[string]any{
		"url": url, "rawUrl": url,
		"model": res.Model, "requestedModel": orig.Model, "provider": string(cr.effective.Provider),
		"requestDuration": time.Since(start).Milliseconds(),
		"functionCalls": []any{map[string]any{
			"funcName": fmt.Sprintf("%s.%s", cr.effective.Provider, method),
			"filePath": "usageflow-go-middleware/pkg/vibe",
			"usage": map[string]any{
				"prompt_tokens":     res.Usage.InputTokens,
				"completion_tokens": res.Usage.OutputTokens,
				"total_tokens":      actual,
			},
		}},
	})
	if err != nil {
		log.Printf("usageflow vibe: %s() settlement failed for allocation %s: %v", method, cr.allocationID, err)
	}
}

// Chat reserves quota, dispatches to the provider (following any Vibe policy routing), and
// settles real usage. A denial returns *RejectionError and the provider is never called.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	start := time.Now()
	cr, err := c.reserveChat(&req)
	if err != nil {
		return nil, err
	}
	res, err := c.dispatch(ctx, cr.effective, cr.maxTokens)
	if err != nil {
		return nil, err
	}
	c.settleChat(&req, cr, res, start, "chat")
	return res, nil
}

// ChatStream is the streaming counterpart of Chat. Text arrives on Text; Text is closed when
// the provider stream ends and settlement is done, after which Result returns the final value.
type ChatStream struct {
	Text   <-chan string
	done   chan struct{}
	result *ChatResult
	err    error
}

// Result blocks until the stream has finished (Text drained) and returns the final result.
func (s *ChatStream) Result() (*ChatResult, error) {
	<-s.done
	return s.result, s.err
}

// Stream reserves up front (same denial behavior as Chat) and defers settlement until the
// provider stream closes, since usage isn't known until then. Text must be drained.
func (c *Client) Stream(ctx context.Context, req ChatRequest) (*ChatStream, error) {
	start := time.Now()
	cr, err := c.reserveChat(&req)
	if err != nil {
		return nil, err
	}
	text := make(chan string, 64)
	s := &ChatStream{Text: text, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(text)
		res, err := c.dispatchStream(ctx, cr.effective, cr.maxTokens, func(d string) {
			select {
			case text <- d:
			case <-ctx.Done():
			}
		})
		if err != nil {
			s.err = err
			return
		}
		c.settleChat(&req, cr, res, start, "stream")
		s.result = res
	}()
	return s, nil
}

// Embed is the OpenAI-only embeddings call, reserved on input tokens only.
func (c *Client) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if req.Identity == "" || req.Model == "" || len(req.Input) == 0 {
		return nil, errors.New("usageflow vibe: Identity, Model and Input are required")
	}
	start := time.Now()
	est := estimateTokens(req.Input...)
	url := "vibe:embed:openai:" + req.Model
	body := mergeMap(map[string]any{
		"identity": req.Identity, "inputCount": len(req.Input), "estimatedInputTokens": est,
	}, req.Metadata)
	meta := baseMetadata("EMBED", url, "openai", req.Model, body)
	r, err := c.reserve(req.Identity, float64(est), req.Workflow, req.Model, meta, req.CustomerMetadata, callHold)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if r.overrideModel != "" {
		model = r.overrideModel
	}
	res, err := c.dispatchEmbedding(ctx, model, req.Input)
	if err != nil {
		return nil, err
	}
	res.RequestedModel = req.Model
	res.VibePolicy = r.policy
	settledURL := "vibe:embed:openai:" + res.Model
	if err := c.settle(r, float64(res.Usage.InputTokens), map[string]any{
		"url": settledURL, "rawUrl": settledURL, "model": res.Model, "requestedModel": req.Model,
		"provider": "openai", "requestDuration": time.Since(start).Milliseconds(),
		"functionCalls": []any{map[string]any{
			"funcName": "openai.embed", "filePath": "usageflow-go-middleware/pkg/vibe",
			"usage": map[string]any{
				"prompt_tokens": res.Usage.InputTokens, "completion_tokens": 0, "total_tokens": res.Usage.InputTokens,
			},
		}},
	}); err != nil {
		// Fire-and-forget: this only means the settle couldn't even be sent (socket down /
		// write failed). A server-side rejection of it can no longer be observed here.
		log.Printf("usageflow vibe: embed() settlement failed for allocation %s: %v", r.allocationID, err)
	}
	return res, nil
}

// Withdraw deducts Amount from Identity outside a metered call (reserve + immediate settle).
// The settle is sent fire-and-forget; Withdraw returns once it has been sent, not once the
// ledger has applied it. The returned error means only that the send failed (couldn't reach
// UsageFlow) — a server-side rejection of the settle can no longer be observed here.
func (c *Client) Withdraw(_ context.Context, req WithdrawRequest) (*WithdrawResult, error) {
	return c.reserveAndSettle(req, req.Amount, "withdraw")
}

// Credit reverses/corrects a prior withdrawal by sending the amount as negative through the
// same reserve/settle pair. Negative-amount behavior is unverified against server policy. Like
// Withdraw, the settle is fire-and-forget: Credit returns once it has been sent, and a
// server-side rejection of it can no longer be observed here.
func (c *Client) Credit(_ context.Context, req WithdrawRequest) (*WithdrawResult, error) {
	return c.reserveAndSettle(req, negative(req.Amount), "credit")
}

func negative(a float64) float64 {
	if a > 0 {
		return -a
	}
	return a
}

func (c *Client) reserveWithdraw(req WithdrawRequest, signed float64, action string, hold time.Duration) (*reservation, error) {
	if req.Identity == "" || req.IdempotencyKey == "" {
		return nil, errors.New("usageflow vibe: Identity and IdempotencyKey are required")
	}
	unit := req.Unit
	if unit == "" {
		unit = "credits"
	}
	url := fmt.Sprintf("vibe:%s:%s", action, req.Identity)
	meta := map[string]any{
		"type": "API_CALL", "method": upper(action), "url": url, "rawUrl": url,
		"clientIP": "internal", "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"headers": map[string]any{}, "queryParams": nil, "pathParams": nil,
		"body": map[string]any{
			"identity": req.Identity, "unit": unit, "idempotencyKey": req.IdempotencyKey,
			"reason": req.Reason, "sourceEventType": req.SourceEventType,
		},
	}
	return c.reserve(req.Identity, signed, req.Workflow, "", meta, req.CustomerMetadata, hold)
}

func (c *Client) reserveAndSettle(req WithdrawRequest, signed float64, action string) (*WithdrawResult, error) {
	r, err := c.reserveWithdraw(req, signed, action, callHold)
	if err != nil {
		return nil, err
	}
	// The amount is already final, so settle the same amount fire-and-forget; a failure to
	// send means the deduction was never even dispatched, so it is surfaced rather than
	// logged (money moves must fail loudly when they could not be sent).
	if err := c.settle(r, signed, nil); err != nil {
		return nil, err
	}
	return &WithdrawResult{Identity: req.Identity, Amount: req.Amount, IdempotencyKey: req.IdempotencyKey, EventID: r.allocationID}, nil
}

// WithdrawAsync reserves now and settles later: it runs the policy check and holds the
// quota, returning a CaptureID for Close. Nothing is deducted until Close.
func (c *Client) WithdrawAsync(_ context.Context, req WithdrawRequest) (*CaptureResult, error) {
	return c.reserveCapture(req, req.Amount, "withdraw")
}

// CreditAsync is the async counterpart of Credit (reserves a negative amount).
func (c *Client) CreditAsync(_ context.Context, req WithdrawRequest) (*CaptureResult, error) {
	return c.reserveCapture(req, negative(req.Amount), "credit")
}

func (c *Client) reserveCapture(req WithdrawRequest, signed float64, action string) (*CaptureResult, error) {
	hold := req.HoldFor
	if hold == 0 {
		hold = defaultCaptureHold
	}
	if hold < 0 {
		return nil, errors.New("usageflow vibe: HoldFor must be positive")
	}
	r, err := c.reserveWithdraw(req, signed, action, hold)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.captures[r.allocationID] = pendingCapture{r: r, idempotencyKey: req.IdempotencyKey}
	c.mu.Unlock()
	return &CaptureResult{
		CaptureID: r.allocationID, Identity: req.Identity, Amount: signed, IdempotencyKey: req.IdempotencyKey,
		ExpiresAt: time.Now().Add(hold).UnixMilli(),
	}, nil
}

// Close settles a capture from WithdrawAsync/CreditAsync. Amount defaults to the reserved
// amount. If the capture isn't held by this process, Identity is required. A capture can
// only be closed once. The settle is sent fire-and-forget: Close returns once it has been
// sent, not once the ledger has applied it, and a server-side rejection of it can no longer
// be observed here. The capture is only removed from local tracking once the send succeeds —
// a failed send leaves it eligible to Close again.
func (c *Client) Close(_ context.Context, req CloseRequest) (*WithdrawResult, error) {
	if req.CaptureID == "" {
		return nil, errors.New("usageflow vibe: CaptureID is required")
	}
	c.mu.Lock()
	p, held := c.captures[req.CaptureID]
	c.mu.Unlock()

	r := p.r
	if !held {
		if req.Identity == "" {
			return nil, fmt.Errorf("usageflow vibe: capture %s is not held by this process — pass Identity", req.CaptureID)
		}
		amt := 0.0
		if req.Amount != nil {
			amt = *req.Amount
		}
		// A negative amount closes a CreditAsync hold; label it so the audit log says so.
		action := "withdraw"
		if amt < 0 {
			action = "credit"
		}
		payload := map[string]any{
			"alias": req.Identity, "amount": amt, "allocationId": req.CaptureID,
			"metadata": map[string]any{
				"type": "API_CALL", "method": upper(action), "url": "vibe:" + action + ":" + req.Identity,
				"rawUrl": "vibe:" + action + ":" + req.Identity, "clientIP": "internal",
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				"headers":   map[string]any{}, "queryParams": nil, "pathParams": nil,
				"usageflowRequestId": req.CaptureID,
				"body":               map[string]any{"identity": req.Identity, "idempotencyKey": req.CaptureID},
			},
		}
		if req.Workflow != "" {
			payload["allocationMetadata"] = map[string]any{"workflowId": req.Workflow}
		}
		if cm := sanitizeMetadata(req.CustomerMetadata); cm != nil {
			payload["customerMetadata"] = cm
		}
		r = &reservation{payload: payload, allocationID: req.CaptureID}
	}
	amount := r.payload["amount"].(float64)
	if req.Amount != nil {
		amount = *req.Amount
	}
	if err := c.settle(r, amount, nil); err != nil {
		return nil, err
	}
	c.mu.Lock()
	delete(c.captures, req.CaptureID)
	c.mu.Unlock()
	key := p.idempotencyKey
	if key == "" {
		key = req.CaptureID
	}
	return &WithdrawResult{Identity: r.payload["alias"].(string), Amount: amount, IdempotencyKey: key, EventID: req.CaptureID}, nil
}

func upper(s string) string {
	b := []byte(s)
	for i, ch := range b {
		if ch >= 'a' && ch <= 'z' {
			b[i] = ch - 32
		}
	}
	return string(b)
}

func or(v, d string) string {
	if v != "" {
		return v
	}
	return d
}
