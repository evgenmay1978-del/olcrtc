---
description: Run the full pre-push gate (build, vet, lint, tests) and report failures.
---

Run the project's pre-push verification and report results concisely.

1. `go build ./...`
2. `go vet ./...`
3. `golangci-lint run --timeout=5m` (or `mage lint`)
4. `go test -count=1 ./...`

If any step fails, stop and show the exact failing output — do not summarize
away the error. If everything passes, say so plainly. Do not commit or push;
this command only verifies.
