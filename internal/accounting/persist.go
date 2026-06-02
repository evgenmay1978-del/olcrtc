package accounting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Record is the persisted form of one session's usage, enriched with the
// device ID seen at handshake so an operator can correlate a session to a
// client when billing.
type Record struct {
	SessionID    string `json:"sessionId"`
	DeviceID     string `json:"deviceId,omitempty"`
	TotalStreams uint64 `json:"totalStreams"`
	BytesIn      uint64 `json:"bytesIn"`
	BytesOut     uint64 `json:"bytesOut"`
}

// usageFile is the on-disk schema for a usage snapshot.
type usageFile struct {
	Records []Record `json:"records"`
}

// WriteRecords writes records to path atomically (temp file + rename) with
// 0600 permissions, so a usage snapshot is not world-readable and a crash
// mid-write cannot truncate the live file.
func WriteRecords(path string, records []Record) error {
	data, err := json.MarshalIndent(usageFile{Records: records}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal usage: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".usage-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp usage: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp usage: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp usage: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp usage: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp usage: %w", err)
	}
	return nil
}

// ReadRecords loads a usage snapshot from path. A missing file yields an empty
// slice and no error, so callers can treat "no usage yet" uniformly.
func ReadRecords(path string) ([]Record, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied usage path
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read usage %q: %w", path, err)
	}
	var f usageFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse usage %q: %w", path, err)
	}
	return f.Records, nil
}
