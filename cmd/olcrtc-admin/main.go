// Package main provides the olcrtc-admin CLI for managing client access to an
// olcrtc server: issuing free grants, handling self-service paid signups
// (pending -> approve/reject), and revoking access.
//
// It edits the same JSON client registry the server reads (access.clients_file
// in the server YAML), so changes take effect without restarting the server.
//
// Usage:
//
//	olcrtc-admin -registry clients.json <command> [args]
//
// Commands:
//
//	list                              show all clients and their status
//	grant   <label> [ttl] [contact]   create an ACTIVE client (free access)
//	request <label> [deadline] [contact]  create a PENDING client (awaiting payment;
//	                                        deadline auto-rejects via `prune`)
//	approve <label> [ttl]             activate a pending client (payment confirmed)
//	reject  <label>                   reject a pending client (payment not received)
//	revoke  <label>                   disable an existing client
//	enable  <label>                   re-enable a disabled client
//	remove  <label>                   delete a client
//	rotate  <label>                   issue a fresh token (e.g. after a leak), keeping the subscription
//	prune                             auto-reject pending requests past their deadline
//	client-config <label> -server <server.yaml>
//	                                  print a ready-to-run client YAML for the client
//	usage -usage <usage.json>         print per-session traffic usage for billing
//	pay                               print payment instructions (from -pay-info file)
//
// ttl is a Go duration like 720h (30 days). Omit for no expiry.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/accounting"
	"github.com/openlibrecommunity/olcrtc/internal/config"
)

// cmdPay is the only command that needs no registry.
const cmdPay = "pay"

// errUsage signals a usage problem; main prints it without a stack.
var errUsage = errors.New("usage: olcrtc-admin -registry <clients.json> <command> [args] " +
	"(commands: list, grant, request, approve, reject, revoke, enable, remove, rotate, prune, client-config, usage, pay)")

// printf writes formatted output, ignoring write errors (stdout to a terminal
// or pipe; a failed write here is not actionable).
func printf(out io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(out, format, a...)
}

func writeLine(out io.Writer, a ...any) {
	_, _ = fmt.Fprintln(out, a...)
}

// errMissingLabel is returned by commands that require a client label argument.
var errMissingLabel = errors.New("command needs a client label")

// errNoPayInfo is returned when `pay` is run without -pay-info.
var errNoPayInfo = errors.New("pay: pass -pay-info <file> pointing at your (uncommitted) payment details")

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// cmdClientConfig generates a ready-to-run client YAML for an existing client.
const cmdClientConfig = "client-config"

// cmdUsage prints traffic usage; it reads the usage file, not the registry.
const cmdUsage = "usage"

// errNoServerConfig is returned when client-config is run without -server.
var errNoServerConfig = errors.New("client-config: pass -server <server.yaml> to mint a matching client config")

// errNoUsageFile is returned when usage is run without -usage.
var errNoUsageFile = errors.New("usage: pass -usage <usage.json> (the server's access.usage_file)")

func run(args []string, out io.Writer) error {
	flags := parseFlags(args)
	if len(flags.rest) == 0 {
		return errUsage
	}
	cmd, cmdArgs := flags.rest[0], flags.rest[1:]

	// pay and usage need no registry.
	if cmd == cmdPay {
		return printPayInfo(flags.payInfo, out)
	}
	if cmd == cmdUsage {
		return showUsage(flags.usage, out)
	}
	if flags.registry == "" {
		return errUsage
	}

	store, err := access.OpenStore(flags.registry)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}

	// client-config needs the server YAML too, so it is handled here rather
	// than via the fixed-arity command table.
	if cmd == cmdClientConfig {
		return clientConfig(store, flags.server, cmdArgs, out)
	}
	return dispatch(cmd, store, cmdArgs, out)
}

// commandFunc handles one admin subcommand.
type commandFunc func(store *access.Store, args []string, out io.Writer) error

