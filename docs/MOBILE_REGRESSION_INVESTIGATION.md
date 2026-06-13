# Task: Find why our fork fails to tunnel on mobile (when upstream worked)

> Для исполнителя (Claude с доступом к репозиториям): язык работы — английский,
> но отвечай пользователю по-русски. Это боевой коммерческий продукт. Нельзя
> сломать 3x-ui на сервере 194.48.141.106. Не выдумывай — только факты с
> доказательствами. Следуй разделу "Operating rules" дословно.

## 1. Ground truth (verified facts — do not re-derive from memory)

- Product: olcRTC tunnels TCP over a WebRTC/Jitsi "video call" on Russian
  whitelisted services to bypass blocking. Payload is XChaCha20-Poly1305 +
  smux. Client (cnc) ↔ Jitsi room ↔ server (srv) ↔ internet.
- Our repo (this one): `github.com/evgenmay1978-del/olcrtc`. It is a fork of:
  - engine/CLI upstream: `github.com/openlibrecommunity/olcrtc` (our Go module
    path is identical: `github.com/openlibrecommunity/olcrtc`).
  - Android/Compose app `app/`: fork of `github.com/evgenmay1978-del/olcrtc` (MIT),
    namespace `ru.maestrovpn.app`.
- Server: `194.48.141.106`, repo cloned at `/root/olcrtc`, run via
  `docker compose -f docker-compose.server.yml` (env in `.local/.env`). A 3x-ui
  install shares the box (sshd on port 80, xray on 443) — MUST NOT be touched.
- Jitsi instance in use: `meet1.arbitr.ru` (resolves 84.201.184.28). Room
  `olc-maria-xr1uvy`. Key/token live in `.local/.env` and `clients.json`.
- The instance advertises these ICE servers (logged on join, verified):
  - `turn:51.250.19.85:3478?transport=tcp`
  - `turns:turns.parcsis.net:5349?transport=tcp`  (turns.parcsis.net → 51.250.19.85)
  - colibri-ws URL is EMPTY in the join log (`colibri-ws=`).
- On the user's MOBILE network: signaling works (client joins the room), but
  ICE fails: `Failed to ping without candidate pairs`, relay reads to
  `51.250.19.85` time out, ICE → Failed, `jitsi peer connection failed`.
  The phone reaches `meet1.arbitr.ru` (84.201.184.28:443) but NOT
  `51.250.19.85`.
- **THE CONTRADICTION TO EXPLAIN:** the user reports that with the UPSTREAM
  client (evgenmay1978-del/olcrtc + openlibrecommunity/olcrtc) and THIS SAME server,
  the tunnel worked. So this is almost certainly a REGRESSION introduced by our
  fork, not a fundamental impossibility. Treat "instance unusable on mobile" as
  a DISPROVEN hypothesis unless you re-verify it with evidence.

## 2. Changes our fork made (candidate regressions — verify each in git)

Investigate the diff between upstream and our fork. Known fork changes:
- App: `app/.../data/model/LocationConfig.kt` `supportedTransportsForProvider`
  — upstream Jitsi = `[datachannel]` only; we changed it to
  `[vp8channel, datachannel, seichannel]` and made vp8channel the DEFAULT.
  **Leading hypothesis:** upstream tunneled Jitsi over `datachannel` (data via
  the colibri-ws / SCTP bridge), which may NOT require a reachable TURN media
  path; our `vp8channel` default sends data as VIDEO over the PeerConnection,
  which REQUIRES ICE+TURN media (blocked on mobile). VERIFY this end to end.
- Engine `internal/engine/jitsi/jitsi.go`: we added (a) rtcp keepalive grace
  until Connected, (b) a diagnostic ICE-server log. We did NOT change ICE/TURN
  selection or colibri-ws handling.
- We vendored `github.com/pion/ice/v4` under `third_party/pion-ice` with
  `insecureSkipVerify=true` (skip TURN cert check). Confirm this didn't change
  ICE behaviour beyond cert validation.
- Other fork additions (payment UI, Android TV, release APK CI) are unrelated.

## 3. The investigation (do this, in order)

1. Obtain upstream sources and diff against our fork — verify: produce a
   concrete list (file:line) of every behavioural difference in: the jitsi
   engine, transport selection, ICE/colibri-ws handling, and the app's default
   transport for Jitsi. `git log`/`git diff` against the fork point, or clone
   `openlibrecommunity/olcrtc` and `evgenmay1978-del/olcrtc` and diff.
