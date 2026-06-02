package config

import (
	"bytes"
	"fmt"
	"text/template"
)

// Default SOCKS5 listener / DNS for generated client configs.
const (
	defaultClientSOCKSHost = "127.0.0.1"
	defaultClientSOCKSPort = 8808
	defaultClientDNS       = "8.8.8.8:53"
)

// clientConfigTemplate renders a clean, ready-to-run client YAML. It is built
// from a server config so the two ends are guaranteed compatible. Optional
// blocks are emitted only when the corresponding server fields are set.
//nolint:gochecknoglobals // immutable compiled template, parsed once at init
var clientConfigTemplate = template.Must(template.New("client").Parse(
	`mode: cnc
auth:
  provider: {{.Provider}}
room:
  id: "{{.RoomID}}"
{{- if .Channel}}
  channel: "{{.Channel}}"
{{- end}}
crypto:
{{- if .Key}}
  key: "{{.Key}}"
{{- end}}
{{- if .KeyFile}}
  key_file: "{{.KeyFile}}"
{{- end}}
{{- if .EngineName}}
engine:
  name: {{.EngineName}}
{{- if .EngineURL}}
  url: "{{.EngineURL}}"
{{- end}}
{{- end}}
net:
  transport: {{.Transport}}
  dns: "{{.DNS}}"
socks:
  host: "{{.SOCKSHost}}"
  port: {{.SOCKSPort}}
{{- if .LivenessInterval}}
liveness:
  interval: {{.LivenessInterval}}
  timeout: {{.LivenessTimeout}}
  failures: {{.LivenessFailures}}
{{- end}}
{{- if .CoverEnabled}}
cover:
  enabled: true
  interval: {{.CoverInterval}}
  size: {{.CoverSize}}
{{- end}}
access:
  token: "{{.Token}}"
data: data
`))

// clientTemplateData is the flattened view the template renders.
type clientTemplateData struct {
	Provider                          string
	RoomID, Channel                   string
	Key, KeyFile                      string
	EngineName, EngineURL             string
	Transport, DNS                    string
	SOCKSHost                         string
	SOCKSPort                         int
	LivenessInterval, LivenessTimeout string
	LivenessFailures                  int
	CoverEnabled                      bool
	CoverInterval                     string
	CoverSize                         int
	Token                             string
}

// GenerateClientConfig renders a ready-to-run client (cnc) YAML from a loaded
// server (srv) config plus an access token. The fields that must match between
// the two ends -- provider, room, crypto key, transport, cover settings -- are
// copied verbatim, so the generated client is guaranteed compatible with the
// server it was minted from. Client-local concerns (SOCKS5 listener, DNS) get
// sensible defaults; server-only concerns (access registry, outbound proxy,
// carrier engine token) are intentionally omitted.
func GenerateClientConfig(server File, token string) ([]byte, error) {
	dns := server.Net.DNS
	if dns == "" {
		dns = defaultClientDNS
	}
	data := clientTemplateData{
		Provider:         server.Auth.Provider,
		RoomID:           server.Room.ID,
		Channel:          server.Room.Channel,
		Key:              server.Crypto.Key,
		KeyFile:          server.Crypto.KeyFile,
		EngineName:       server.Engine.Name,
		EngineURL:        server.Engine.URL,
		Transport:        server.Net.Transport,
		DNS:              dns,
		SOCKSHost:        defaultClientSOCKSHost,
		SOCKSPort:        defaultClientSOCKSPort,
		LivenessInterval: server.Liveness.Interval,
		LivenessTimeout:  server.Liveness.Timeout,
		LivenessFailures: server.Liveness.Failures,
		CoverEnabled:     server.Cover.Enabled,
		CoverInterval:    server.Cover.Interval,
		CoverSize:        server.Cover.Size,
		Token:            token,
	}
	var buf bytes.Buffer
	if err := clientConfigTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render client config: %w", err)
	}
	return buf.Bytes(), nil
}
