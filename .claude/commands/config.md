---
description: Build a release config YAML for a given mode (srv/cnc/gen) and provider.
argument-hint: <mode> <provider>
---

Generate a minimal, valid olcRTC YAML config for mode `$1` and provider `$2`.

Rules:
- Modes: `srv` (server, TCP dial to targets), `cnc` (client, local SOCKS5), `gen` (room id).
- `auth.provider` is one of `jitsi | telemost | wbstream | none`.
- Always set `data:` (data directory) — the CLI errors without it.
- Reference `docs/configuration.md` and `docs/examples/` for the exact schema;
  read them first instead of guessing field names.
- Never invent fields. If a required credential is unknown, leave a clearly
  marked `# TODO` placeholder rather than a fake value.

Output only the YAML, preceded by a one-line note on where to place it.
