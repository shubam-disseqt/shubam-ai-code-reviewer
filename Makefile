# z-code-reviewer Makefile
# All targets run from the repo root.

BINARY      := zreview
PKG         := github.com/shubam-disseqt/z-code-reviewer
CMD_PKG     := $(PKG)/cmd/zreview
BIN_DIR     := bin
DIST_DIR    := dist

# Version metadata — overridden by CI at release time
VERSION     ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo "dev")
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
	-X $(CMD_PKG).Version=$(VERSION) \
	-X $(CMD_PKG).GitCommit=$(GIT_COMMIT) \
	-X $(CMD_PKG).BuildDate=$(BUILD_DATE)

GO_BUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build test coverage lint vet fmt check clean tidy docs vuln help

## build: build the zreview binary for the host platform into ./bin/
build: $(BIN_DIR)/$(BINARY)

$(BIN_DIR)/$(BINARY):
	@mkdir -p $(BIN_DIR)
	$(GO_BUILD) -o $(BIN_DIR)/$(BINARY) ./cmd/zreview

## test: run all unit tests
test:
	go test ./... -race -count=1

## coverage: run tests with coverage and enforce 80% floor per changed package
coverage:
	go test ./... -race -count=1 -covermode=atomic -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

## fmt: format all Go source
fmt:
	gofmt -w -s .

## vet: run go vet
vet:
	go vet ./...

## lint: fmt + vet
lint: fmt vet

## tidy: tidy up go.mod / go.sum
tidy:
	go mod tidy

## check: full CI-equivalent battery
check: tidy lint test

## docs: docs are plain HTML+CSS under docs/, embedded via //go:embed
docs:
	@echo "Docs are plain HTML+CSS under docs/ — no build step."
	@echo "Served offline by 'zreview docs' and online via GitHub Pages."

## vuln: run govulncheck against all packages (matches CI)
vuln:
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	@GOBIN=$$(go env GOBIN); \
	if [ -z "$$GOBIN" ]; then GOBIN=$$(go env GOPATH)/bin; fi; \
	PATH="$$GOBIN:$$PATH" govulncheck ./...

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html

## help: list targets
help:
	@grep -E '^##' Makefile | sed 's/^## //'
