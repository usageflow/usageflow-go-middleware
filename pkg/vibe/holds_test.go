package vibe

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every reservation carries an explicit hold so a late settle isn't lost to the server's 60 s
// default; settles never resend it.

func payloadOf(sock *mockSocket, i int) map[string]any {
	return sock.sent[i].Payload.(map[string]any)
}

func assertHolds(t *testing.T, sock *mockSocket, want int64) {
	t.Helper()
	for i, m := range sock.sent {
		p := payloadOf(sock, i)
		if m.Type == "request_for_allocation" {
			assert.Equal(t, want, p["duration"], "reserve %d", i)
		} else {
			assert.NotContains(t, p, "duration", "%s %d", m.Type, i)
		}
	}
}

func TestCallsHoldTenMinutes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			fmt.Fprint(w, `{"model":"e","data":[],"usage":{"prompt_tokens":1}}`)
		default:
			fmt.Fprint(w, `{"model":"gpt-4o-mini","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
		}
	}))
	defer srv.Close()
	sock := &mockSocket{}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)
	ctx := context.Background()
	msgs := []Message{{Role: "user", Content: "hi"}}

	_, err := c.Chat(ctx, ChatRequest{Identity: "c", Model: "gpt-4o-mini", Messages: msgs})
	require.NoError(t, err)
	_, err = c.Embed(ctx, EmbedRequest{Identity: "c", Model: "e", Input: []string{"hi"}})
	require.NoError(t, err)
	_, err = c.Withdraw(ctx, WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k"})
	require.NoError(t, err)
	_, err = c.Credit(ctx, WithdrawRequest{Identity: "c", Amount: 1, IdempotencyKey: "k"})
	require.NoError(t, err)

	assert.Len(t, sock.sent, 8)
	assertHolds(t, sock, (10 * time.Minute).Milliseconds())
}

func TestStreamHoldsTenMinutes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
	}))
	defer srv.Close()
	sock := &mockSocket{}
	c := newWithTransport(Options{OpenAIAPIKey: "k", OpenAIBaseURL: srv.URL}, sock)
	s, err := c.Stream(context.Background(), ChatRequest{Identity: "c", Model: "gpt-4o-mini", Messages: []Message{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	for range s.Text {
	}
	_, err = s.Result()
	require.NoError(t, err)
	assertHolds(t, sock, (10 * time.Minute).Milliseconds())
}

func TestCaptureHolds(t *testing.T) {
	sock := &mockSocket{}
	c := newWithTransport(Options{}, sock)
	ctx := context.Background()

	before := time.Now().UnixMilli()
	cap1, err := c.WithdrawAsync(ctx, WithdrawRequest{Identity: "c", Amount: 5, IdempotencyKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, (24 * time.Hour).Milliseconds(), payloadOf(sock, 0)["duration"])
	assert.GreaterOrEqual(t, cap1.ExpiresAt, before+(24*time.Hour).Milliseconds())

	_, err = c.WithdrawAsync(ctx, WithdrawRequest{Identity: "c", Amount: 5, IdempotencyKey: "k2", HoldFor: 2 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, (2 * time.Hour).Milliseconds(), payloadOf(sock, 1)["duration"])

	_, err = c.CreditAsync(ctx, WithdrawRequest{Identity: "c", Amount: 5, IdempotencyKey: "k3", HoldFor: 90 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, int64(90_000), payloadOf(sock, 2)["duration"])

	_, err = c.Close(ctx, CloseRequest{CaptureID: cap1.CaptureID})
	require.NoError(t, err)
	assert.NotContains(t, payloadOf(sock, 3), "duration")
}

func TestCaptureRejectsNegativeHold(t *testing.T) {
	sock := &mockSocket{}
	c := newWithTransport(Options{}, sock)
	_, err := c.WithdrawAsync(context.Background(), WithdrawRequest{Identity: "c", Amount: 5, IdempotencyKey: "k", HoldFor: -time.Second})
	assert.Error(t, err)
	assert.Empty(t, sock.sent)
}

func TestForeignCloseOfCreditIsLabeledCredit(t *testing.T) {
	sock := &mockSocket{}
	c := newWithTransport(Options{}, sock)
	amt := -7.0
	_, err := c.Close(context.Background(), CloseRequest{CaptureID: "cap-y", Identity: "cust", Amount: &amt})
	require.NoError(t, err)
	md := payloadOf(sock, 0)["metadata"].(map[string]any)
	assert.Equal(t, "CREDIT", md["method"])
	assert.Equal(t, "vibe:credit:cust", md["url"])
}