// commands maps subcommand names to handlers. Wrapping the fixed-arity helpers
// keeps dispatch a simple table lookup (no large switch).
//
//nolint:gochecknoglobals // immutable command table, initialized once
var commands = map[string]commandFunc{
	"list":    func(s *access.Store, _ []string, o io.Writer) error { return listClients(s, o) },
	"grant":   grant,
	"request": request,
	"approve": approve,
	"reject":  func(s *access.Store, a []string, o io.Writer) error { return setStatus(s, a, access.StatusRejected, o) },
	"revoke":  func(s *access.Store, a []string, o io.Writer) error { return setDisabled(s, a, true, o) },
	"enable":  func(s *access.Store, a []string, o io.Writer) error { return setDisabled(s, a, false, o) },
	"remove":  remove,
	"rotate":  rotate,
	"prune":   func(s *access.Store, _ []string, o io.Writer) error { return prune(s, o) },
}

func dispatch(cmd string, store *access.Store, args []string, out io.Writer) error {
	handler, ok := commands[cmd]
	if !ok {
		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
	return handler(store, args, out)
}

// adminFlags holds the parsed -registry/-pay-info/-server flags plus the
// remaining positional command and its arguments.
type adminFlags struct {
	registry string
	payInfo  string
	server   string
	usage    string
	rest     []string
}

// flagTargets maps a parsed adminFlags to the string field each named flag
// fills, so parseFlags stays a simple loop instead of a large switch.
func (f *adminFlags) flagTargets() map[string]*string {
	return map[string]*string{
		"-registry": &f.registry, "--registry": &f.registry,
		"-pay-info": &f.payInfo, "--pay-info": &f.payInfo,
		"-server": &f.server, "--server": &f.server,
		"-usage": &f.usage, "--usage": &f.usage,
	}
}

// parseFlags pulls the named flags out of args, returning the rest as the
// positional command + args. A tiny hand-rolled parser keeps the flags usable
// in any position without pulling in the flag package's global state.
func parseFlags(args []string) adminFlags {
	var f adminFlags
	targets := f.flagTargets()
	for i := 0; i < len(args); i++ {
		if dst, ok := targets[args[i]]; ok {
			if i+1 < len(args) {
				*dst = args[i+1]
				i++
			}
			continue
		}
		f.rest = append(f.rest, args[i])
	}
	return f
}

// showUsage prints the server's persisted traffic usage, one row per session,
// for volume billing. The usage file is the server's access.usage_file.
func showUsage(usagePath string, out io.Writer) error {
	if usagePath == "" {
		return errNoUsageFile
	}
	records, err := accounting.ReadRecords(usagePath)
	if err != nil {
		return fmt.Errorf("read usage: %w", err)
	}
	if len(records) == 0 {
		writeLine(out, "no usage recorded yet")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SESSION\tDEVICE\tSTREAMS\tIN\tOUT\tTOTAL")
	var totalIn, totalOut uint64
	for _, r := range records {
		totalIn += r.BytesIn
		totalOut += r.BytesOut
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			r.SessionID, r.DeviceID, r.TotalStreams,
			humanBytes(r.BytesIn), humanBytes(r.BytesOut), humanBytes(r.BytesIn+r.BytesOut))
	}
	_, _ = fmt.Fprintf(w, "TOTAL\t\t\t%s\t%s\t%s\n",
		humanBytes(totalIn), humanBytes(totalOut), humanBytes(totalIn+totalOut))
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

// humanBytes formats a byte count with a binary unit suffix.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGT"[exp])
}

// clientConfig prints a ready-to-run client YAML for an existing client,
// minted from the server config so the two ends are guaranteed compatible.
func clientConfig(store *access.Store, serverPath string, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: client-config", errMissingLabel)
	}
	if serverPath == "" {
		return errNoServerConfig
	}
	c, ok := store.Lookup(args[0])
	if !ok {
		return fmt.Errorf("%w: %q", access.ErrClientNotFound, args[0])
	}
	server, err := config.Load(serverPath)
	if err != nil {
		return fmt.Errorf("load server config: %w", err)
	}
	yamlBytes, err := config.GenerateClientConfig(server, c.Token)
	if err != nil {
		return fmt.Errorf("generate client config: %w", err)
	}
	printf(out, "# olcrtc client config for %q — save as client.yaml and run: olcrtc client.yaml\n", c.Label)
	_, _ = out.Write(yamlBytes)
	return nil
}

