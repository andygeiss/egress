# egress

An HTTPS proxy that reaches public addresses only. Put it in front of a program
that may read the open web, but must not reach the machine it runs on or the
networks that machine sits in.

```sh
go install github.com/andygeiss/egress@latest
egress &                                              # listens on 127.0.0.1:8888
curl -x http://127.0.0.1:8888 https://example.com/    # goes through
curl -x http://127.0.0.1:8888 https://192.168.1.1/    # refused: CONNECT tunnel failed, response 403
```

It is built to the [engineering baseline](https://github.com/andygeiss/baseline)
as a CLI tool. [SPEC.md](SPEC.md) says what it is for: claude-sandbox in
[baseline-ops](https://github.com/andygeiss/baseline-ops) runs it in front of an
agent that reads the open web.

## What goes through

A `CONNECT` to port 443, when the address egress connects to is on the public
internet. That is how HTTPS travels through a proxy. Everything else is refused:
plain HTTP, other methods, and other ports.

## What it refuses

egress checks the address it connects to, after DNS, not the name it was given.
A name that resolves into a private network is refused like the address itself,
and before a single packet goes there.

- The machine itself: loopback, `0.0.0.0` and `::`.
- The networks it sits in: `10/8`, `172.16/12`, `192.168/16` and `fc00::/7`.
- Link-local and multicast, among them `169.254.169.254`, the cloud metadata address.
- Carrier NAT and Tailscale: `100.64/10`.
- Every other range in IANA's special-purpose registries: documentation,
  benchmarking, reserved.
- An IPv6 address with an IPv4 address inside — mapped, NAT64, 6to4, Teredo —
  because the address inside can be a private one.
- An IPv6 address with a zone, such as `::%en0`. A public address never has
  one, and `::` with a zone still reaches the machine itself.

## Flags

```text
-listen   address to listen on (env EGRESS_LISTEN, default 127.0.0.1:8888)
-version  print the version and exit
```

## What it prints

Nothing on stdout. Each request leaves one line on stderr:

```text
egress: allow api.anthropic.com:443 (160.79.104.10:443)
egress: refuse 10.0.0.1:443 (10.0.0.1 is not a public address)
egress: fail example.com:443 (dial tcp 93.184.215.14:443: i/o timeout)
```

Exit codes: 0 fine, 1 something failed or a signal stopped it, 2 a bad flag.

## Decisions

- **What counts as public is code, not configuration.** No flag lets a private
  range through. Changing the list is a release.
- **The check runs where the dialer connects** (`net.Dialer.ControlContext`), once
  for every address it tries. A name cannot pass the check with one address and
  then connect to another.
- **HTTPS only.** A plain-HTTP proxy has to read and rewrite requests. A `CONNECT`
  tunnel only copies bytes, so egress never sees what goes through it.
- **A signal always ends in exit 1.** It is the only way egress stops, and the
  baseline's checklist asks an interrupted run to exit non-zero. Ctrl-C closes
  every open tunnel first.
- **The proxy tests use real loopback sockets,** not httptest's in-memory server:
  the proxy takes over its connections and dials out of its own. Every address
  in them is an IP literal or `localhost`, so no test leaves the machine.

## Baseline deviations

None.
