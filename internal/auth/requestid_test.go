package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
)

func TestNewRequestID_Shape(t *testing.T) {
	re := regexp.MustCompile(`^req-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for range 50 {
		id := NewRequestID()
		if !re.MatchString(id) || !ValidRequestID(id) {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate %q", id)
		}
		seen[id] = true
	}
}

func TestValidRequestID(t *testing.T) {
	for id, want := range map[string]bool{
		"req-11111111-2222-4333-8444-555555555555": true,
		"":                                     false,
		"11111111-2222-4333-8444-555555555555": false,
		"req-11111111-2222-4333-8444-55555555555":    false,
		"req-11111111-2222-4333-8444-5555555555555":  false,
		"REQ-11111111-2222-4333-8444-555555555555":   false,
		"req-1111111G-2222-4333-8444-555555555555":   false,
		"req-11111111-2222-4333-8444-555555555555\n": false,
		"x req-11111111-2222-4333-8444-555555555555": false,
	} {
		if got := ValidRequestID(id); got != want {
			t.Errorf("ValidRequestID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestResolveRequestID(t *testing.T) {
	o := &Options{}
	a, err := o.ResolveRequestID()
	if err != nil || !ValidRequestID(a) {
		t.Fatalf("mint: %q, %v", a, err)
	}
	if b, _ := o.ResolveRequestID(); b != a {
		t.Errorf("not idempotent: %q then %q", a, b)
	}

	const want = "req-11111111-2222-4333-8444-555555555555"
	o = &Options{RequestID: want}
	if got, err := o.ResolveRequestID(); err != nil || got != want {
		t.Errorf("override: %q, %v", got, err)
	}

	o = &Options{RequestID: "nope"}
	if _, err := o.ResolveRequestID(); err == nil {
		t.Error("invalid id accepted")
	}
	if _, err := o.Authenticate(context.Background()); err == nil {
		t.Error("Authenticate accepted an invalid request id")
	}
}

// The header is on every call through a koc HTTP client: plain calls, a redirect
// hop, and a call that already carries its own id keeps it.
func TestHTTPClient_StampsEveryCall(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get(RequestIDHeader))
		mu.Unlock()
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/landed", http.StatusFound)
		}
	}))
	defer srv.Close()

	const want = "req-11111111-2222-4333-8444-555555555555"
	o := &Options{RequestID: want}
	hc := o.httpClient(nil)
	for _, path := range []string{"/a", "/b", "/hop"} {
		resp, err := doGet(t, &hc, srv.URL+path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	mu.Lock()
	got := append([]string(nil), seen...)
	seen = nil
	mu.Unlock()
	if len(got) != 4 { // a, b, hop, landed
		t.Fatalf("server saw %d requests: %v", len(got), got)
	}
	for i, id := range got {
		if id != want {
			t.Errorf("request %d carried %q, want %q", i, id, want)
		}
	}

	// A request's own id is left alone, and the caller's request is not mutated.
	const own = "req-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/own", nil)
	req.Header.Set(RequestIDHeader, own)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	plain, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/plain", nil)
	resp, err = hc.Do(plain)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if plain.Header.Get(RequestIDHeader) != "" {
		t.Error("the transport mutated the caller's request")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != own || seen[1] != want {
		t.Errorf("own/plain ids: %v", seen)
	}
}

// Keystone-less auth (none, admin_token, http_basic) builds its own client; it
// is stamped as well.
func TestKeystoneless_StampsEveryCall(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(RequestIDHeader)
	}))
	defer srv.Close()

	const want = "req-11111111-2222-4333-8444-555555555555"
	o := &Options{AuthType: "none", Endpoint: srv.URL, RequestID: want}
	c, err := o.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc := &gophercloud.ServiceClient{ProviderClient: c.Provider, Endpoint: srv.URL + "/"}
	// The reply is irrelevant, the header is not.
	if resp, err := sc.Get(context.Background(), srv.URL+"/x", nil, nil); err == nil {
		_ = resp.Body.Close()
	}
	if got != want {
		t.Errorf("keystoneless call carried %q, want %q", got, want)
	}
}
