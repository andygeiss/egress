package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A test never sends a request off this machine (the baseline's
// patterns/go-testing.md). So every address below is an IP literal or
// localhost, every refused address is refused before a packet leaves, and the
// servers listen on loopback. They are real sockets, not httptest's in-memory
// server: the proxy takes over its connections and dials out of its own.

func TestServeHTTP_refuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		target string
		status int
		line   string
	}{
		{"only CONNECT goes through", http.MethodGet, "http://example.com/", http.StatusMethodNotAllowed, "refuse example.com (only CONNECT, not GET)"},
		{"CONNECT needs a port", http.MethodConnect, "example.com", http.StatusBadRequest, "refuse example.com (no port)"},
		{"a private address", http.MethodConnect, "10.0.0.1:443", http.StatusForbidden, "refuse 10.0.0.1:443 (10.0.0.1 is not a public address)"},
		{"the machine itself", http.MethodConnect, "127.0.0.1:443", http.StatusForbidden, "refuse 127.0.0.1:443 (127.0.0.1 is not a public address)"},
		{"a name that resolves to the machine itself", http.MethodConnect, "localhost:443", http.StatusForbidden, "refuse localhost:443 ("},
		{"a private address inside an IPv6 one", http.MethodConnect, "[::ffff:10.0.0.1]:443", http.StatusForbidden, "refuse [::ffff:10.0.0.1]:443 (10.0.0.1 is not a public address)"},
		{"an address with a zone", http.MethodConnect, "[::%25en0]:443", http.StatusForbidden, "refuse [::%en0]:443 (::%en0 is not a public address)"},
		{"a port other than 443", http.MethodConnect, "1.1.1.1:80", http.StatusForbidden, "refuse 1.1.1.1:80 (port 80, and only 443 goes through)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var lines lines
			rec := httptest.NewRecorder()
			New(PublicHTTPS, lines.printf).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.status, rec.Body)
			}
			if got := lines.String(); !strings.HasPrefix(got, tt.line) {
				t.Errorf("line = %q, want it to start with %q", got, tt.line)
			}
		})
	}
}

func TestServeHTTP_tunnels(t *testing.T) {
	t.Parallel()
	upstream := echoServer(t)
	var lines lines
	srv := httptest.NewServer(New(allowAll, lines.printf))
	t.Cleanup(srv.Close)

	conn, r := connect(t, srv.Listener.Addr().String(), upstream)
	fmt.Fprint(conn, "ping\n")
	got, err := r.ReadString('\n')
	if err != nil || got != "ping\n" {
		t.Fatalf("through the tunnel: got %q, %v; want %q", got, err, "ping\n")
	}
	if want := fmt.Sprintf("allow %s (%s)", upstream, upstream); lines.String() != want {
		t.Errorf("line = %q, want %q", lines.String(), want)
	}
}

func TestServeHTTP_failsWhenNothingListens(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()

	var lines lines
	rec := httptest.NewRecorder()
	New(allowAll, lines.printf).ServeHTTP(rec, httptest.NewRequest(http.MethodConnect, closed, nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if want := "fail " + closed + " ("; !strings.HasPrefix(lines.String(), want) {
		t.Errorf("line = %q, want it to start with %q", lines.String(), want)
	}
}

func TestServeHTTP_shutdownClosesTunnels(t *testing.T) {
	t.Parallel()
	upstream := echoServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	var lines lines
	srv := httptest.NewUnstartedServer(New(allowAll, lines.printf))
	srv.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	srv.Start()
	t.Cleanup(srv.Close)

	conn, r := connect(t, srv.Listener.Addr().String(), upstream)
	cancel()
	// The deadline only turns a tunnel that stays open into a failure instead
	// of a hang; the read returns as soon as the proxy closes the tunnel.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := r.ReadByte(); errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("the tunnel is still open after the context ended")
	}
}

// allowAll lets the tests dial their own loopback servers.
func allowAll(netip.AddrPort) error { return nil }

// echoServer listens on loopback and sends back every byte it gets. Each
// goroutine ends when the listener or its connection closes, at cleanup.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // the listener closed
			}
			wg.Go(func() {
				defer conn.Close()
				io.Copy(conn, conn) // ends when the other side closes
			})
		}
	})
	t.Cleanup(func() { ln.Close(); wg.Wait() })
	return ln.Addr().String()
}

// connect opens a tunnel to target through the proxy at proxyAddr and fails
// the test unless the proxy answers 200. Reads go through the reader it
// returns, which may already hold the first bytes from target.
func connect(t *testing.T, proxyAddr, target string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT %s: got %s, want 200", target, resp.Status)
	}
	return conn, r
}

// lines collects what the proxy reports, safely across its goroutines.
type lines struct {
	mu  sync.Mutex
	all []string
}

func (l *lines) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, fmt.Sprintf(format, args...))
}

func (l *lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.all, "\n")
}
