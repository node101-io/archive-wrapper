GO_BUILD_TAGS := purego
VERSION ?= dev
COMMIT_SHA ?= $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
SOURCE_DATE_EPOCH ?= $(shell git show -s --format=%ct HEAD 2>/dev/null || printf 0)
BUILD_DATE ?= $(shell date -u -d "@$(SOURCE_DATE_EPOCH)" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || printf unknown)
BUILD_LDFLAGS := -s -w -X 'main.version=$(VERSION)' -X 'main.commitSHA=$(COMMIT_SHA)' -X 'main.buildDate=$(BUILD_DATE)'

IMAGE ?= archive-wrapper:$(COMMIT_SHA)
DOCKER_PLATFORM ?= linux/amd64
DOCKER_PLATFORMS ?= linux/amd64,linux/arm64
OCI_OUTPUT ?= $(CURDIR)/.cache/archive-wrapper.oci.tar
DOCKER_BUILDER ?= archive-wrapper-builder
DOCKER_BUILD_ARGS := \
	--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT_SHA=$(COMMIT_SHA) \
	--build-arg BUILD_DATE=$(BUILD_DATE) \
	--build-arg SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH)

-include .env

export GOCACHE := $(CURDIR)/.cache/go-build
export GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint
export POSTGRES_URI
export ARCHIVE_WRAPPER_CONFIG
export ARCHIVE_WRAPPER_CHAIN_HOME
export ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS
export ARCHIVE_WRAPPER_GRPC_TRANSPORT_MODE
export ARCHIVE_WRAPPER_DB_PATH
export ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH
export ARCHIVE_WRAPPER_LOG_PATH
export ARCHIVE_WRAPPER_HEALTHCHECK_ADDRESS
CONFIG ?= config.yaml

.PHONY: ensure-cache ensure-docker-builder proto fmt test lint build run stop docker-build docker-build-multiarch docker-inspect docker-test reproducible-build

ensure-cache:
	mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"

ensure-docker-builder:
	@docker buildx inspect $(DOCKER_BUILDER) >/dev/null 2>&1 || \
		docker buildx create --name $(DOCKER_BUILDER) --driver docker-container
	@docker buildx inspect --bootstrap $(DOCKER_BUILDER) >/dev/null

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
	go build -tags=$(GO_BUILD_TAGS) -trimpath -buildvcs=false \
		-ldflags="$(BUILD_LDFLAGS)" -o archive-wrapper ./cmd/archive-wrapper

docker-build: ensure-cache
	docker buildx build --load --platform=$(DOCKER_PLATFORM) \
		$(DOCKER_BUILD_ARGS) -t $(IMAGE) .

docker-build-multiarch: ensure-cache ensure-docker-builder
	docker buildx build --builder=$(DOCKER_BUILDER) --platform=$(DOCKER_PLATFORMS) \
		$(DOCKER_BUILD_ARGS) \
		--output type=oci,dest=$(OCI_OUTPUT),rewrite-timestamp=true .

docker-inspect: docker-build
	docker image inspect $(IMAGE) --format '{{json .Config}}'
	@docker run --rm --entrypoint /usr/local/bin/archive-wrapper $(IMAGE) version

docker-test:
	ARCHIVE_WRAPPER_IMAGE=$(IMAGE) ./scripts/test-container.sh

reproducible-build:
	VERSION=$(VERSION) COMMIT_SHA=$(COMMIT_SHA) SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) \
		BUILD_DATE=$(BUILD_DATE) ./scripts/check-reproducible-build.sh

run: build
	@test -n "$(CONFIG)" || (echo "CONFIG is required" && exit 1)
	./archive-wrapper run --config "$(CONFIG)" $(if $(CHAIN_HOME),--home "$(CHAIN_HOME)",)

stop:
	./archive-wrapper stop --config "$(CONFIG)" $(if $(CONTROL_SOCKET_PATH),--control-socket-path "$(CONTROL_SOCKET_PATH)",)
