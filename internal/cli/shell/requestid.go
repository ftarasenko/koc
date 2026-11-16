package shell

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ftarasenko/koc/internal/auth"
	"github.com/ftarasenko/koc/internal/kochome"
)

// RequestIDAnnotation is the root-command annotation that carries a line's
// global request id from PersistentPreRunE to the error printer and the audit
// event.
const RequestIDAnnotation = "koc.request-id"

// EnvRequestIDFile names a file the host rewrites with the id of the next
// line. One shell runs many lines, so an environment variable cannot carry a
// per-line id; the host writes a `req-<uuid>` into this file after each audited
// line and koc reads it before the next one.
const EnvRequestIDFile = "KOC_REQUEST_ID_FILE"

// lineRequestID picks the id of the line about to run: the one in
// KOC_REQUEST_ID_FILE when it is valid and not used yet, else OS_REQUEST_ID
// when valid, else a fresh one. Every line, built-ins and refused lines
// included, gets one for its audit event.
func (s *Shell) lineRequestID() string {
	if p := os.Getenv(EnvRequestIDFile); p != "" {
		if b, err := kochome.ReadFile(kochome.Resolve(p)); err == nil {
			id := strings.TrimSpace(string(b))
			if auth.ValidRequestID(id) && id != s.lastRequestID {
				s.lastRequestID = id
				return id
			}
		}
	}
	if id := os.Getenv("OS_REQUEST_ID"); auth.ValidRequestID(id) {
		return id
	}
	return auth.NewRequestID()
}

// applyRequestID gives a line's tree its id as the --request-id flag's value,
// unmarked as changed, so an id typed on the line still wins.
func (s *Shell) applyRequestID(root *cobra.Command, rec *lineRecord) {
	if f := root.PersistentFlags().Lookup("request-id"); f != nil {
		_ = f.Value.Set(rec.requestID)
	}
}
