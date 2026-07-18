#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BIN_DIR="${TMPDIR:-/tmp}/usageflow-go-chat"

if [[ -f "$SCRIPT_DIR/.env" ]]; then
	set -a
	# shellcheck disable=SC1091
	source "$SCRIPT_DIR/.env"
	set +a
fi

# Reuse OpenAI key from the JS examples if not set in this demo's .env.
JS_EXAMPLES_ENV="$REPO_ROOT/../js/examples/.env"
if [[ -z "${OPENAI_API_KEY:-}" && -f "$JS_EXAMPLES_ENV" ]]; then
	set -a
	# shellcheck disable=SC1091
	source "$JS_EXAMPLES_ENV"
	set +a
fi

if [[ -z "${USAGEFLOW_API_KEY:-}" ]]; then
	echo "USAGEFLOW_API_KEY is required." >&2
	echo "Copy .env.example to .env and add your UsageFlow application key." >&2
	exit 1
fi

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
	echo "OPENAI_API_KEY is required for live LLM replies." >&2
	echo "Add it to examples/chat/.env or agents/js/examples/.env (same key the JS demos use)." >&2
	exit 1
fi

mkdir -p "$BIN_DIR"

echo "Building UsageFlow instrumentation CLI..."
go build -o "$BIN_DIR/usageflow" "$REPO_ROOT/cmd/usageflow"

echo "Building automatically instrumented Go chat..."
(
	cd "$SCRIPT_DIR"
	"$BIN_DIR/usageflow" go build -o "$BIN_DIR/chat" .
)

PORT="${PORT:-8081}"
echo
echo "Starting UsageFlow Go chat: http://127.0.0.1:$PORT"
echo "The status badge turns green when the live WebSocket is connected."
echo

exec "$BIN_DIR/chat"
