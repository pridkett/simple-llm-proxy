#!/usr/bin/env bash
# dev.sh — Start backend and frontend dev servers with secrets from 1Password.
# Usage: ./scripts/dev.sh  (or: make dev)
# Ctrl+C kills both processes.

set -euo pipefail
trap 'kill 0' EXIT

echo "Starting backend on :8080 and frontend on :5173..."
echo ""

op run --env-file op.env --no-masking -- make run 2>&1 | sed 's/^/[backend]  /' &
op run --env-file op.env --no-masking -- make frontend-dev 2>&1 | sed 's/^/[frontend] /' &

wait
