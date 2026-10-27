// Command egress is an HTTPS proxy that reaches public addresses only.
//
// It answers CONNECT requests for port 443 and refuses every address outside
// the public internet: the machine itself, private networks, link-local,
// carrier NAT, and the other ranges IANA reserves. It checks the address it
// connects to, after DNS, so a name that resolves into a private network is
// refused too. claude-sandbox in baseline-ops runs it in front of an agent
// that may read the open web but must not reach the Mac it runs on.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Shutting down can take seconds, so a second Ctrl-C kills at once. This
	// goroutine ends when ctx is done, or with the process.
	go func() { <-ctx.Done(); stop() }()

	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(2) // the message was already printed where the problem was found
	default:
		fmt.Fprintf(os.Stderr, "egress: %v\n", err)
		os.Exit(1)
	}
}
