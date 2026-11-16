package auth

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"regexp"
)

// RequestIDHeader is the header OpenStack services read as the caller's global
// request id: nova, neutron and the rest log it as global_request_id next to
// their own per-call req-id.
const RequestIDHeader = "X-OpenStack-Request-ID"

// requestIDRe is the one shape OpenStack's oslo.middleware accepts for a
// caller-supplied id (anything else is ignored by the server); it is the same
// check the lcm-core gateway applies.
var requestIDRe = regexp.MustCompile(`^req-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidRequestID reports whether s is `req-<uuid>` in lowercase hex.
func ValidRequestID(s string) bool { return requestIDRe.MatchString(s) }

// NewRequestID mints `req-<uuid4>`.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("koc: no randomness for a request id: %v", err))
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("req-%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ResolveRequestID fixes the invocation's one global request id: the value of
// --request-id / OS_REQUEST_ID when given (an invalid one is an error), else a
// freshly minted req-<uuid4>. It is idempotent: the first call decides.
func (o *Options) ResolveRequestID() (string, error) {
	if o.RequestID == "" {
		o.RequestID = NewRequestID()
		return o.RequestID, nil
	}
	if !ValidRequestID(o.RequestID) {
		return "", fmt.Errorf("invalid request id %q (--request-id / OS_REQUEST_ID): want req-<uuid>, lowercase, e.g. %s",
			o.RequestID, NewRequestID())
	}
	return o.RequestID, nil
}

// requestIDTransport stamps every request with the global request id, unless the
// request already carries one. It sits outermost, above --debug, so the dump
// shows the header that goes out; retries and re-authentication reuse the same
// client and so the same id.
type requestIDTransport struct {
	rt http.RoundTripper
	id string
}

func (t *requestIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(RequestIDHeader) != "" {
		return t.rt.RoundTrip(req)
	}
	// A RoundTripper must not modify the request it is given.
	req = req.Clone(req.Context())
	req.Header.Set(RequestIDHeader, t.id)
	return t.rt.RoundTrip(req)
}

// withRequestID wraps rt so every call carries id.
func withRequestID(rt http.RoundTripper, id string) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &requestIDTransport{rt: rt, id: id}
}
