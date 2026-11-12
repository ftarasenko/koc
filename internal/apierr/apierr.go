// Package apierr renders a failed command's error as the one line koc prints.
//
// gophercloud's ErrUnexpectedResponseCode spells out the whole request and the
// raw response body, which for most OpenStack services is a JSON envelope around
// the one sentence the operator needs. Message finds those errors anywhere in an
// error chain, replaces each with that sentence and the HTTP status, and leaves
// the context the command wrapped around it alone.
package apierr

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"golang.org/x/term"
)

// maxBody is how many runes of an unrecognised response body a line carries
// before --debug is needed to see all of it.
const maxBody = 500

// Options selects how Message renders.
type Options struct {
	// Debug adds the request method and URL path (never the host or query)
	// and the server's request id to the status.
	Debug bool
	// Color styles the status and hints with ANSI sequences.
	Color bool
}

// UseColor reports whether w is a terminal that should be styled: NO_COLOR
// (set and non-empty, per no-color.org) and TERM=dumb switch it off.
func UseColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && colorWanted(term.IsTerminal(int(f.Fd())))
}

// colorWanted is the environment half of UseColor.
func colorWanted(tty bool) bool {
	return tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

// Prefix is the "koc: " every error line starts with.
func Prefix(color bool) string {
	return paint(color, "1;31", "koc:") + " "
}

func paint(on bool, code, s string) string {
	if !on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Message renders err. An error with no API response in its chain comes back
// as err.Error(); one with a response has each response replaced by
// "<service message> (HTTP <status>)", and known cases get a "hint:" line.
func Message(err error, o Options) string {
	full := err.Error()
	var b strings.Builder
	var hints []string
	pos := 0
	for _, e := range collect(err, nil) {
		raw := e.Error()
		i := strings.Index(full[pos:], raw)
		if i < 0 {
			continue
		}
		i += pos
		b.WriteString(full[pos:i])
		text := bodyMessage(e.Body, o.Debug)
		if text == "" {
			text = strings.ToLower(http.StatusText(e.Actual))
			if text == "" {
				text = "request failed"
			}
		}
		b.WriteString(text + " " + paint(o.Color, "2", "("+status(e, o.Debug)+")"))
		if h := hint(text); h != "" {
			hints = append(hints, h)
		}
		pos = i + len(raw)
	}
	b.WriteString(full[pos:])
	for _, h := range hints {
		b.WriteString("\n" + paint(o.Color, "36", "hint:") + " " + h)
	}
	return b.String()
}

// status is "HTTP 400", with the method, path and request id under debug.
func status(e gophercloud.ErrUnexpectedResponseCode, debug bool) string {
	s := fmt.Sprintf("HTTP %d", e.Actual)
	if !debug {
		return s
	}
	if e.Method != "" || e.URL != "" {
		s += ", " + strings.TrimSpace(e.Method+" "+urlPath(e.URL))
	}
	for _, h := range []string{"X-Openstack-Request-Id", "X-Compute-Request-Id"} {
		if id := e.ResponseHeader.Get(h); id != "" {
			s += ", request-id " + id
			break
		}
	}
	return s
}

// urlPath is raw's path without scheme, host or query.
func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.EscapedPath()
}

// collect gathers every API response error in err's tree, in the order their
// text appears in err.Error().
func collect(err error, out []gophercloud.ErrUnexpectedResponseCode) []gophercloud.ErrUnexpectedResponseCode {
	if err == nil {
		return out
	}
	switch e := err.(type) { //nolint:errorlint // the tree is walked by hand below, including types that do not unwrap
	case gophercloud.ErrUnexpectedResponseCode:
		return append(out, e)
	case *gophercloud.ErrUnexpectedResponseCode:
		return append(out, *e)
	case gophercloud.ErrUnableToReauthenticate:
		return collect(e.ErrReauth, collect(e.ErrOriginal, out))
	case *gophercloud.ErrUnableToReauthenticate:
		return collect(e.ErrReauth, collect(e.ErrOriginal, out))
	}
	switch e := err.(type) { //nolint:errorlint // same walk: only the Unwrap shape matters
	case interface{ Unwrap() error }:
		return collect(e.Unwrap(), out)
	case interface{ Unwrap() []error }:
		for _, c := range e.Unwrap() {
			out = collect(c, out)
		}
	}
	return out
}
