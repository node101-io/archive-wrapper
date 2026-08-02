# archive-wrapper

`archive-wrapper` is a local sidecar for a Mina validator node. It reads a
configured zkApp's actions from the archive PostgreSQL database, follows the
confirmed best chain, stores indexed actions and cursor metadata in LevelDB,
and exposes read-only gRPC queries for the validator application.

The sidecar defaults to a local-only gRPC transport. `loopback` accepts only a
literal loopback address such as `127.0.0.1:9095` or `[::1]:9095`. For a shared
Docker/private network, `trusted-network` must be explicitly selected; it may
bind to private or unspecified addresses and uses plaintext gRPC. This mode is
an operator-declared trust boundary, not a firewall or network-isolation check.
Deployments outside a controlled private network require a future TLS/mTLS
transport.

## Requirements

- Go 1.26 for building the binary.
- A Pulsar node home whose `config/genesis.json` contains `app_state.bridge.params`.
- A Pulsar genesis whose `bridge` module params contain a non-empty
  `contract_address`, a positive `confirmation_depth`, and a positive
  `max_block_range`, plus a positive `start_block_height`.
- An archive PostgreSQL database reachable through `POSTGRES_URI`.

The archive database must provide the tables and relationships queried by
[`fetchmina/sql/queries.sql`](fetchmina/sql/queries.sql).

## Configuration

[`config.yaml`](config.yaml) is the example configuration file. The required
fields are:

| Field | Purpose |
| --- | --- |
| `block_height_database_key` | LevelDB key for the latest processed cursor. |
| `db_path` | Local LevelDB directory. |
| `grpc_listen_address` | TCP address for the local query server. |
| `grpc_transport_mode` | `loopback` by default, or explicit `trusted-network`. |
| `chain_home` | Optional Pulsar home; `run` requires it from config, CLI, or environment. |
| `control_socket_path` | Unix socket used by the `run` and `stop` commands. |
| `deployment_metadata_key` | LevelDB key used to store deployment identity. |
| `deployment_metadata.schema_version` | Stored deployment metadata schema version. |
| `deployment_metadata.mina_network_id` | Mina network whose archive database is indexed. |

`run` binds a fresh LevelDB database to its schema version, Mina network,
contract address, and genesis start height. On restart, the same command
validates that identity and resumes from the persisted cursor. A database with
mismatched metadata, application state without metadata, malformed metadata or
cursor data, or a lock held by another process fails before PostgreSQL or gRPC
is opened. To change deployment identity, use a new `db_path`.

The latest cursor is advanced only after a block has been fetched successfully;
action-bearing blocks and their cursor are committed atomically. A missing
best-chain block or a temporary PostgreSQL failure leaves the cursor available
for retry after restart or reconnection.

## Build and run

Prefer the Makefile targets for day-to-day use.

For local Makefile usage, values may be placed in a repository-root `.env`
file. The Makefile includes this file; the executable itself never loads
`.env`. Container and direct binary deployments must inject environment
variables through their process environment.

Example:

```dotenv
POSTGRES_URI=postgres://user:password@127.0.0.1:5432/archive?sslmode=disable
ARCHIVE_WRAPPER_CHAIN_HOME=/path/to/validator
CONFIG=config.yaml
```

`CHAIN_HOME` remains a Makefile convenience variable. The binary uses
`ARCHIVE_WRAPPER_CHAIN_HOME`; `CONFIG` is a Makefile convenience variable and
the binary requires `--config` or `ARCHIVE_WRAPPER_CONFIG`.

Build the pure-Go binary with:

```sh
make build
```

Run the sidecar. `CHAIN_HOME` is optional when chain home is already supplied by
`chain_home` or `ARCHIVE_WRAPPER_CHAIN_HOME`:

```sh
make run CHAIN_HOME=/path/to/validator
```

If you want a non-default wrapper config file, pass it explicitly:

```sh
make run CONFIG=/path/to/config.yaml CHAIN_HOME=/path/to/validator
```

`run` reads `bridge.contract_address`, `bridge.confirmation_depth`,
`bridge.start_block_height`, and `bridge.max_block_range` from
`/path/to/validator/config/genesis.json`, catches up to the archive tip minus
that depth, and then follows the PostgreSQL `blocks_inserted` notifications.
Notifications are wake-up signals only: after each notification, the wrapper
queries the authoritative archive tip and reconciles from its persisted LevelDB
cursor. Payload contents, duplicate notifications, and coalesced notifications
do not determine the indexed range. Query and notification connection failures
are retried while the process is running.