func listClients(store *access.Store, out io.Writer) error {
	clients := store.Clients()
	if len(clients) == 0 {
		writeLine(out, "no clients")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "LABEL\tSTATUS\tDISABLED\tEXPIRES\tCONTACT\tTOKEN")
	for _, c := range clients {
		status := c.Status
		if status == "" {
			status = access.StatusActive
		}
		expires := "never"
		if !c.Expires.IsZero() {
			expires = c.Expires.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\n",
			c.Label, status, c.Disabled, expires, c.Contact, c.Token)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

func grant(store *access.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: grant", errMissingLabel)
	}
	ttl, contact := parseTTLAndContact(args[1:])
	token, err := store.Add(args[0], contact, access.StatusActive, ttl)
	if err != nil {
		return fmt.Errorf("add client: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "granted active access to %q\ntoken: %s\n", args[0], token)
	return nil
}

func request(store *access.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: request", errMissingLabel)
	}
	// Optional [deadline] [contact]: a duration sets a payment deadline after
	// which `prune` auto-rejects the unpaid request; anything else is contact.
	deadline, contact := parseTTLAndContact(args[1:])
	token, err := store.Add(args[0], contact, access.StatusPending, deadline)
	if err != nil {
		return fmt.Errorf("add client: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "created pending client %q (awaiting payment approval)\ntoken: %s\n", args[0], token)
	return nil
}

func prune(store *access.Store, out io.Writer) error {
	rejected := store.PruneExpiredPending()
	if len(rejected) == 0 {
		writeLine(out, "no expired pending requests")
		return nil
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	for _, label := range rejected {
		printf(out, "auto-rejected expired pending %q\n", label)
	}
	return nil
}

func approve(store *access.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: approve", errMissingLabel)
	}
	ttl, _ := parseTTLAndContact(args[1:])
	if err := store.SetStatus(args[0], access.StatusActive, ttl); err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "approved %q (payment confirmed)\n", args[0])
	return nil
}

func setStatus(store *access.Store, args []string, status string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: %s", errMissingLabel, status)
	}
	if err := store.SetStatus(args[0], status, 0); err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "set %q -> %s\n", args[0], status)
	return nil
}

func setDisabled(store *access.Store, args []string, disabled bool, out io.Writer) error {
	if len(args) == 0 {
		return errMissingLabel
	}
	if err := store.SetDisabled(args[0], disabled); err != nil {
		return fmt.Errorf("set disabled: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	verb := "enabled"
	if disabled {
		verb = "revoked"
	}
	printf(out, "%s %q\n", verb, args[0])
	return nil
}

func remove(store *access.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: remove", errMissingLabel)
	}
	if err := store.Remove(args[0]); err != nil {
		return fmt.Errorf("remove client: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "removed %q\n", args[0])
	return nil
}

func rotate(store *access.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: rotate", errMissingLabel)
	}
	token, err := store.Rotate(args[0])
	if err != nil {
		return fmt.Errorf("rotate token: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	printf(out, "rotated token for %q (old token no longer works)\ntoken: %s\n", args[0], token)
	return nil
}

// parseTTLAndContact interprets optional [ttl] [contact] trailing args. A ttl
// is any value time.ParseDuration accepts; anything else is treated as contact.
func parseTTLAndContact(args []string) (time.Duration, string) {
	var (
		ttl     time.Duration
		contact string
	)
	for _, a := range args {
		if d, err := time.ParseDuration(a); err == nil {
			ttl = d
			continue
		}
		contact = a
	}
	return ttl, contact
}

// printPayInfo prints the contents of the admin's payment-instructions file.
// The file is intentionally NOT part of the repository: it holds the admin's
// personal phone number / bank details, which must not be committed to git.
func printPayInfo(path string, out io.Writer) error {
	if path == "" {
		return errNoPayInfo
	}
	data, err := os.ReadFile(path) //nolint:gosec // admin-supplied path to their own pay-info file
	if err != nil {
		return fmt.Errorf("read pay-info: %w", err)
	}
	_, _ = fmt.Fprint(out, string(data))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		writeLine(out)
	}
	return nil
}
