# Archive Wrapper Container Contract

## Artifact

The `Dockerfile` produces a pure-Go archive-wrapper binary in a pinned Go
builder and copies it into a pinned distroless `static-debian12:nonroot`
runtime. The runtime image has no shell or package manager and runs as
UID/GID `65532:65532`.

Supported image platforms are:

```text
linux/amd64
linux/arm64
```

Build a native image or a multi-platform OCI archive with:

```sh
make docker-build IMAGE=archive-wrapper:local
make docker-build-multiarch OCI_OUTPUT=/tmp/archive-wrapper.oci.tar
```

`VERSION`, `COMMIT_SHA`, `SOURCE_DATE_EPOCH`, and `BUILD_DATE` may be supplied
as Make variables. Defaults use the checked-out commit and its timestamp;
wall-clock build time is not embedded. OCI timestamps are rewritten to
`SOURCE_DATE_EPOCH`, and provenance attestations are disabled for byte-stable
local OCI exports.

Verify binary and OCI reproducibility with:

```sh
make reproducible-build
make reproducible-image
```

The pinned builder, runtime, and PostgreSQL test-fixture digests must be updated
through a reviewed dependency bump. Re-run both platform builds, artifact
reproducibility checks, and `make docker-test` after any digest change.

## Vulnerability scanning

Container CI scans the actual builder stage and final runtime image separately.
Fixable `HIGH` and `CRITICAL` operating-system package vulnerabilities fail the
build for either image. Findings without an available upstream patch do not
block changes that cannot yet consume a fix.

Go dependency findings are analyzed from source with `govulncheck` inside the
pinned builder environment. The scan targets only the shipped
`./cmd/archive-wrapper` executable, uses the production `purego` build tag, and
stores reachable findings as a report-only SARIF artifact. Findings remain
visible without blocking pull requests; tool installation, configuration, or
analysis failures still fail CI. Import-only and module-only informational
entries are excluded from the artifact; reachable findings are never
suppressed automatically.

## Runtime mounts

The image contains no wrapper config, Pulsar genesis, PostgreSQL URI, `.env`
file, LevelDB state, or log file. A deployment must provide:

| Container path | Access | Purpose |
| --- | --- | --- |
| `/etc/archive-wrapper/config.yaml` | read-only | Wrapper configuration. |
| `/var/lib/pulsar` | read-only | Pulsar home containing `config/genesis.json`. |
| `/var/lib/archive-wrapper` | read-write volume | Persistent wrapper state. |
| `/run/archive-wrapper` | read-write tmpfs | Ephemeral control socket. |

The container can run with a read-only root filesystem. The recommended
configuration uses:

```yaml
read_only: true
volumes:
  - wrapper_data:/var/lib/archive-wrapper
  - ./config.yaml:/etc/archive-wrapper/config.yaml:ro
  - ./pulsar-home:/var/lib/pulsar:ro
tmpfs:
  - /run/archive-wrapper:uid=65532,gid=65532,mode=0700
```

Set these absolute paths in the mounted config:

```yaml
db_path: /var/lib/archive-wrapper/data/leveldb
control_socket_path: /run/archive-wrapper/control.sock
grpc_listen_address: 0.0.0.0:9095
grpc_transport_mode: trusted-network
```

The image pre-creates `/var/lib/archive-wrapper/data` with ownership
`65532:65532`. Docker copies that directory into a newly created named volume.
For a host bind mount, the operator must create the data directory and set its
ownership to `65532:65532` before starting the container.

## Process lifecycle

The image contract is:

```text
ENTRYPOINT: /usr/local/bin/archive-wrapper
CMD: run
stop signal: SIGTERM
```

`POSTGRES_URI` must be injected through the process environment or a container
secret mechanism. The wrapper writes logs to stderr by default. Do not set
`ARCHIVE_WRAPPER_LOG_PATH` unless a writable file mount is intentionally
provided.

The same image command is used for first boot and restart. A fresh volume is
initialized with deployment metadata, while an existing volume is validated
and resumed from its cursor. Reusing a volume for another network, contract, or
start height fails. A second process using the same LevelDB volume fails with a
database lock error.

On `SIGTERM`, the wrapper withdraws query readiness, cancels indexing, closes
the control listener, drains gRPC for up to ten seconds, and closes PostgreSQL
and LevelDB. Container stop timeouts should therefore be greater than ten
seconds; fifteen seconds is the tested minimum.

## Health and networking

The image healthcheck runs:

```sh
archive-wrapper healthcheck --timeout 3s
```

It reads the effective gRPC listener from
`ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS` and the mounted config, then checks the
standard gRPC health record for `query.Query`. Wildcard bind addresses are
dialed through loopback inside the container. `SERVING` is reported only after
a successful reconciliation has produced or recovered a LevelDB cursor.

PostgreSQL connectivity alone does not produce readiness. Initial catch-up,
finality waiting, PostgreSQL loss, and reconnect are correctly non-serving.
The wrapper process remains alive during retryable PostgreSQL outages so the
healthcheck can distinguish unready from crashed.

An archive target below the persisted LevelDB cursor is also non-serving and is
reported as `WAITING_FOR_ARCHIVE`. The wrapper does not rewind or delete the
cursor; it retries until the archive source catches up. Business query RPCs are
rejected with gRPC `Unavailable` while non-serving, while health and diagnostics
remain accessible for recovery inspection.

`trusted-network` permits plaintext gRPC on a controlled private container
network. It records an operator-selected trust boundary; it does not implement
firewalling, authentication, or encryption. TLS/mTLS and public-network
exposure are outside this deployment mode.

## Validation

Run the complete image contract locally with:

```sh
make docker-test
```

The harness uses an isolated Compose project and pinned PostgreSQL fixture. It
checks fresh boot, catch-up, PostgreSQL loss and recovery, restart persistence,
graceful exit, LevelDB locking, non-root/read-only execution, secret redaction,
three concurrent clients, and three independent wrapper instances. It removes
only the containers and volumes created under its unique project name.

Pulsar validator containers, shared/per-validator topology generation, and the
full Pulsar-wrapper end-to-end lifecycle are intentionally deferred to the
next deployment phase.
