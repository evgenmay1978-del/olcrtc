# third_party

Vendored, minimally-patched copies of upstream dependencies that olcRTC needs
to fork. Each is wired in via a `replace` directive in the root `go.mod`.

## pion-ice (github.com/pion/ice/v4 @ v4.2.5)

One-line behavioural patch in `agent.go`: the ICE agent's
`insecureSkipVerify` is forced to `true` so TURN/DTLS relay candidates are
gathered even when the relay presents an expired or otherwise invalid TLS
certificate.

Rationale: olcRTC tunnels ride on third-party whitelisted SFUs (Jitsi
instances) whose TURN relays are outside our control and periodically serve
broken certificates. On mobile / CGNAT networks the TURN relay is mandatory
for connectivity, so a bad relay cert otherwise drops every session. The
tunnel payload is already end-to-end encrypted with XChaCha20-Poly1305, so
validating the relay's transport certificate provides no additional security.

Test files were removed from the vendored copy; only the build sources are
kept. To re-sync with upstream, re-copy the module and re-apply the
`insecureSkipVerify: true` change.
