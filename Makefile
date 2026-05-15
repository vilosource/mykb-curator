# mykb-curator — build, test, lint targets.
#
# The default target (`make`) runs the dev inner loop: lint + unit
# tests. Higher pyramid levels are gated behind explicit targets so
# they don't slow the inner loop.

.DEFAULT_GOAL := check

GO ?= go
GOLANGCI_LINT ?= golangci-lint

PKG ?= ./...

.PHONY: help check build test test-unit test-integration test-contract test-scenario test-all lint fmt vet tidy clean

help:
	@echo "Targets:"
	@echo "  check             — fmt + vet + lint + unit tests (default)"
	@echo "  build             — build mykb-curator binary into ./bin/"
	@echo "  test              — alias for test-unit"
	@echo "  test-unit         — run pyramid level 1 (Go unit tests)"
	@echo "  test-integration  — add pyramid level 2 (//go:build integration)"
	@echo "  test-contract     — add pyramid level 3 (//go:build contract)"
	@echo "  test-scenario     — add pyramid level 4 (//go:build scenario)"
	@echo "  test-all          — all pyramid levels"
	@echo "  lint              — golangci-lint run"
	@echo "  fmt               — gofmt -w ."
	@echo "  vet               — go vet ./..."
	@echo "  tidy              — go mod tidy"
	@echo "  clean             — rm -rf ./bin"

check: fmt vet lint test-unit

build:
	mkdir -p bin
	$(GO) build -o bin/mykb-curator ./cmd/mykb-curator
	$(GO) build -o bin/pi-wrapper   ./cmd/pi-wrapper

test: test-unit

test-unit:
	$(GO) test -race -count=1 $(PKG)

test-integration:
	$(GO) test -race -count=1 -tags=integration $(PKG) ./test/integration/...

test-contract:
	$(GO) test -race -count=1 -tags=contract $(PKG) ./test/contract/...

test-scenario:
	$(GO) test -race -count=1 -tags=scenario -timeout=30m ./test/scenario/...

test-all: test-unit test-integration test-contract test-scenario

lint:
	$(GOLANGCI_LINT) run

fmt:
	$(GO) fmt $(PKG)

vet:
	$(GO) vet $(PKG)

tidy:
	$(GO) mod tidy

clean:
	rm -rf ./bin
