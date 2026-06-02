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
//	prune                             auto-reject pending requests past their deadline
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
)

// cmdPay is the only command that needs no registry.
const cmdPay = "pay"

// errUsage signals a usage problem; main prints it without a stack.
var errUsage = errors.New("usage: olcrtc-admin -registry <clients.json> <command> [args] " +
	"(commands: list, grant, request, approve, reject, revoke, enable, remove, prune, pay)")

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

func run(args []string, out io.Writer) error {
	registry, payInfo, rest := parseFlags(args)
	if len(rest) == 0 {
		return errUsage
	}
	cmd, cmdArgs := rest[0], rest[1:]

	// pay only prints instructions; it needs no registry.
	if cmd == cmdPay {
		return printPayInfo(payInfo, out)
	}
	if registry == "" {
		return errUsage
	}

	store, err := access.OpenStore(registry)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
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
	"prune":   func(s *access.Store, _ []string, o io.Writer) error { return prune(s, o) },
}

func dispatch(cmd string, store *access.Store, args []string, out io.Writer) error {
	handler, ok := commands[cmd]
	if !ok {
		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
	return handler(store, args, out)
}

// parseFlags pulls -registry and -pay-info out of args, returning the rest as
// the positional command + args. A tiny hand-rolled parser keeps the flags
// usable in any position without pulling in the flag package's global state.
func parseFlags(args []string) (string, string, []string) {
	var (
		registry string
		payInfo  string
		rest     []string
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-registry", "--registry":
			if i+1 < len(args) {
				registry = args[i+1]
				i++
			}
		case "-pay-info", "--pay-info":
			if i+1 < len(args) {
				payInfo = args[i+1]
				i++
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return registry, payInfo, rest
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
