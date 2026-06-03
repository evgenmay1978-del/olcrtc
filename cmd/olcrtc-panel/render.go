package main

import (
	_ "embed"
	"html/template"
	"net/http"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/access"
)

// indexData is the view model for the panel page.
type indexData struct {
	Clients []clientRow
	Notice  string
}

type clientRow struct {
	Label    string
	Status   string
	Disabled bool
	Expires  string
	DaysLeft string
	Contact  string
	Token    string
}

//go:embed index.gohtml
var indexHTML string

// indexTmpl renders the whole panel. The template lives in index.gohtml and is
// embedded into the binary, so the panel ships as a single self-contained
// executable. html/template auto-escapes all interpolations.
//
//nolint:gochecknoglobals // compiled template, parsed once at init
var indexTmpl = template.Must(template.New("index").Parse(indexHTML))


// tmplView is what indexTmpl actually receives (adds IsErr without leaking it
// into indexData's public shape).
type tmplView struct {
	indexData
	IsErr bool
}

// renderIndexErr writes the panel page with the notice styled as an error.
func renderIndexErr(w http.ResponseWriter, clients []access.Client, notice string) {
	renderWith(w, clients, notice, true)
}

// renderIndex writes the panel page with an optional info banner.
func renderIndex(w http.ResponseWriter, clients []access.Client, notice string) {
	renderWith(w, clients, notice, false)
}

func renderWith(w http.ResponseWriter, clients []access.Client, notice string, isErr bool) {
	rows := make([]clientRow, 0, len(clients))
	now := time.Now()
	for _, c := range sortedByLabel(clients) {
		status := c.Status
		if status == "" {
			status = access.StatusActive
		}
		expires, daysLeft := "никогда", "—"
		if !c.Expires.IsZero() {
			expires = c.Expires.Format("2006-01-02 15:04")
			d := int(time.Until(c.Expires).Hours() / 24)
			if c.Expires.After(now) {
				daysLeft = itoa(d) + " дн."
			} else {
				daysLeft = "истёк"
			}
		}
		rows = append(rows, clientRow{
			Label: c.Label, Status: status, Disabled: c.Disabled,
			Expires: expires, DaysLeft: daysLeft, Contact: c.Contact, Token: c.Token,
		})
	}
	view := tmplView{indexData: indexData{Clients: rows, Notice: notice}, IsErr: isErr}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, view)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
