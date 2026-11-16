package cli

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

var reqIDShape = regexp.MustCompile(`req-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

// Every call of one command, the token request and the retry after a 401
// included, carries the same req-<uuid4>, and a failing command prints it.
func TestRequestID_OneIDOnEveryCallAndInTheError(t *testing.T) {
	k := &keystoneFleet{}
	startFleet(t, k)
	var once sync.Once
	k.listCode = func(n int64) int {
		if n == 1 {
			once.Do(k.expireAll) // the token dies: re-auth and retry
		}
		return 0
	}

	out, code := runCaptured(t, "server", "list")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	k.mu.Lock()
	ids := append([]string(nil), k.reqIDs...)
	k.mu.Unlock()
	if len(ids) < 2 {
		t.Fatalf("expected the token request and the list, got %v", ids)
	}
	if !reqIDShape.MatchString(ids[0]) {
		t.Fatalf("not a req-<uuid4>: %q", ids[0])
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Errorf("call %d carried %q, want the one id %q (all %v)", i, id, ids[0], ids)
		}
	}

	// A second command is a new invocation: a new id.
	_, _ = runCaptured(t, "server", "list")
	k.mu.Lock()
	last := k.reqIDs[len(k.reqIDs)-1]
	k.mu.Unlock()
	if last == ids[0] {
		t.Errorf("a second command reused the id %q", last)
	}
}

func TestRequestID_FlagAndEnvOverride(t *testing.T) {
	const flagID = "req-11111111-2222-4333-8444-555555555555"
	const envID = "req-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	k := &keystoneFleet{}
	startFleet(t, k)

	if out, code := runCaptured(t, "server", "list", "--request-id", flagID); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	assertAllIDs(t, k, flagID)

	k.mu.Lock()
	k.reqIDs = nil
	k.mu.Unlock()
	t.Setenv("OS_REQUEST_ID", envID)
	if out, code := runCaptured(t, "server", "list"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	assertAllIDs(t, k, envID)

	// The flag wins over the environment.
	k.mu.Lock()
	k.reqIDs = nil
	k.mu.Unlock()
	if out, code := runCaptured(t, "server", "list", "--request-id", flagID); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	assertAllIDs(t, k, flagID)
}

func assertAllIDs(t *testing.T, k *keystoneFleet, want string) {
	t.Helper()
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.reqIDs) == 0 {
		t.Fatal("no request reached the fake cloud")
	}
	for i, id := range k.reqIDs {
		if id != want {
			t.Errorf("call %d carried %q, want %q", i, id, want)
		}
	}
}

func TestRequestID_InvalidIsRefused(t *testing.T) {
	k := &keystoneFleet{}
	startFleet(t, k)

	for _, bad := range []string{"abc", "req-123", "REQ-11111111-2222-4333-8444-555555555555", "req-11111111-2222-4333-8444-55555555555g"} {
		out, code := runCaptured(t, "server", "list", "--request-id", bad)
		if code != ExitUsage || !strings.Contains(out, "invalid request id") {
			t.Errorf("--request-id %q: exit %d, out %q", bad, code, out)
		}
	}
	t.Setenv("OS_REQUEST_ID", "not-an-id")
	if out, code := runCaptured(t, "server", "list"); code != ExitUsage || !strings.Contains(out, "invalid request id") {
		t.Errorf("OS_REQUEST_ID: exit %d, out %q", code, out)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.reqIDs) != 0 {
		t.Errorf("a refused id still reached the cloud: %v", k.reqIDs)
	}
}

// A command that fails against the API names the id it sent, and the id is the
// one the cloud received.
func TestRequestID_PrintedOnError(t *testing.T) {
	k := &keystoneFleet{}
	k.listCode = func(int64) int { return 0 }
	startFleet(t, k)
	k.refuseAuth()

	out, code := runCaptured(t, "server", "list")
	if code != ExitFailure {
		t.Fatalf("exit %d, want %d\n%s", code, ExitFailure, out)
	}
	m := reqIDShape.FindString(out)
	if m == "" || !strings.Contains(out, "request id: "+m) {
		t.Fatalf("the error does not name its request id:\n%s", out)
	}
	assertAllIDs(t, k, m)
}

func TestExitStatus_RequestIDLine(t *testing.T) {
	root := &cobra.Command{Use: "koc"}
	leaf := &cobra.Command{Use: "x", SilenceUsage: true}
	root.AddCommand(leaf)

	if _, msg := ExitStatus(leaf, errors.New("boom")); msg != "koc: boom" {
		t.Errorf("no id yet: %q", msg)
	}
	root.Annotations = map[string]string{RequestIDAnnotation: "req-1"}
	if _, msg := ExitStatus(leaf, errors.New("boom")); msg != "koc: boom\nrequest id: req-1" {
		t.Errorf("with id: %q", msg)
	}
}

// --debug names the id up front, and the dump shows the header that goes out.
func TestRequestID_InDebug(t *testing.T) {
	startFleet(t, &keystoneFleet{})
	const id = "req-11111111-2222-4333-8444-555555555555"
	stderr := captureStderr(t, func() { runCaptured(t, "server", "list", "--debug", "--request-id", id) })
	if !strings.Contains(stderr, "request id: "+id) {
		t.Errorf("--debug did not name the id:\n%s", stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "x-openstack-request-id: "+id) {
		t.Errorf("--debug dump lacks the header:\n%s", stderr)
	}
}
