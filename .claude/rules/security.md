---
paths:
  - "internal/crypto/**/*.go"
  - "internal/handshake/**/*.go"
  - "internal/auth/**/*.go"
---

# Security-sensitive code

This code handles encryption keys, handshakes, and credential acquisition for a
censorship-circumvention tunnel. Treat correctness and secrecy as critical.

- Never log secret material: `crypto.key`, tokens, room credentials, nonces.
- XChaCha20-Poly1305 nonces must be unique per key; never reuse a nonce.
- Keep constant-time comparisons for secrets (`crypto/subtle`), not `==`.
- Do not weaken or "simplify" auth/handshake flows without explicit instruction.
- New errors here stay sentinel `var Err... = errors.New(...)`, wrapped with `%w`.
