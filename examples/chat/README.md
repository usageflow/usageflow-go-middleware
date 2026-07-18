# UsageFlow Go Chat

A browser chat application backed by Gin and connected to the real UsageFlow
WebSocket server. Business functions are automatically instrumented at build
time—there are no `Track` or `Wrap` calls.

## Run

```bash
cd examples/chat
cp .env.example .env
# Add your UsageFlow application key to .env
# OPENAI_API_KEY can live in this .env, or run-live.sh will reuse agents/js/examples/.env
./run-live.sh
```

Open [http://127.0.0.1:8081](http://127.0.0.1:8081).

Replies come from OpenAI (`gpt-4o-mini`), the same model/key path as the JS express demo.
The instrumented `llmCompletion` call records real `usage` / `aiModel` on the call chain.

## UsageFlow application setup

Configure the application to monitor:

- Method: `POST`
- Path: `/api/chat`

The browser sends `X-User-ID: local-chat-user`, which can be selected as the
identity header when configuring a policy.

The UI status badge uses `/api/status` and turns green when at least one
UsageFlow WebSocket connection is active.
