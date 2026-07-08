GO_BUILD_TAGS := purego

export GOCACHE := $(CURDIR)/.cache/go-build
export GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint

.PHONY: ensure-cache fmt test lint build

ensure-cache:
	mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"

fmt:
	gofmt -w $(shell rg --files -g '*.go')

test: ensure-cache
	go test -tags=$(GO_BUILD_TAGS) ./...

lint: ensure-cache
	@command -v golangci-lint >/dev/null || (echo "golangci-lint is required but not installed" && exit 1)
	golangci-lint run --build-tags=$(GO_BUILD_TAGS) ./...

build: ensure-cache
	go build -tags=$(GO_BUILD_TAGS) -o archive-wrapper ./cmd/archive-wrapper