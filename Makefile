GO_BUILD_TAGS := purego

-include .env

export GOCACHE := $(CURDIR)/.cache/go-build
export GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint
CONFIG ?= config.yaml

.PHONY: ensure-cache fmt test lint build start stop

ensure-cache:
	mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"

fmt:
	gofmt -w $(shell rg --files -g '*.go')

test: ensure-cache
	go test -tags=$(GO_BUILD_TAGS) -v ./...

lint: ensure-cache
	@command -v golangci-lint >/dev/null || (echo "golangci-lint is required but not installed" && exit 1)
	golangci-lint run --build-tags=$(GO_BUILD_TAGS) ./...

build: ensure-cache
	go build -tags=$(GO_BUILD_TAGS) -o archive-wrapper ./cmd/archive-wrapper

start: build
	@test -n "$(CHAIN_HOME)" || (echo "usage: make start CHAIN_HOME=/path/to/pulsar [CONFIG=config.yaml]" && exit 1)
	./archive-wrapper start --config $(CONFIG) --home $(CHAIN_HOME)

stop:
	./archive-wrapper stop --config $(CONFIG) $(if $(SOCKET_PATH),--socket-path $(SOCKET_PATH),)
