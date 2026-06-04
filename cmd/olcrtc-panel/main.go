// Package main provides olcrtc-panel, a small web UI for managing client
// access to an olcrtc server: create clients, set their subscription length in
// days, disable/enable, rotate tokens, and delete. It edits the same JSON
// client registry the server reads (access.clients_file), so changes take
// effect without restarting the server.
//
// The panel binds to a local address by default and is protected by HTTP basic
// auth. Expose it only over a trusted channel (SSH tunnel or an authenticated
// reverse proxy); it must not be open to the internet unauthenticated.
//
// Usage:
//
//	OLCRTC_PANEL_PASSWORD=secret olcrtc-panel \
//	  -registry /var/lib/olcrtc/clients.json -addr 127.0.0.1:8090
package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	defaultAddr     = "127.0.0.1:8090"
	defaultRegistry = "/var/lib/olcrtc/clients.json"
	readTimeout     = 10 * time.Second
	writeTimeout    = 10 * time.Second
)

// errNoPassword is returned when the panel is started without a password.
var errNoPassword = errors.New("set OLCRTC_PANEL_PASSWORD to protect the panel")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "olcrtc-panel:", err)
		os.Exit(1)
	}
}

// flags holds the panel's parsed command-line options.
type flags struct {
	addr         string
	registry     string
	serverConfig string
}

// parseArgs reads -addr/-registry/-server, falling back to defaults.
func parseArgs(args []string) flags {
	f := flags{addr: defaultAddr, registry: defaultRegistry}
	targets := map[string]*string{
		"-addr": &f.addr, "--addr": &f.addr,
		"-registry": &f.registry, "--registry": &f.registry,
		"-server": &f.serverConfig, "--server": &f.serverConfig,
	}
	for i := 0; i < len(args); i++ {
		if dst, ok := targets[args[i]]; ok && i+1 < len(args) {
			*dst = args[i+1]
			i++
		}
	}
	return f
}

func run() error {
	f := parseArgs(os.Args[1:])
	addr, registry, serverConfig := f.addr, f.registry, f.serverConfig

	password := os.Getenv("OLCRTC_PANEL_PASSWORD")
	if password == "" {
		return errNoPassword
	}
	user := os.Getenv("OLCRTC_PANEL_USER")
	if user == "" {
		user = "admin"
	}

	srv := newServer(registry, user, password)
	srv.serverConfig = serverConfig
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv.routes(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}
	fmt.Fprintf(os.Stderr, "olcrtc-panel listening on http://%s (user %q)\n", addr, user)
	if err := httpSrv.ListenAndServe(); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

// requireAuth wraps h with HTTP basic auth, comparing credentials in constant
// time so the panel does not leak the password through timing.
func (s *server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(u), []byte(s.user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(p), []byte(s.password)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="olcrtc-panel"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}
