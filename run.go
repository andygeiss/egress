package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/andygeiss/egress/internal/proxy"
)

// errUsage means the message was already printed where the problem was found,
// so main only has to pick the exit code.
var errUsage = errors.New("usage error")

const usage = `egress — an HTTPS proxy that reaches public addresses only.

Usage:
  egress [-listen address]   answer CONNECT requests until Ctrl-C
  egress -version            print the version

A CONNECT to port 443 goes through when the address it connects to is on the
public internet. Loopback, private, link-local, carrier-NAT and the other
reserved ranges are refused, checked after DNS, so a name that points into a
private network is refused too. Every other request is refused.

Flags:
  -listen   address to listen on (env EGRESS_LISTEN, default 127.0.0.1:8888)
  -version  print the version and exit

Each request leaves one line on stderr:
  egress: allow api.anthropic.com:443 (160.79.104.10:443)
  egress: refuse 10.0.0.1:443 (10.0.0.1 is not a public address)
  egress: fail example.com:443 (dial tcp 93.184.215.14:443: i/o timeout)

Exit codes: 0 fine, 1 something failed or a signal stopped it, 2 a bad flag.
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("egress", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	listen := fs.String("listen", cmp.Or(os.Getenv("EGRESS_LISTEN"), "127.0.0.1:8888"), "address to listen on (env EGRESS_LISTEN)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h: usage already printed, exit 0
		}
		return errUsage // bad flag: message already printed by fs, exit 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "egress: unexpected argument %q — every setting is a flag\n", fs.Arg(0))
		return errUsage
	}
	if *showVersion {
		fmt.Fprintln(stdout, version())
		return nil
	}
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		fmt.Fprintf(stderr, "egress: -listen %q: want host:port, such as 127.0.0.1:8888\n", *listen)
		return errUsage
	}
	// Only a number is a port: net.Listen would look up a service name, or
	// pick any free port for an empty one, and its errors exit 1, not 2.
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		fmt.Fprintf(stderr, "egress: -listen %q: want a port from 0 to 65535\n", *listen)
		return errUsage
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return serve(ctx, ln, stderr)
}

// serve answers CONNECT requests on ln until ctx is done. A signal is the only
// way it ends, so it always returns an error: an interrupted run exits 1.
func serve(ctx context.Context, ln net.Listener, stderr io.Writer) error {
	out := &syncWriter{w: stderr}
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "egress: %s\n", fmt.Sprintf(format, args...))
	}
	srv := &http.Server{
		Handler:           proxy.New(proxy.PublicHTTPS, logf),
		ReadHeaderTimeout: 10 * time.Second,
		// Every request carries ctx, so the tunnels close when it is done:
		// Shutdown leaves connections that were taken over alone.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	logf("listening on %s", ln.Addr())
	served := make(chan error, 1)
	// Serve returns once Shutdown, below, has closed the listener.
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	// ctx is done already, so the deadline needs a context of its own.
	deadline, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := srv.Shutdown(deadline)
	<-served
	if err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	return fmt.Errorf("stopped: %w", ctx.Err())
}

// syncWriter lets concurrent tunnels share stderr without mixing their lines:
// each line is one Write, and only one Write runs at a time.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v // stamped by the toolchain: go install @version, or VCS-derived (Go 1.24+)
	}
	return "unknown" // "(devel)" means no VCS metadata — the vcs.* settings are absent too
}
