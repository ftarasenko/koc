package apierr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
)

const (
	nodeID      = "11111111-2222-3333-4444-555555555555"
	ironicInner = `{\"faultcode\": \"Client\", \"faultstring\": \"The requested action \\\"abort\\\" can not be performed on node \\\"` + nodeID + `\\\" while it is in state \\\"wait call-back\\\".\", \"debuginfo\": null}`
	ironicBody  = `{"error_message": "` + ironicInner + `"}`
	ironicText  = `The requested action "abort" can not be performed on node "` + nodeID + `" while it is in state "wait call-back".`
)

func apiError(method, path string, code int, body string) gophercloud.ErrUnexpectedResponseCode {
	return gophercloud.ErrUnexpectedResponseCode{
		URL: "https://cloud.example.test:6385" + path, Method: method,
		Expected: []int{202}, Actual: code, Body: []byte(body),
		ResponseHeader: http.Header{"X-Openstack-Request-Id": {"req-0001"}},
	}
}

func TestMessage_IronicAbort(t *testing.T) {
	raw := apiError("PUT", "/v1/nodes/lab-node-1/states/provision", 400, ironicBody)
	err := fmt.Errorf("abort node lab-node-1: %w", raw)
	hint := "this Ironic does not abort a deploy that waits for its call-back; tear it down instead: koc baremetal node undeploy " + nodeID

	if !strings.Contains(err.Error(), "Expected HTTP response code") {
		t.Fatal("fixture does not reproduce gophercloud's raw error")
	}
	for _, tc := range []struct {
		name string
		o    Options
		want string
	}{
		{"plain", Options{}, "abort node lab-node-1: " + ironicText + " (HTTP 400)\nhint: " + hint},
		{"debug", Options{Debug: true}, "abort node lab-node-1: " + ironicText + " (HTTP 400, PUT /v1/nodes/lab-node-1/states/provision, request-id req-0001)\nhint: " + hint},
		{"color", Options{Color: true}, "abort node lab-node-1: " + ironicText + " \x1b[2m(HTTP 400)\x1b[0m\n\x1b[36mhint:\x1b[0m " + hint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Message(err, tc.o)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if strings.Contains(got, "cloud.example.test") || strings.Contains(got, "6385") {
				t.Errorf("host leaked: %q", got)
			}
		})
	}
}

func TestMessage_NoHintForOtherStates(t *testing.T) {
	body := strings.Replace(ironicBody, "wait call-back", "active", 1)
	got := Message(apiError("PUT", "/v1/nodes/x/states/provision", 400, body), Options{})
	if strings.Contains(got, "hint") {
		t.Errorf("hint for a state the table does not cover: %q", got)
	}
}

func TestMessage_Shapes(t *testing.T) {
	retried := apiError("GET", "/v2.1/servers/srv-1", 404, `{"itemNotFound": {"message": "Instance srv-1 could not be found.", "code": 404}}`)
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"bare value", retried, "Instance srv-1 could not be found. (HTTP 404)"},
		{"pointer", &retried, "Instance srv-1 could not be found. (HTTP 404)"},
		{"text after the error", fmt.Errorf("%w (while resolving)", retried), "Instance srv-1 could not be found. (HTTP 404) (while resolving)"},
		{"after reauth", &gophercloud.ErrErrorAfterReauthentication{ErrOriginal: &retried},
			"Successfully re-authenticated, but got error executing request: Instance srv-1 could not be found. (HTTP 404)"},
		{"reauth failed", &gophercloud.ErrUnableToReauthenticate{ErrOriginal: retried, ErrReauth: errors.New("boom")},
			"Unable to re-authenticate: Instance srv-1 could not be found. (HTTP 404): boom"},
		{"empty body", apiError("HEAD", "/v2/images/x", 404, ""), "not found (HTTP 404)"},
		{"unknown body", apiError("GET", "/x", 502, "<html>Bad Gateway</html>"), "Bad Gateway (HTTP 502)"},
		{"not an API error", errors.New("open clouds.yaml: no such file"), "open clouds.yaml: no such file"},
		{"joined", errors.Join(
			fmt.Errorf("deleting a: %w", apiError("DELETE", "/v2/a", 409, `{"conflictingRequest": {"message": "a is busy", "code": 409}}`)),
			fmt.Errorf("deleting b: %w", apiError("DELETE", "/v2/b", 404, `{"itemNotFound": {"message": "b is gone", "code": 404}}`))),
			"deleting a: a is busy (HTTP 409)\ndeleting b: b is gone (HTTP 404)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Message(tc.err, Options{}); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestMessage_RealClient runs the formatter on what gophercloud itself returns.
func TestMessage_RealClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(ironicBody))
	}))
	defer srv.Close()
	pc := &gophercloud.ProviderClient{HTTPClient: *srv.Client()}
	sc := &gophercloud.ServiceClient{ProviderClient: pc, Endpoint: srv.URL + "/"}
	_, err := sc.Put(context.Background(), sc.ServiceURL("v1", "nodes", "lab-node-1", "states", "provision"), map[string]string{"target": "abort"}, nil,
		&gophercloud.RequestOpts{OkCodes: []int{202}})
	if err == nil {
		t.Fatal("no error")
	}
	got := Message(fmt.Errorf("abort node lab-node-1: %w", err), Options{Debug: true})
	want := "abort node lab-node-1: " + ironicText + " (HTTP 400, PUT /v1/nodes/lab-node-1/states/provision)"
	if !strings.HasPrefix(got, want) {
		t.Errorf("got  %q\nwant prefix %q", got, want)
	}
	if strings.Contains(got, "127.0.0.1") {
		t.Errorf("host leaked: %q", got)
	}
}

func TestUseColor(t *testing.T) {
	var sink strings.Builder
	if UseColor(&sink) {
		t.Error("a non-file writer is not a terminal")
	}
	for _, tc := range []struct {
		name, noColor, term string
		tty, want           bool
	}{
		{"terminal", "", "xterm-256color", true, true},
		{"not a terminal", "", "xterm-256color", false, false},
		{"NO_COLOR set", "1", "xterm-256color", true, false},
		{"dumb terminal", "", "dumb", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			t.Setenv("TERM", tc.term)
			if got := colorWanted(tc.tty); got != tc.want {
				t.Errorf("colorWanted(%v) = %v", tc.tty, got)
			}
		})
	}
}

func TestPrefix(t *testing.T) {
	if got := Prefix(false); got != "koc: " {
		t.Errorf("plain prefix %q", got)
	}
	if got := Prefix(true); got != "\x1b[1;31mkoc:\x1b[0m " {
		t.Errorf("colour prefix %q", got)
	}
}
