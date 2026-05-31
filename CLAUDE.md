# olcRTC

Encrypted TCP-over-WebRTC tunnel written in Go. Traffic is disguised as a regular
WebRTC/SFU video call on whitelisted services (Jitsi, Yandex Telemost, WbStream) to
bypass blocking. Payload is encrypted with XChaCha20-Poly1305 and multiplexed with smux.

```
app -> SOCKS5 -> olcrtc cnc -> WebRTC/SFU service -> olcrtc srv -> internet
```

## Build & test commands

The build system is [Mage](https://magefile.org) (`magefile.go`). Go 1.26+ is required.

- `go build ./...` — compile everything (fast sanity check)
- `go test -count=1 ./...` — run unit tests
- `go test -count=1 -race ./...` — run tests with the race detector
- `mage test` — unit tests via Mage; `mage testFull` — race + full
- `mage vet` — `go vet`; `mage lint` — golangci-lint
- `mage build` — build the CLI for the host; `mage cross` — cross-compile binaries
- `mage mobile` — build the Android AAR (gomobile); `mage tidy` — `go mod tidy`
- `mage check` — vet + lint + test (run this before pushing)

E2E tests in `internal/e2e/` are gated behind flags and skip by default:
- Local e2e runs in the default `go test ./...`.
- Real-provider e2e needs `-olcrtc.real-e2e` and hits live Jitsi/Telemost/WbStream —
  do NOT run it casually; it burns provider quota. Soak/stress need `-olcrtc.*-soak`/`-olcrtc.stress`.

## Code layout

- `cmd/olcrtc/` — CLI entrypoint. Takes a single YAML config: `olcrtc <config.yaml>`.
- `internal/engine/` — SFU providers: `jitsi`, `goolom` (Telemost), `livekit` (WbStream), `builtin`.
- `internal/transport/` — carrier channels: `datachannel`, `vp8channel`, `seichannel`, `videochannel`.
- `internal/auth/` — credential acquisition per provider: `telemost`, `wbstream`, `jitsi`.
- `internal/crypto/` — XChaCha20-Poly1305.
- `internal/muxconn/`, `internal/framing/`, `internal/handshake/`, `internal/control/` — tunnel plumbing.
- `internal/supervisor/` — failover profiles. `internal/app/session/` — session orchestration.
- `pkg/olcrtc/` — embeddable Go library API. `mobile/` — gomobile bindings.
- `docs/` — user docs (configuration, manual, settings matrix, docker, URI/sub formats).

## Modes & config

The CLI is configured entirely by one YAML file (no other flags). Modes:
- `srv` — server side; dials TCP to targets.
- `cnc` — client side; listens on a local SOCKS5.
- `gen` — generates a Room ID for providers that create rooms.

`auth.provider` selects the service: `jitsi` | `telemost` | `wbstream` | `none`.
The legacy name `carrier` still appears in internal API/logs for the auth/provider path.

## Conventions

- Lint is strict: golangci-lint v2 with ~50 linters enabled (`.golangci.yml`), including
  `gochecknoglobals`, `err113`, `errorlint`, `cyclop`, `dupl`. Run `mage lint` before pushing.
- Errors are sentinel `var Err... = errors.New(...)` values, wrapped with `errors.Join`/`%w`.
  Match this pattern; do not introduce bare `fmt.Errorf` string errors where a sentinel fits.
- Package-level test flags use the `olcrtc.` prefix and `//nolint:gochecknoglobals` comments.
- License is WTFPL. Do not add license headers that contradict it.

## Repo etiquette

- CI (`.github/workflows/ci.yml`) runs test, race, coverage, lint, govulncheck, and gated
  real-e2e/soak jobs. Keep the fast jobs green; never wire live-provider jobs to per-push runs.
- Run `mage check` (or at least `go build ./...` + `go test ./...` + `mage lint`) before pushing.
