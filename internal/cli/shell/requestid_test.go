package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ftarasenko/koc/internal/auth"
)

const (
	idA = "req-11111111-1111-4111-8111-111111111111"
	idB = "req-22222222-2222-4222-8222-222222222222"
)

func writeID(t *testing.T, path, id string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func auditIDs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, ev := range readEvents(t, string(raw)) {
		id, _ := ev["request_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// A line uses the id the host left in KOC_REQUEST_ID_FILE, once; the next line
// takes the next id the host wrote, or mints its own when it wrote none. Every
// event, built-ins and refused lines included, carries the id.
func TestRequestID_FileIDPerLine(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "request_id")
	audit := filepath.Join(dir, "audit.jsonl")
	t.Setenv(EnvAuditFD, "")
	t.Setenv(EnvAuditFile, audit)
	t.Setenv(EnvRequestIDFile, idFile)
	t.Setenv("OS_REQUEST_ID", "")
	writeID(t, idFile, idA)

	k := &fakeKoc{}
	s := newSession(t, k, strings.NewReader("server list\rscope\rserver list --long\r"))
	s.run(t)

	ids := auditIDs(t, audit)
	if len(ids) != 3 {
		t.Fatalf("%d events, want 3", len(ids))
	}
	if ids[0] != idA {
		t.Errorf("line 1 ran with %q, want the file's %q", ids[0], idA)
	}
	for i, id := range ids {
		if !auth.ValidRequestID(id) {
			t.Errorf("event %d request_id %q is not a request id", i, id)
		}
	}
	// The file was not refreshed: lines 2 and 3 must not reuse idA.
	if ids[1] == idA || ids[2] == idA || ids[1] == ids[2] {
		t.Errorf("a stale file's id was reused: %v", ids)
	}
	// The command ran with the id it audited.
	if got := k.ids; len(got) != 2 || got[0] != idA || got[1] != ids[2] {
		t.Errorf("commands ran with %v; audit says %v", got, ids)
	}
}

// A new id in the file is picked up on the next line.
func TestRequestID_RefreshedFileIsUsed(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "request_id")
	t.Setenv(EnvRequestIDFile, idFile)
	t.Setenv("OS_REQUEST_ID", "")
	k := &fakeKoc{}
	s := newSession(t, k, strings.NewReader(""))

	writeID(t, idFile, idA)
	if got := s.sh.lineRequestID(); got != idA {
		t.Fatalf("got %q, want %q", got, idA)
	}
	if got := s.sh.lineRequestID(); got == idA {
		t.Fatal("the same file id was used for two lines")
	}
	writeID(t, idFile, idB)
	if got := s.sh.lineRequestID(); got != idB {
		t.Fatalf("got %q, want the refreshed %q", got, idB)
	}
}

// A file that is missing, empty or malformed is ignored: the line mints its own.
func TestRequestID_BadFileMintsOwn(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "request_id")
	t.Setenv(EnvRequestIDFile, idFile)
	t.Setenv("OS_REQUEST_ID", "")
	s := newSession(t, &fakeKoc{}, strings.NewReader(""))
	for name, content := range map[string]string{
		"missing":   "",
		"malformed": "not-an-id\n",
		"upper":     strings.ToUpper(idA) + "\n",
		"two lines": idA + "\n" + idB + "\n",
	} {
		_ = os.Remove(idFile)
		if name != "missing" {
			if err := os.WriteFile(idFile, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got := s.sh.lineRequestID()
		if !auth.ValidRequestID(got) || got == idA || got == idB {
			t.Errorf("%s: got %q, want a freshly minted id", name, got)
		}
	}
}

// OS_REQUEST_ID still pins the id when no file is set, and the file wins over it.
func TestRequestID_EnvAndFile(t *testing.T) {
	t.Setenv(EnvRequestIDFile, "")
	t.Setenv("OS_REQUEST_ID", idB)
	s := newSession(t, &fakeKoc{}, strings.NewReader(""))
	if got := s.sh.lineRequestID(); got != idB {
		t.Fatalf("env id: got %q, want %q", got, idB)
	}
	idFile := filepath.Join(t.TempDir(), "request_id")
	writeID(t, idFile, idA)
	t.Setenv(EnvRequestIDFile, idFile)
	if got := s.sh.lineRequestID(); got != idA {
		t.Fatalf("file id: got %q, want %q over the env's", got, idA)
	}
}

// An id typed on the line wins, and it is the one audited.
func TestRequestID_FlagOnTheLineWins(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "request_id")
	audit := filepath.Join(dir, "audit.jsonl")
	t.Setenv(EnvAuditFD, "")
	t.Setenv(EnvAuditFile, audit)
	t.Setenv(EnvRequestIDFile, idFile)
	t.Setenv("OS_REQUEST_ID", "")
	writeID(t, idFile, idA)
	k := &fakeKoc{}
	s := newSession(t, k, strings.NewReader("server list --request-id "+idB+"\r"))
	s.run(t)
	if ids := auditIDs(t, audit); len(ids) != 1 || ids[0] != idB {
		t.Fatalf("audited %v, want [%s]", ids, idB)
	}
}
