package accounting

import (
	"path/filepath"
	"testing"
)

func TestWriteReadRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	in := []Record{
		{SessionID: "a", DeviceID: "d1", TotalStreams: 2, BytesIn: 100, BytesOut: 200},
		{SessionID: "b", TotalStreams: 1, BytesIn: 5, BytesOut: 0},
	}
	if err := WriteRecords(path, in); err != nil {
		t.Fatalf("WriteRecords: %v", err)
	}
	got, err := ReadRecords(path)
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	if len(got) != 2 || got[0].SessionID != "a" || got[0].BytesOut != 200 || got[1].SessionID != "b" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestReadRecordsMissingFileIsEmpty(t *testing.T) {
	got, err := ReadRecords(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("ReadRecords(missing) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("ReadRecords(missing) = %v, want nil", got)
	}
}

func TestRecordsReflectTrackerState(t *testing.T) {
	tr := New(0)
	tr.SetDevice("s", "device-x")
	_ = tr.Acquire("s")
	tr.AddBytes("s", 10, 20)
	recs := tr.Records()
	if len(recs) != 1 {
		t.Fatalf("records len = %d, want 1", len(recs))
	}
	r := recs[0]
	if r.SessionID != "s" || r.DeviceID != "device-x" || r.BytesIn != 10 || r.BytesOut != 20 || r.TotalStreams != 1 {
		t.Fatalf("record mismatch: %+v", r)
	}
}
