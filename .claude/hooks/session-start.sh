#!/bin/bash
# SessionStart hook: warm the Go build cache and install dev tools so tests,
# linters, and vuln scans are ready immediately in Claude Code web sessions.
# Idempotent and non-interactive. Web sessions only.
set -euo pipefail

# Only run in the remote (web) environment; local users have their own setup.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

# Pre-download modules so the first `go build`/`go test` is fast.
go mod download || true

# Best-effort dev tooling used by CI (lint + vuln scan). Don't fail the
# session if a tool install hiccups; the agent can install on demand.
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest || true
go install golang.org/x/vuln/cmd/govulncheck@latest || true

# Expose installed tools on PATH for the rest of the session.
if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  echo "export PATH=\"\$PATH:$(go env GOPATH)/bin\"" >> "$CLAUDE_ENV_FILE"
fi

exit 0
