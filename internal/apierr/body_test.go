package apierr

import (
	"strings"
	"testing"
)

// The bodies are recorded shapes with invented names and ids.
func TestBodyMessage(t *testing.T) {
	const ironicInner = `{\"faultcode\": \"Client\", \"faultstring\": \"The requested action \\\"abort\\\" can not be performed on node \\\"11111111-2222-3333-4444-555555555555\\\" while it is in state \\\"wait call-back\\\".\", \"debuginfo\": null}`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"ironic nested error_message", `{"error_message": "` + ironicInner + `"}`,
			`The requested action "abort" can not be performed on node "11111111-2222-3333-4444-555555555555" while it is in state "wait call-back".`},
		{"ironic plain error_message", `{"error_message": "Node lab-node-1 could not be found."}`,
			`Node lab-node-1 could not be found.`},
		{"octavia faultstring", `{"faultcode": "Client", "faultstring": "Load Balancer lb-1 not found", "debuginfo": null}`,
			`Load Balancer lb-1 not found`},
		{"nova itemNotFound", `{"itemNotFound": {"message": "Instance srv-1 could not be found.", "code": 404}}`,
			`Instance srv-1 could not be found.`},
		{"nova badRequest", `{"badRequest": {"message": "Invalid input for field/attribute name.", "code": 400}}`,
			`Invalid input for field/attribute name.`},
		{"nova conflictingRequest", `{"conflictingRequest": {"message": "Cannot 'stop' instance srv-1 while it is in vm_state stopped", "code": 409}}`,
			`Cannot 'stop' instance srv-1 while it is in vm_state stopped`},
		{"cinder volume", `{"badRequest": {"code": 400, "message": "Invalid volume: Volume status must be available"}}`,
			`Invalid volume: Volume status must be available`},
		{"neutron", `{"NeutronError": {"type": "NetworkNotFound", "message": "Network net-1 could not be found.", "detail": ""}}`,
			`Network net-1 could not be found.`},
		{"keystone", `{"error": {"code": 401, "message": "The request you have made requires authentication.", "title": "Unauthorized"}}`,
			`The request you have made requires authentication.`},
		{"oauth style", `{"error": "invalid_request", "error_description": "Missing required parameter."}`,
			`Missing required parameter.`},
		{"error string only", `{"error": "something broke"}`, `something broke`},
		{"glance v2 errors list", `{"errors": [{"status": 404, "title": "Not Found", "detail": "Image img-1 not found\n\n  ", "request": "/v2/images/img-1"}]}`,
			`Image img-1 not found`},
		{"placement errors list", `{"errors": [{"status": 409, "title": "Conflict", "detail": "update conflict: resource provider has allocations.", "request_id": "req-x"}]}`,
			`update conflict: resource provider has allocations.`},
		{"two errors", `{"errors": [{"detail": "first"}, {"detail": "second"}]}`, `first; second`},
		{"designate", `{"code": 404, "type": "zone_not_found", "message": "Could not find Zone", "request_id": "req-x"}`,
			`Could not find Zone`},
		{"barbican", `{"title": "Not Found", "description": "Secret not found."}`, `Not Found: Secret not found.`},
		{"plain text", "  Bad Gateway\n", `Bad Gateway`},
		{"plain multi-line", "400 Bad Request\n\nThe server could not comply with the request.\n", `400 Bad Request The server could not comply with the request.`},
		{"html page", "<html><head><title>502</title></head><body><h1>Bad Gateway</h1>\n<p>try later</p></body></html>", `502 Bad Gateway try later`},
		{"unknown json", `{"a": 1, "b": [2]}`, `{"a": 1, "b": [2]}`},
		{"json string", `"just a string"`, `just a string`},
		{"truncated json", `{"error": `, `{"error":`},
		{"empty", ``, ``},
		{"blank", " \n ", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyMessage([]byte(tc.body), false); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestBodyMessage_LongUnknownTextIsCutUnlessFull(t *testing.T) {
	body := []byte("gateway said " + strings.Repeat("x", 2*maxBody))
	short := bodyMessage(body, false)
	if !strings.HasSuffix(short, "...") || len([]rune(short)) != maxBody+3 {
		t.Errorf("not cut to %d runes: %d", maxBody, len([]rune(short)))
	}
	if full := bodyMessage(body, true); full != string(body) {
		t.Errorf("full body changed")
	}
}
