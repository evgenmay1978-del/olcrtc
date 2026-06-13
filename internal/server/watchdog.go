package server

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// The server must stay reachable in its room the way a normal VPN stays up:
// reconnect on its own, forever. The jitsi reconnect path can, in rare cases,
// block waiting for a peer (WaitJingleReinitiate) while its MUC presence has
// silently died, leaving the server a ghost participant that never recovers.
// The watchdog is a last-resort liveness guard: if the server shows no healthy
// activity (link up, session opened, or traffic) within healthDeadline, it
// exits so the container manager (Docker restart=unless-stopped) restarts it
// with a fresh join. Active traffic continuously refreshes the clock, so a busy
// server never restarts; an idle-but-alive server only pays a few-second
// reconnect; a stuck server recovers automatically.
const (
	healthDeadline = 10 * time.Minute
	watchdogTick   = 30 * time.Second
)

// healthEnvVar names the heartbeat file; when unset the watchdog stays off
// (e.g. in unit tests), so server.Run never calls os.Exit there.
const healthEnvVar = "OLCRTC_HEARTBEAT"

// healthClock tracks the last moment the server was demonstrably healthy.
type healthClock struct{ last atomic.Int64 }

func (h *healthClock) mark() { h.last.Store(time.Now().UnixNano()) }

func (h *healthClock) age() time.Duration {
	return time.Since(time.Unix(0, h.last.Load()))
}

func heartbeatPath() string { return os.Getenv(healthEnvVar) }

func writeHeartbeat(path string) {
	if path == "" {
		return
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o750)
	}
	_ = os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
}

// runWatchdog refreshes the heartbeat file while healthy and exits the process
// if the server has been unhealthy for longer than healthDeadline. It is a
// no-op unless OLCRTC_HEARTBEAT is set.
func (s *Server) runWatchdog(ctx context.Context) {
	path := heartbeatPath()
	if path == "" {
		return
	}
	s.wd.mark()
	writeHeartbeat(path)
	t := time.NewTicker(watchdogTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.wd.age() < healthDeadline {
				writeHeartbeat(path)
				continue
			}
			logger.Errorf(
				"watchdog: no healthy activity for %s - restarting process for a fresh room join",
				s.wd.age().Round(time.Second),
			)
			t.Stop()
			//nolint:gocritic // intentional hard exit; the process is terminating so deferred cleanup is moot
			os.Exit(1)
		}
	}
}
