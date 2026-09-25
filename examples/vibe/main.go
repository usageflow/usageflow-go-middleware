// Vibe example: the Go counterpart of agents/js/examples/vibe-app. Same routes, same
// defaults, so the curl commands in the JS README work here (default port 4003).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/usageflow/usageflow-go-middleware/v2/pkg/vibe"
)

var client *vibe.Client

// body is the union of fields the example routes accept; all optional.
type body struct {
	Prompt         string   `json:"prompt"`
	Identity       string   `json:"identity"`
	Workflow       string   `json:"workflow"`
	Plan           string   `json:"plan"`
	Amount         *float64 `json:"amount"`
	IdempotencyKey string   `json:"idempotencyKey"`
	Reason         string   `json:"reason"`
	CaptureID      string   `json:"captureId"`
	HoldFor        float64  `json:"holdFor"` // seconds; withdraw-async/credit-async only (default 24h)
	Input          []string `json:"input"`
}

func or(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

func (b body) amount() float64 {
	if b.Amount != nil {
		return *b.Amount
	}
	return 50
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleErr(w http.ResponseWriter, err error) {
	var rej *vibe.RejectionError
	if errors.As(err, &rej) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "rejected", "message": rej.Message, "reason": rej.Reason})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// post wraps a handler: POST-only, decodes the optional JSON body, logs the request.
func post(fn func(ctx context.Context, b body) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
			return
		}
		var b body
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
				return
			}
		}
		out, err := fn(r.Context(), b)
		if err != nil {
			handleErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func chatReq(b body, provider vibe.Provider, model, defWorkflow, defIdentity string) vibe.ChatRequest {
	req := vibe.ChatRequest{
		Provider: provider,
		Model:    model,
		Workflow: or(b.Workflow, defWorkflow),
		Identity: or(b.Identity, defIdentity),
		Messages: []vibe.Message{{Role: "user", Content: or(b.Prompt, "Say hello in 5 words")}},
	}
	// Same defaults as the JS example: OpenAI chat → Enterprise, Anthropic → Free.
	plan := b.Plan
	if plan == "" {
		plan = map[vibe.Provider]string{vibe.ProviderOpenAI: "Enterprise", vibe.ProviderAnthropic: "Free"}[provider]
	}
	req.CustomerMetadata = map[string]any{"plan": plan}
	return req
}

func chat(provider vibe.Provider, model, workflow, defIdentity string) http.HandlerFunc {
	return post(func(ctx context.Context, b body) (any, error) {
		return client.Chat(ctx, chatReq(b, provider, model, workflow, defIdentity))
	})
}

// stream collects the deltas server-side and returns them with the settled result (plain
// JSON, no SSE — same as the JS example).
func stream(provider vibe.Provider, model, workflow string) http.HandlerFunc {
	return post(func(ctx context.Context, b body) (any, error) {
		s, err := client.Stream(ctx, chatReq(b, provider, model, workflow, "example-user"))
		if err != nil {
			return nil, err
		}
		chunks := []string{}
		for c := range s.Text {
			chunks = append(chunks, c)
		}
		res, err := s.Result()
		if err != nil {
			return nil, err
		}
		return map[string]any{"chunks": chunks, "result": res}, nil
	})
}

func withdrawReq(b body) vibe.WithdrawRequest {
	return vibe.WithdrawRequest{
		Identity:       or(b.Identity, "cust_acme"),
		Amount:         b.amount(),
		IdempotencyKey: or(b.IdempotencyKey, "demo-1"),
		Reason:         or(b.Reason, "manual test"),
		Workflow:       b.Workflow,
		HoldFor:        time.Duration(b.HoldFor * float64(time.Second)),
	}
}

func main() {
	if os.Getenv("USAGEFLOW_API_KEY") == "" {
		log.Fatal("USAGEFLOW_API_KEY is required — set it in examples/vibe/.env (see .env.example)")
	}
	var err error
	client, err = vibe.New(vibe.Options{APIKey: os.Getenv("USAGEFLOW_API_KEY")})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Destroy()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/api/chat/openai", chat(vibe.ProviderOpenAI, "gpt-4o-mini", "vibe-example-openai", "example-user"))
	mux.HandleFunc("/api/chat/anthropic", chat(vibe.ProviderAnthropic, "claude-haiku-4-5-20251001", "vibe-example-anthropic", "free_user"))
	mux.HandleFunc("/api/stream/openai", stream(vibe.ProviderOpenAI, "gpt-4o-mini", "vibe-example-openai-stream"))
	mux.HandleFunc("/api/stream/anthropic", stream(vibe.ProviderAnthropic, "claude-haiku-4-5-20251001", "vibe-example-anthropic-stream"))

	mux.HandleFunc("/api/embed", post(func(ctx context.Context, b body) (any, error) {
		in := b.Input
		if len(in) == 0 {
			in = []string{or(b.Prompt, "hello world")}
		}
		return client.Embed(ctx, vibe.EmbedRequest{
			Identity: or(b.Identity, "example-user"), Model: "text-embedding-3-small", Input: in, Workflow: b.Workflow,
		})
	}))

	// Read-only: account, identity and per-workflow credits (what you would show your users).
	mux.HandleFunc("/api/credits", post(func(ctx context.Context, b body) (any, error) {
		return client.Credits(ctx, vibe.CreditsRequest{Identity: or(b.Identity, "example-user"), Workflow: b.Workflow})
	}))
	mux.HandleFunc("/api/withdraw", post(func(ctx context.Context, b body) (any, error) {
		return client.Withdraw(ctx, withdrawReq(b))
	}))
	mux.HandleFunc("/api/credit", post(func(ctx context.Context, b body) (any, error) {
		return client.Credit(ctx, withdrawReq(b))
	}))
	// Reserve now, settle later: returns { captureId, expiresAt } for /api/close. Optional
	// holdFor (seconds) sets how long the hold stays open; default 24 hours.
	mux.HandleFunc("/api/withdraw-async", post(func(ctx context.Context, b body) (any, error) {
		return client.WithdrawAsync(ctx, withdrawReq(b))
	}))
	mux.HandleFunc("/api/credit-async", post(func(ctx context.Context, b body) (any, error) {
		return client.CreditAsync(ctx, withdrawReq(b))
	}))
	// Settles a capture. amount is optional (defaults to the reserved amount); identity is
	// only needed if the capture came from another process.
	mux.HandleFunc("/api/close", post(func(ctx context.Context, b body) (any, error) {
		return client.Close(ctx, vibe.CloseRequest{
			CaptureID: b.CaptureID, Amount: b.Amount, Identity: b.Identity, Workflow: b.Workflow,
		})
	}))

	port := or(os.Getenv("PORT"), "4003")
	log.Printf("vibe example listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
