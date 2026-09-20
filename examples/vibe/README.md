# UsageFlow Vibe Example (Go)

Go counterpart of `agents/js/examples/vibe-app`, using `pkg/vibe`. Same routes and defaults; default port is **4003**.

## Setup

```bash
cp .env.example .env   # set USAGEFLOW_API_KEY (+ OPENAI_API_KEY / ANTHROPIC_API_KEY as needed)
./run-local.sh
```

All request fields are optional, so an empty body works for a smoke test.

## Endpoints (all POST except health)

`/api/health`, `/api/chat/{openai,anthropic}`, `/api/stream/{openai,anthropic}`, `/api/embed`,
`/api/withdraw`, `/api/credit`, `/api/withdraw-async`, `/api/credit-async`, `/api/close`.

## curl

```bash
curl http://localhost:4003/api/health

curl -X POST -H 'Content-Type: application/json' \
  -d '{"prompt":"Say hello in 5 words","identity":"cust_acme","workflow":"vibe-example-openai"}' \
  http://localhost:4003/api/chat/openai

curl -X POST -H 'Content-Type: application/json' -d '{"prompt":"Say hello"}' http://localhost:4003/api/stream/anthropic

# One-shot ledger deduction
curl -X POST -H 'Content-Type: application/json' \
  -d '{"identity":"cust_acme","amount":50,"idempotencyKey":"demo-1"}' http://localhost:4003/api/withdraw

# Reserve now (returns captureId), settle later with the real amount
curl -X POST -H 'Content-Type: application/json' \
  -d '{"identity":"cust_acme","amount":100,"idempotencyKey":"demo-2","workflow":"vibe-example-openai"}' \
  http://localhost:4003/api/withdraw-async
curl -X POST -H 'Content-Type: application/json' \
  -d '{"captureId":"<captureId from above>","amount":60}' http://localhost:4003/api/close
```

A policy/quota denial returns HTTP 403 `{ "error": "rejected", ... }`.
