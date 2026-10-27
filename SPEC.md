# Specification: egress

`egress` is an HTTPS proxy that reaches public addresses only. It exists for
claude-sandbox in [baseline-ops](https://github.com/andygeiss/baseline-ops): an
agent that reads the open web runs behind it.

## The brief

The baseline's `README.md` *The task brief* asks every project to keep these four fields
at its root, job and why one line each; each bullet names its long form.

- **Job** — a program behind `egress` reaches any public HTTPS site, and nothing on
  the machine it runs on or in the networks that machine sits in.
- **Why** — a page can steer an agent to a LAN address or to the Mac's own services,
  and a proxy that filters names lets both through once its allowlist is off (the
  README's *What it refuses*).
- **Guardrails** — the README's *Decisions* and *Baseline deviations*.
- **Done means** — `make ci` is green and the baseline's `checklists/cli-tool.md`
  walks clean.
