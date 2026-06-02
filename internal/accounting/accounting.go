// Package accounting tracks per-session usage for an olcrtc server: the number
// of concurrent tunnel streams (to enforce a per-client limit) and cumulative
// byte counts (for volume-based billing).
//
// A session here corresponds to one authorized client: the server derives the
// session ID from the client's access token at handshake time, so all streams
// and bytes recorded under that ID belong to the same subscriber.
//
// The zero value is not usable; construct a Tracker with New. All methods are
// safe for concurrent use.
package accounting

import (
	"sort"
	"sync"
)

// Tracker accumulates per-session stream and byte counts and enforces an
// optional cap on concurrent streams per session.
type Tracker struct {
	maxStreams int // per-session concurrent stream cap; 0 means unlimited

	mu    sync.Mutex
	state map[string]*sessionState
}

type sessionState struct {
	deviceID string // device ID seen at handshake, for billing correlation
	active   int    // currently open streams
	bytesIn  uint64 // client -> target
	bytesOut uint64 // target -> client
	streams  uint64 // total streams ever opened
}

// Stat is an immutable snapshot of one session's usage.
type Stat struct {
	SessionID    string
	ActiveStream int
	TotalStreams uint64
	BytesIn      uint64
	BytesOut     uint64
}

// New returns a Tracker that caps concurrent streams per session at maxStreams.
// A maxStreams of 0 (or negative) disables the limit; the tracker still meters
// bytes and stream counts.
func New(maxStreams int) *Tracker {
	if maxStreams < 0 {
		maxStreams = 0
	}
	return &Tracker{
		maxStreams: maxStreams,
		state:      make(map[string]*sessionState),
	}
}

// Acquire reserves a stream slot for sessionID. It returns false if the session
// is already at the configured concurrency limit, in which case no slot is
// taken and the caller must reject the stream. When the limit is disabled it
// always succeeds. A successful Acquire must be paired with one Release.
func (t *Tracker) Acquire(sessionID string) bool {
	if t == nil {
		return true // nil tracker: unlimited, no metering
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	st := t.state[sessionID]
	if st == nil {
		st = &sessionState{}
		t.state[sessionID] = st
	}
	if t.maxStreams > 0 && st.active >= t.maxStreams {
		return false
	}
	st.active++
	st.streams++
	return true
}

// Release frees a stream slot previously taken by Acquire. It is safe to call
// for an unknown session (no-op) and never drops the active count below zero.
func (t *Tracker) Release(sessionID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if st := t.state[sessionID]; st != nil && st.active > 0 {
		st.active--
	}
}

// AddBytes records bytesIn/bytesOut transferred on a stream of sessionID.
func (t *Tracker) AddBytes(sessionID string, bytesIn, bytesOut uint64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	st := t.state[sessionID]
	if st == nil {
		st = &sessionState{}
		t.state[sessionID] = st
	}
	st.bytesIn += bytesIn
	st.bytesOut += bytesOut
}

// SetDevice records the device ID associated with sessionID, for correlating
// usage to a client at billing time. Safe to call before any bytes/streams.
func (t *Tracker) SetDevice(sessionID, deviceID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state[sessionID]
	if st == nil {
		st = &sessionState{}
		t.state[sessionID] = st
	}
	st.deviceID = deviceID
}

// Records returns the persistable per-session usage, sorted by session ID.
func (t *Tracker) Records() []Record {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Record, 0, len(t.state))
	for id, st := range t.state {
		out = append(out, Record{
			SessionID:    id,
			DeviceID:     st.deviceID,
			TotalStreams: st.streams,
			BytesIn:      st.bytesIn,
			BytesOut:     st.bytesOut,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// Forget drops all accounting for sessionID. Call it when a session ends so a
// long-lived server does not accumulate state for departed clients. Any active
// count is discarded along with the totals.
func (t *Tracker) Forget(sessionID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, sessionID)
}

// Snapshot returns the current per-session usage, sorted by session ID, for
// metrics or billing. The returned slice is a copy and safe to retain.
func (t *Tracker) Snapshot() []Stat {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Stat, 0, len(t.state))
	for id, st := range t.state {
		out = append(out, Stat{
			SessionID:    id,
			ActiveStream: st.active,
			TotalStreams: st.streams,
			BytesIn:      st.bytesIn,
			BytesOut:     st.bytesOut,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}
