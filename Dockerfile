# syntax=docker/dockerfile:1.10

ARG SOURCE_DATE_EPOCH=0

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags=purego -trimpath -buildvcs=false \
    -ldflags="-s -w -X 'main.version=$VERSION' -X 'main.commitSHA=$COMMIT_SHA' -X 'main.buildDate=$BUILD_DATE'" \
    -o /out/archive-wrapper ./cmd/archive-wrapper && \
    mkdir -p /out/rootfs/var/lib/archive-wrapper/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="archive-wrapper" \
      org.opencontainers.image.description="Mina archive sidecar for Pulsar validators" \
      org.opencontainers.image.source="https://github.com/node101-io/archive-wrapper" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$COMMIT_SHA \
      org.opencontainers.image.created=$BUILD_DATE

ENV ARCHIVE_WRAPPER_CONFIG=/etc/archive-wrapper/config.yaml \
    ARCHIVE_WRAPPER_CHAIN_HOME=/var/lib/pulsar

COPY --from=builder --chown=65532:65532 /out/rootfs/var/lib/archive-wrapper /var/lib/archive-wrapper
COPY --from=builder --chown=65532:65532 /out/archive-wrapper /usr/local/bin/archive-wrapper

USER 65532:65532
EXPOSE 9095
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=6 \
    CMD ["/usr/local/bin/archive-wrapper", "healthcheck", "--timeout", "3s"]
ENTRYPOINT ["/usr/local/bin/archive-wrapper"]
CMD ["run"]
