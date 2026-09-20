#!/usr/bin/env bash
# Loads .env (if present) and runs the Vibe example on $PORT (default 4003).
set -euo pipefail
cd "$(dirname "$0")"
if [ -f .env ]; then set -a; . ./.env; set +a; fi
exec go run .
