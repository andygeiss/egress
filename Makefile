# Copied from the baseline (stack/makefile.md). Adjust per its rule 5; record
# any other deviation in the README.

# The main package: ./cmd/server for a web application, . for a single-binary
# CLI.
MAIN = .

# Targets are alphabetical, so the default is named rather than first.
.DEFAULT_GOAL = check
.PHONY: build check ci clean fmt run test

# Release-shaped local binary in bin/ (go build creates the directory).
build:
	CGO_ENABLED=0 go build -trimpath -o bin/ $(MAIN)

# Default. Every gate, in this order (operations/ci.md), against the working
# tree. Run before every commit.
check:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go fix -diff ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go mod tidy -diff
	go test -race -shuffle=on ./...
	CGO_ENABLED=0 go build -trimpath ./...

# The same gates against the commit: a file never added, or a .env, cannot
# make it green. Run before every push. go version runs first, inside the
# copy, so the run records which toolchain ran. The archive goes through a
# file so git's exit status stops the run; one shell line so the trap cleans
# up however check ends.
ci:
	t=$$(mktemp); d=$$(mktemp -d); trap 'rm -rf "$$t" "$$d"' EXIT; git archive -o "$$t" HEAD && tar -xf "$$t" -C "$$d" && go -C "$$d" version && $(MAKE) -C "$$d" check

clean:
	rm -rf bin/ egress

# goimports first: go fix type-checks, so a missing import would stop the
# recipe before goimports could add it. go fix applies each fixer once and one
# rewrite exposes the next, so it repeats until its own diff is empty — bounded,
# so a fixer that cannot converge fails the recipe instead of hanging it.
# goimports again last: the import go fix adds to a single-import file is a
# second declaration, which gofmt keeps and only goimports merges.
fmt:
	go run golang.org/x/tools/cmd/goimports@latest -w .
	n=3; until go fix -diff ./... > /dev/null 2>&1; do [ $$n -gt 0 ] || exit 1; go fix ./... || exit 1; n=$$((n - 1)); done
	go run golang.org/x/tools/cmd/goimports@latest -w .

# Loads .env when it is there, so a local start is one command. Only run:
# check and test MUST NOT depend on a developer's machine (rule 6). One shell
# line, because each recipe line gets its own shell.
run:
	set -a; if [ -f .env ]; then . ./.env; fi; set +a; go run $(MAIN)

# The inner loop.
test:
	go test -race -shuffle=on ./...
