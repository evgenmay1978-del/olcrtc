package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateClientConfig(t *testing.T) {
	const key = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	server := File{
		Mode:     "srv",
		Auth:     Auth{Provider: testProviderJits},
		Room:     Room{ID: "https://meet1.arbitr.ru/secretroom"},
		Crypto:   Crypto{Key: key},
		Net:      Net{Transport: testTransportDC, DNS: "1.1.1.1:53"},
		Liveness: Liveness{Interval: "10s", Timeout: "5s", Failures: 3},
		Cover:    Cover{Enabled: true, Interval: testDelay20ms, Size: 128},
		Access:   Access{ClientsFile: "clients.json"}, // server-only, must not leak
	}

	data, err := GenerateClientConfig(server, "TOKEN-XYZ")
	if err != nil {
		t.Fatalf("GenerateClientConfig: %v", err)
	}

	// The generated YAML must round-trip through Load with matching fields.
	f := parseClientYAML(t, data)
	checks := map[string]bool{
		"mode cnc":      f.Mode == testModeCNC,
		"token copied":  f.Access.Token == "TOKEN-XYZ",
		"key copied":    f.Crypto.Key == key,
		"provider kept": f.Auth.Provider == testProviderJits,
		"transport":     f.Net.Transport == testTransportDC,
		"cover enabled": f.Cover.Enabled && f.Cover.Interval == testDelay20ms,
		"socks host":    f.SOCKS.Host == defaultClientSOCKSHost,
		"socks port":    f.SOCKS.Port == defaultClientSOCKSPort,
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("check failed: %s (got %+v)", name, f)
		}
	}

	// Server-only fields must not appear in the client output.
	if strings.Contains(string(data), "clients_file") {
		t.Error("generated client config leaked server clients_file")
	}
}

func TestGenerateClientConfigDefaultDNS(t *testing.T) {
	data, err := GenerateClientConfig(File{Net: Net{Transport: testTransportDC}}, "t")
	if err != nil {
		t.Fatalf("GenerateClientConfig: %v", err)
	}
	if !strings.Contains(string(data), defaultClientDNS) {
		t.Fatalf("expected default DNS %q in output", defaultClientDNS)
	}
}

// parseClientYAML loads generated YAML through Load (same machinery the CLI
// uses), without the caller touching the filesystem.
func parseClientYAML(t *testing.T, data []byte) File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("generated config does not parse: %v", err)
	}
	return f
}
