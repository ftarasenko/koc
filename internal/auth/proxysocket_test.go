package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// connectProxy listens on a unix socket, records each request line and either
// tunnels to upstream (allow) or answers 403.
type connectProxy struct {
	ln       net.Listener
	upstream string
	allow    bool
	mu       sync.Mutex
	lines    []string
}

func startConnectProxy(t *testing.T, upstream string, allow bool) (*connectProxy, string) {
	t.Helper()
	// A short path: unix socket names are limited to ~100 bytes.
	dir, err := os.MkdirTemp("", "kocps")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "p.sock")
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	p := &connectProxy{ln: ln, upstream: upstream, allow: allow}
	t.Cleanup(func() { _ = ln.Close() })
	go p.serve()
	return p, sock
}

func (p *connectProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *connectProxy) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.lines = append(p.lines, req.Method+" "+req.Host)
	p.mu.Unlock()
	if req.Method != http.MethodConnect || !p.allow {
		_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	up, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", p.upstream)
	if err != nil {
		_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	defer func() { _ = up.Close() }()
	_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	done := make(chan struct{}, 2)
	go func() { _, _ = copyBuf(up, br); done <- struct{}{} }()
	go func() { _, _ = copyBuf(c, up); done <- struct{}{} }()
	<-done
}

func copyBuf(dst net.Conn, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var n int64
	for {
		r, err := src.Read(buf)
		if r > 0 {
			w, werr := dst.Write(buf[:r])
			n += int64(w)
			if werr != nil {
				return n, werr
			}
		}
		if err != nil {
			return n, err
		}
	}
}

func (p *connectProxy) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

func TestApplyProxySocket_UnsetKeepsEnvProxy(t *testing.T) {
	t.Setenv(EnvProxySocket, "")
	tr := newHTTPTransport(nil)
	if tr.Proxy == nil {
		t.Fatal("Proxy is nil: HTTPS_PROXY would be ignored")
	}
}

func TestApplyProxySocket_PlaceholderParses(t *testing.T) {
	t.Setenv(EnvProxySocket, "/nonexistent.sock")
	tr := newHTTPTransport(nil)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/", http.NoBody)
	u, err := tr.Proxy(req)
	if err != nil || u == nil || u.Host != strings.TrimPrefix(proxyPlaceholder, "http://") {
		t.Fatalf("Proxy = %v, %v", u, err)
	}
}

func TestApplyProxySocket_TunnelsHTTPSThroughSocket(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	proxy, sock := startConnectProxy(t, srv.Listener.Addr().String(), true)
	t.Setenv(EnvProxySocket, sock)
	// An env proxy must not win over the socket.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	tr := newHTTPTransport(&tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12})
	defer tr.CloseIdleConnections()
	resp, err := get(tr, srv.URL)
	if err != nil {
		t.Fatalf("GET through the socket: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got := proxy.seen()
	if len(got) != 1 || got[0] != "CONNECT "+srv.Listener.Addr().String() {
		t.Fatalf("proxy saw %v, want one CONNECT to the server", got)
	}
}

func TestApplyProxySocket_DeniedConnectIsAnError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached the server although the proxy denied it")
	}))
	defer srv.Close()
	_, sock := startConnectProxy(t, srv.Listener.Addr().String(), false)
	t.Setenv(EnvProxySocket, sock)

	tr := newHTTPTransport(nil)
	defer tr.CloseIdleConnections()
	resp, err := get(tr, srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected an error for a denied CONNECT")
	}
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("error %q does not carry the proxy status", err)
	}
}

func TestApplyProxySocket_NoDirectDialWhenSocketMissing(t *testing.T) {
	var hit bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	t.Setenv(EnvProxySocket, filepath.Join(t.TempDir(), "gone.sock"))

	tr := newHTTPTransport(nil)
	defer tr.CloseIdleConnections()
	resp, err := get(tr, srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a dial error")
	}
	if hit {
		t.Fatal("the request went out directly")
	}
}

func get(tr http.RoundTripper, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	return (&http.Client{Transport: tr}).Do(req)
}
