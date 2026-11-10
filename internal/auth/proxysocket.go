package auth

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// EnvProxySocket names a unix socket of an HTTP forward proxy. When it is set,
// every OpenStack-facing request leaves koc as a CONNECT through that socket and
// nothing else: HTTPS_PROXY and NO_PROXY are ignored and no direct dial is made.
// A sandbox with no network uses it to reach only the endpoints its host allows.
const EnvProxySocket = "KOC_PROXY_SOCKET"

// proxyHost names the proxy for net/http. It is never resolved: the dial hook
// below sends the connection to the socket. The scheme is "http" because the
// CONNECT goes over the local socket in clear; the tunnelled request stays TLS.
const proxyHost = "koc-proxy-socket.invalid"

// dialTimeout bounds one connect to the proxy socket.
const dialTimeout = 10 * time.Second

// applyProxySocket points tr at the unix socket in KOC_PROXY_SOCKET, if set.
// Plain http:// targets go out as absolute-URI requests, which a CONNECT-only
// proxy refuses; the region endpoints are https.
func applyProxySocket(tr *http.Transport) {
	sock := os.Getenv(EnvProxySocket)
	if sock == "" {
		return
	}
	tr.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: proxyHost})
	dialer := &net.Dialer{Timeout: dialTimeout}
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", sock)
	}
}