The same `make run` command is used for first boot and every restart. It accepts
an initialized LevelDB even before its first cursor is written, resumes from the
cursor when present, or continues initial sync from the genesis start height.

Stop the running process through its Unix control socket:

```sh
make stop
```

If you need a custom config or socket path while stopping:

```sh
make stop CONFIG=/path/to/config.yaml CONTROL_SOCKET_PATH=/tmp/archive-wrapper.sock
```

The control socket accepts `PING` (responding with `PONG`) and `STOP`. The
process also shuts down gracefully on `SIGINT` or `SIGTERM`. The `stop`
command can use `--control-socket-path` directly. Otherwise it resolves the
socket from `ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH` and then the selected config
file.
Shutdown first publishes `NOT_SERVING`, then cancels indexing and closes the
control listener. gRPC receives a 10-second graceful drain period before active
RPCs are forcibly stopped; PostgreSQL and LevelDB are closed afterward.

### Without Makefile

If you need to bypass the Makefile, the equivalent direct commands are:

```sh
./archive-wrapper run --config config.yaml --home /path/to/validator

./archive-wrapper stop --config config.yaml
```

For local development, you can also run the CLI without building first:

```sh
go run ./cmd/archive-wrapper run --config config.yaml --home /path/to/validator
```

The supported process environment variables are:

- `POSTGRES_URI` — archive PostgreSQL connection string; required for `run`.
- `ARCHIVE_WRAPPER_CONFIG` — default config path for `run` and `stop`.
- `ARCHIVE_WRAPPER_CHAIN_HOME` — Pulsar chain home for `run`.
- `ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS` — effective gRPC listen address.
- `ARCHIVE_WRAPPER_GRPC_TRANSPORT_MODE` — effective gRPC transport mode.
- `ARCHIVE_WRAPPER_DB_PATH` — effective LevelDB path.
- `ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH` — fallback socket path for `stop`.
- `ARCHIVE_WRAPPER_LOG_PATH` — optional append-only log path. Logs always go to
  `stderr`; when this value is non-empty they are also written to the selected
  file. Newly created log files use mode `0600`.

## gRPC queries

The query service is defined in
[`proto/query/query.proto`](proto/query/query.proto):

- `GetMinaBlockHeight` returns the latest block successfully indexed into the
  local LevelDB store.
- `GetActionsInRange` returns `DEPOSIT` and `WITHDRAW` actions for an inclusive
  indexed range. The requested range must stay within the persisted earliest
  and latest indexed heights, and cannot exceed the `bridge.max_block_range`
  value loaded from Pulsar genesis at startup.

Blocks with no supported actions are still recorded by advancing the cursor.
The gRPC endpoint has no public authentication or authorization layer, so it
must stay in the boundary selected by `grpc_transport_mode`. In
`trusted-network` mode, network isolation and access control are deployment
responsibilities.

### Health and diagnostics

The standard gRPC health service reports whether the query API is ready for
use. Both the overall server (`""`) and `query.Query` remain `NOT_SERVING`
during initial PostgreSQL connection, initial catch-up, finality waiting, and
PostgreSQL reconnection. They become `SERVING` only after a successful sync has
created or recovered the local indexed-height cursor. A successful PostgreSQL
Ping proves connectivity but does not make the query API ready.

The read-only `diagnostics.DiagnosticsService` remains available while the
query API is starting or reconnecting. It reports the operational state,
archive and target heights, indexed height, confirmed lag, and timestamps for
the latest successful sync and operational error. Supported states are
`STARTING`, `CONNECTING`, `SYNCING`, `WAITING_FOR_FINALITY`, `READY`,
`RECONNECTING`, `FAILED`, and `STOPPING`.

For example, with the default listen address:

```sh
grpcurl -plaintext \
  -d '{"service":"query.Query"}' \
  127.0.0.1:9095 grpc.health.v1.Health/Check

grpcurl -plaintext \
  -d '{}' \
  127.0.0.1:9095 diagnostics.DiagnosticsService/GetStatus
```

Health status is advisory and does not reject query RPCs server-side. Clients
that require readiness gating must check or enable gRPC health checking.

## Development checks

```sh
make fmt
make lint
make proto
buf lint
```

`make lint` runs `golangci-lint` with the `purego` build tag, `make proto`
regenerates checked-in protobuf Go code, and `buf lint` checks the protobuf
sources.