2. Determine how UPSTREAM establishes the Jitsi data path on mobile — verify:
   read upstream code and state, with file:line, which transport upstream uses
   for Jitsi by default and whether its data path goes over colibri-ws/SCTP
   (no TURN video) vs a PeerConnection video track (needs TURN). Confirm
   whether colibri-ws is expected to be present and why ours logs it empty.
3. Reproduce both paths against the user's server/instance — verify: with the
   server reachable, capture engine logs for a `datachannel` attempt vs a
   `vp8channel` attempt and show exactly where each succeeds/fails. (Server
   commands must be given to the user to run; you cannot SSH — sshd is on
   port 80 behind an HTTP proxy that blocks raw SSH from the sandbox.)
4. Identify the single root cause with evidence — verify: name the exact
   diverging behaviour and tie it to the observed mobile failure. Distinguish
   FACT from HYPOTHESIS.
5. Implement the minimal fix — verify: `go build ./...`,
   `go test ./internal/...`, and the app jvmTest pass; the APK builds in CI.
6. Field-verify on the user's mobile — verify: app logs show `Link connected`
   sustained > 2 min AND real traffic (a website loads). Do NOT declare success
   without this on-device evidence.

## 4. Tools you must use

- Spawn parallel agents (Explore / general-purpose / provider-researcher /
  tunnel-reviewer) for breadth: one to diff upstream vs fork engine, one for the
  app transport path, one (provider-researcher) for Jitsi colibri-ws / TURN
  semantics. Read ACTUAL code; do not rely on training memory about Jitsi.
- Use `git` to compare against the fork point and upstream tags.
- Use the GitHub MCP tools for our repo (`evgenmay1978-del/olcrtc`) and CI/APK
  (release `android-latest`). The repo is PUBLIC now — Actions are free.
- Build APK only by merging to `main` (android-app.yml triggers on app/, mobile/,
  internal/, third_party/, pkg/, go.mod, go.sum). Debug APKs are unsigned and
  re-signed per build, so the user must uninstall before reinstalling.

## 5. Operating rules (anti-laziness / anti-hallucination — mandatory)

1. EVIDENCE OR SILENCE. Every factual claim cites `path:line` or a command's
   output. If you have not opened the file or run the command, do not state it
   as fact. No "probably", "should", "typically" presented as truth.
2. FACT vs HYPOTHESIS. Label every statement. A hypothesis is not a fix. Never
   ship a change justified only by a hypothesis — first prove the mechanism.
3. REPRODUCE BEFORE FIXING. Show the failure (logs) and the exact diverging
   line vs upstream before writing any fix. "Fix the bug" → "show the log line
   that proves the bug, then the code line that causes it, then make a test or
   capture that flips from fail to pass".
4. SURGICAL CHANGES. Touch only what the root cause requires. Match existing
   style. Don't refactor unrelated code. Every changed line traces to the fix.
5. VERIFY EVERY STEP. After each change: `go build ./...`, relevant
   `go test`, `mage lint` if Go; CI green for app. Paste the result. Don't
   claim green you didn't see.
6. NO SUCCESS WITHOUT FIELD PROOF. "It connects" requires on-device mobile
   logs: `Link connected` held > 2 min and a loaded website. Wi-Fi success does
   NOT count — the target is mobile under the whitelist.
7. DON'T BREAK 3x-ui. Only touch the `olcrtc-server`/`olcrtc-panel` containers
   and our files. Never modify ports 443/2053/2096 or x-ui. Outbound probes are
   fine.
8. KEEP A LIVING LOG. Append findings (with evidence) to this file as you go,
   under "## Findings", so nothing is lost across context resets. Re-read it
   when resuming.
9. STATE BLOCKERS EXPLICITLY. If you need data only the user can get (server
   command output, on-device logs), give the EXACT command/steps and stop —
   don't guess past the missing data.
10. NO SECRETS IN GIT. Phone number, root password, `clients.json`,
    `pay-info.txt`, keys stay out of commits (already gitignored).

## 6. Success criteria

- A written root-cause statement citing the exact upstream-vs-fork divergence
  (file:line) that breaks mobile, backed by reproduced logs.
- A minimal, verified fix merged to `main`, APK rebuilt.
- On-device mobile confirmation: tunnel up > 2 min + a website loads, with the
  app log excerpt proving it.

## Findings

(Append evidence here as you investigate — newest first. Each entry: claim →
evidence `path:line` or command output → FACT/HYPOTHESIS.)
