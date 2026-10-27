// Package proxy answers HTTP CONNECT requests: it asks a policy about every
// address before it dials, then splices the two connections together.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"syscall"
	"time"
)

// A Refusal is a policy's answer for an address it will not dial.
type Refusal struct {
	Reason string
}

func (r *Refusal) Error() string { return r.Reason }

// Proxy is the http.Handler that answers CONNECT requests.
type Proxy struct {
	logf   func(format string, args ...any)
	dialer net.Dialer
}

// New returns a Proxy that dials an address only when allow returns nil for
// it. allow sees the address after DNS, the one the connection really goes to.
// logf receives one line per request and must be safe for concurrent use.
func New(allow func(netip.AddrPort) error, logf func(format string, args ...any)) *Proxy {
	return &Proxy{
		logf: logf,
		dialer: net.Dialer{
			Timeout: 10 * time.Second,
			// ControlContext runs for every address the dialer tries, after
			// DNS and before connect, so a refused address gets no packet.
			ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil {
					return &Refusal{Reason: fmt.Sprintf("cannot read the address %q", address)}
				}
				return allow(ap)
			},
		},
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if r.Method != http.MethodConnect {
		p.logf("refuse %s (only CONNECT, not %s)", target, r.Method)
		w.Header().Set("Allow", http.MethodConnect)
		http.Error(w, "egress: only CONNECT goes through", http.StatusMethodNotAllowed)
		return
	}
	if _, _, err := net.SplitHostPort(target); err != nil {
		p.logf("refuse %s (no port)", target)
		http.Error(w, "egress: CONNECT needs host:port", http.StatusBadRequest)
		return
	}

	upstream, err := p.dialer.DialContext(r.Context(), "tcp", target)
	if err != nil {
		if refusal, ok := errors.AsType[*Refusal](err); ok {
			p.logf("refuse %s (%s)", target, refusal.Reason)
			http.Error(w, "egress: refused: "+refusal.Reason, http.StatusForbidden)
			return
		}
		p.logf("fail %s (%v)", target, err)
		http.Error(w, "egress: cannot reach "+target, http.StatusBadGateway)
		return
	}

	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		upstream.Close() // the tunnel never opened, so the error says nothing more
		p.logf("fail %s (%v)", target, err)
		http.Error(w, "egress: cannot open the tunnel", http.StatusInternalServerError)
		return
	}
	p.logf("allow %s (%s)", target, upstream.RemoteAddr())
	// The client may send its first bytes before it reads this answer; they
	// wait in buffered.Reader, so the copy to upstream reads from there.
	io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n") // a failed write ends the copies below
	splice(r.Context(), client, buffered.Reader, upstream)
}

// splice copies bytes both ways until one side is done, then closes both
// connections, which ends the other copy too. It also closes both when ctx is
// done first, so a shutdown ends every tunnel.
func splice(ctx context.Context, client net.Conn, fromClient io.Reader, upstream net.Conn) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			// Either side closing ends the tunnel; a Close error changes nothing.
			client.Close()
			upstream.Close()
		})
	}
	stop := context.AfterFunc(ctx, closeBoth)
	defer stop()

	// Each copy ends when its source closes or fails. Its error only says
	// which side ended first, and the answer to either is closing both.
	var wg sync.WaitGroup
	wg.Go(func() { io.Copy(upstream, fromClient); closeBoth() })
	wg.Go(func() { io.Copy(client, upstream); closeBoth() })
	wg.Wait()
}
