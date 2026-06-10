#!/bin/sh
# Liveness check for the olcrtc server. The process being alive is not enough:
# a hung/ghost room presence keeps the process up while no client can connect.
# The server writes OLCRTC_HEARTBEAT while it is demonstrably healthy (link up,
# session opened, or traffic); we treat a stale heartbeat as unhealthy so
# `docker ps` / orchestration reflects the real state. The in-process watchdog
# is what actually restarts a stuck server.
set -eu

# Process must exist.
pidof olcrtc >/dev/null 2>&1 || exit 1

# If no heartbeat is configured, fall back to process-liveness only.
HB="${OLCRTC_HEARTBEAT:-}"
[ -n "$HB" ] || exit 0

# Heartbeat file must exist and be fresh (< 11 min; watchdog deadline is 10 min).
[ -f "$HB" ] || exit 1
now=$(date +%s)
mtime=$(stat -c %Y "$HB" 2>/dev/null || echo 0)
age=$((now - mtime))
[ "$age" -lt 660 ] || exit 1
exit 0
