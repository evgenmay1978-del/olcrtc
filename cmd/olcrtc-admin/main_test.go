package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/accounting"
)

const (
	flagRegistry = "-registry"
	flagUsage    = "-usage"
	cmdGrant     = "grant"
	labelAlice   = "alice"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunGrantRequestApproveReject(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "clients.json")

	var out bytes.Buffer
	mustRun := func(args ...string) {
		t.Helper()
		out.Reset()
		full := append([]string{flagRegistry, reg}, args...)
		if err := run(full, &out); err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
	}

	mustRun(cmdGrant, labelAlice, "720h", "free")
	if !strings.Contains(out.String(), "granted active access") {
		t.Fatalf("grant output = %q", out.String())
	}

	mustRun("request", "bob", "sber-6564")
	if !strings.Contains(out.String(), "pending") {
		t.Fatalf("request output = %q", out.String())
	}

	mustRun("list")
	listing := out.String()
	if !strings.Contains(listing, labelAlice) || !strings.Contains(listing, "bob") {
		t.Fatalf("list missing clients: %q", listing)
	}

	mustRun("approve", "bob", "720h")
	mustRun("reject", labelAlice)
	mustRun("list")
	if !strings.Contains(out.String(), "rejected") {
		t.Fatalf("expected alice rejected: %q", out.String())
	}
}

func TestRunPrune(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "clients.json")
	var out bytes.Buffer
	run2 := func(args ...string) error {
		out.Reset()
		return run(append([]string{flagRegistry, reg}, args...), &out)
	}

	// A pending request whose deadline is already in the past (negative-ish:
	// 1ns). After a brief wait it should be prunable.
	if err := run2("request", "late", "1ms", "paid-late"); err != nil {
		t.Fatalf("request: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := run2("prune"); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !strings.Contains(out.String(), "auto-rejected") {
		t.Fatalf("prune output = %q, want auto-rejected", out.String())
	}

	// Pruning again is a no-op.
	if err := run2("prune"); err != nil {
		t.Fatalf("prune again: %v", err)
	}
	if !strings.Contains(out.String(), "no expired pending") {
		t.Fatalf("second prune output = %q", out.String())
	}
}

func TestRunClientConfig(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "clients.json")
	srv := filepath.Join(dir, "server.yaml")
	writeFile(t, srv, "mode: srv\nauth: {provider: jitsi}\n"+
		"room: {id: \"https://meet1.arbitr.ru/x\"}\n"+
		"crypto: {key: \"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff\"}\n"+
		"net: {transport: datachannel, dns: \"8.8.8.8:53\"}\n"+
		"cover: {enabled: true, interval: 20ms, size: 128}\ndata: data\n")

	var out bytes.Buffer
	if err := run([]string{flagRegistry, reg, cmdGrant, labelAlice, "720h"}, &out); err != nil {
		t.Fatalf("grant: %v", err)
	}

	out.Reset()
	if err := run([]string{flagRegistry, reg, "-server", srv, "client-config", labelAlice}, &out); err != nil {
		t.Fatalf("client-config: %v", err)
	}
	got := out.String()
	for _, want := range []string{"mode: cnc", "token:", "datachannel", "enabled: true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("client-config output missing %q:\n%s", want, got)
		}
	}

	// Without -server it must error clearly.
	out.Reset()
	if err := run([]string{flagRegistry, reg, "client-config", labelAlice}, &out); !errors.Is(err, errNoServerConfig) {
		t.Fatalf("client-config without -server = %v, want errNoServerConfig", err)
	}
}

func TestRunRotate(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "clients.json")
	var out bytes.Buffer
	run2 := func(args ...string) error {
		out.Reset()
		return run(append([]string{flagRegistry, reg}, args...), &out)
	}

	if err := run2(cmdGrant, labelAlice, "720h"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := run2("rotate", labelAlice); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if !strings.Contains(out.String(), "rotated token") || !strings.Contains(out.String(), "token:") {
		t.Fatalf("rotate output = %q", out.String())
	}

	// Rotating an unknown client errors.
	if err := run2("rotate", "ghost"); err == nil {
		t.Fatal("rotate ghost: expected error, got nil")
	}
}

func TestRunUsage(t *testing.T) {
	var out bytes.Buffer
	// Without -usage: specific error.
	if err := run([]string{cmdUsage}, &out); !errors.Is(err, errNoUsageFile) {
		t.Fatalf("usage without file = %v, want errNoUsageFile", err)
	}

	// Empty/missing file: friendly message, no error.
	missing := filepath.Join(t.TempDir(), "usage.json")
	out.Reset()
	if err := run([]string{flagUsage, missing, cmdUsage}, &out); err != nil {
		t.Fatalf("usage missing file: %v", err)
	}
	if !strings.Contains(out.String(), "no usage recorded") {
		t.Fatalf("usage missing output = %q", out.String())
	}

	// Populated file: a row with the session and human bytes appears.
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := accounting.WriteRecords(path, []accounting.Record{
		{SessionID: "sess-1", DeviceID: "dev-1", TotalStreams: 4, BytesIn: 1500000, BytesOut: 0},
	}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	out.Reset()
	if err := run([]string{flagUsage, path, cmdUsage}, &out); err != nil {
		t.Fatalf("usage: %v", err)
	}
	got := out.String()
	for _, want := range []string{"sess-1", "dev-1", "MiB", "TOTAL"} {
		if !strings.Contains(got, want) {
			t.Fatalf("usage output missing %q:\n%s", want, got)
		}
	}
}

func TestRunUsageErrors(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); !errors.Is(err, errUsage) {
		t.Fatalf("run(nil) = %v, want errUsage", err)
	}
	// A registry command with no label is rejected.
	reg := filepath.Join(t.TempDir(), "c.json")
	if err := run([]string{flagRegistry, reg, cmdGrant}, &out); !errors.Is(err, errMissingLabel) {
		t.Fatalf("grant without label = %v, want errMissingLabel", err)
	}
}

func TestRunPayNeedsNoRegistry(t *testing.T) {
	var out bytes.Buffer
	// pay without -pay-info is a specific error, not the generic usage error.
	if err := run([]string{cmdPay}, &out); !errors.Is(err, errNoPayInfo) {
		t.Fatalf("pay without info = %v, want errNoPayInfo", err)
	}

	info := filepath.Join(t.TempDir(), "pay.txt")
	writeFile(t, info, "Pay to +7...6564\n")
	out.Reset()
	if err := run([]string{"-pay-info", info, cmdPay}, &out); err != nil {
		t.Fatalf("pay: %v", err)
	}
	if !strings.Contains(out.String(), "6564") {
		t.Fatalf("pay output = %q", out.String())
	}
}
