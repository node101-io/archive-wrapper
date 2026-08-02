GO_BUILD_TAGS := purego

-include .env

export GOCACHE := $(CURDIR)/.cache/go-build
export GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint
CONFIG ?= config.yaml

.PHONY: ensure-cache proto fmt test lint build run stop

ensure-cache:
	mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"

proto:
	cd proto && buf generate --template buf.gen.gogo.yaml

fmt:
	gofmt -w $(shell rg --files -g '*.go')

test: ensure-cache
	go test -tags=$(GO_BUILD_TAGS) -v ./...

lint: ensure-cache
	@command -v golangci-lint >/dev/null || (echo "golangci-lint is required but not installed" && exit 1)
	golangci-lint run --build-tags=$(GO_BUILD_TAGS) ./...

build: ensure-cache
	go build -tags=$(GO_BUILD_TAGS) -o archive-wrapper ./cmd/archive-wrapper

run: build
	@test -n "$(CONFIG)" || (echo "CONFIG is required" && exit 1)
	./archive-wrapper run --config "$(CONFIG)" $(if $(CHAIN_HOME),--home "$(CHAIN_HOME)",)

stop:
	./archive-wrapper stop --config "$(CONFIG)" $(if $(CONTROL_SOCKET_PATH),--control-socket-path "$(CONTROL_SOCKET_PATH)",)
