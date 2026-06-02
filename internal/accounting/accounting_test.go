package accounting

import (
	"sync"
	"testing"
)

func TestAcquireRespectsLimit(t *testing.T) {
	tr := New(2)
	if !tr.Acquire("s1") {
		t.Fatal("first acquire should succeed")
	}
	if !tr.Acquire("s1") {
		t.Fatal("second acquire should succeed at limit 2")
	}
	if tr.Acquire("s1") {
		t.Fatal("third acquire should fail at limit 2")
	}
	// A different session is independent.
	if !tr.Acquire("s2") {
		t.Fatal("other session should not be limited by s1")
	}
	// Releasing frees a slot.
	tr.Release("s1")
	if !tr.Acquire("s1") {
		t.Fatal("acquire should succeed after release")
	}
}

func TestUnlimitedWhenZero(t *testing.T) {
	tr := New(0)
	for range 1000 {
		if !tr.Acquire("s") {
			t.Fatal("zero limit must never block")
		}
	}
}

func TestNegativeLimitTreatedAsUnlimited(t *testing.T) {
	tr := New(-5)
	for range 10 {
		if !tr.Acquire("s") {
			t.Fatal("negative limit must be unlimited")
		}
	}
}

func TestReleaseNeverGoesNegative(t *testing.T) {
	tr := New(1)
	tr.Release("ghost") // unknown session: no panic, no-op
	if !tr.Acquire("s") {
		t.Fatal("acquire should succeed")
	}
	tr.Release("s")
	tr.Release("s") // extra release must not underflow
	if !tr.Acquire("s") {
		t.Fatal("acquire should still succeed at limit 1")
	}
	if tr.Acquire("s") {
		t.Fatal("limit 1 should block second concurrent stream")
	}
}

func TestBytesAndSnapshot(t *testing.T) {
	tr := New(0)
	_ = tr.Acquire("b")
	tr.AddBytes("b", 100, 250)
	tr.AddBytes("b", 50, 0)
	tr.AddBytes("a", 1, 2)

	snap := tr.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	// Sorted by session ID: "a" before "b".
	if snap[0].SessionID != "a" || snap[1].SessionID != "b" {
		t.Fatalf("snapshot not sorted: %+v", snap)
	}
	b := snap[1]
	if b.BytesIn != 150 || b.BytesOut != 250 {
		t.Fatalf("bytes = in %d out %d, want 150/250", b.BytesIn, b.BytesOut)
	}
	if b.TotalStreams != 1 || b.ActiveStream != 1 {
		t.Fatalf("streams total %d active %d, want 1/1", b.TotalStreams, b.ActiveStream)
	}
}

func TestForget(t *testing.T) {
	tr := New(0)
	_ = tr.Acquire("s")
	tr.AddBytes("s", 10, 10)
	tr.Forget("s")
	if len(tr.Snapshot()) != 0 {
		t.Fatal("Forget should drop all session state")
	}
}

func TestNilTrackerIsSafe(t *testing.T) {
	var tr *Tracker // a server constructed without a tracker
	if !tr.Acquire("s") {
		t.Fatal("nil tracker must allow acquire (unlimited)")
	}
	tr.Release("s")
	tr.AddBytes("s", 1, 1)
	tr.Forget("s")
	if tr.Snapshot() != nil {
		t.Fatal("nil tracker snapshot should be nil")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tr := New(0)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "s"
			if i%2 == 0 {
				id = "t"
			}
			if tr.Acquire(id) {
				tr.AddBytes(id, 1, 1)
				tr.Release(id)
			}
		}()
	}
	wg.Wait()
	// Both sessions should have zero active streams after all releases.
	for _, s := range tr.Snapshot() {
		if s.ActiveStream != 0 {
			t.Fatalf("session %s left %d active streams", s.SessionID, s.ActiveStream)
		}
	}
}
