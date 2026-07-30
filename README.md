# archive-wrapper

`archive-wrapper` is a local sidecar for a Mina validator node. It reads a
configured zkApp's actions from the archive PostgreSQL database, follows the
confirmed best chain, stores indexed actions and cursor metadata in LevelDB,
and exposes read-only gRPC queries for the validator application.

The sidecar is intended to remain local to the validator host. The sample
configuration binds gRPC to `127.0.0.1:9095`; keep it on a loopback address
unless the deployment has an equivalent local access boundary.

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
| `control_socket_path` | Unix socket used by the `stop` command. |

The first start persists the `bridge.start_block_height` value loaded from
Pulsar genesis as the earliest indexed height. Later starts must see the same
genesis value when reusing an existing LevelDB directory. The latest cursor is
advanced only after a block has been fetched successfully; action-bearing
blocks are stored before the cursor update. A missing best-chain block or a
temporary PostgreSQL failure leaves the cursor available for retry after
restart or reconnection.

## Build and run

Prefer the Makefile targets for day-to-day use.

Create a local `.env` file in the repository root first. The Makefile includes
it, and the executable also loads it at runtime.

Example:

```dotenv
POSTGRES_URI=postgres://user:password@127.0.0.1:5432/archive?sslmode=disable
CHAIN_HOME=/path/to/validator
CONFIG=config.yaml
```

`CHAIN_HOME` is used by the Makefile. `CONFIG` is optional if you want to use
the default `config.yaml`.

Build the pure-Go binary with:

```sh
make build
```

Start the sidecar by providing the validator chain home:

```sh
make start
```

If you want a non-default wrapper config file, pass it explicitly:

```sh
make start CONFIG=/path/to/config.yaml
```

`start` reads `bridge.contract_address`, `bridge.confirmation_depth`,
`bridge.start_block_height`, and `bridge.max_block_range` from
`/path/to/validator/config/genesis.json`, catches up to the archive tip minus
that depth, and then follows the PostgreSQL `blocks_inserted` notifications.
Query and notification connection failures are retried while the process is
running.

If the LevelDB already has a persisted cursor and you want to resume
explicitly from it, use:

```sh
make proceed
```

`proceed` requires an existing latest processed block height in LevelDB. It
keeps the genesis start height for bounds validation, then resumes indexing
from the persisted cursor already stored in the local database.

Stop the running process through its Unix control socket:

```sh
make stop
```

If you need a custom config or socket path while stopping:

```sh
make stop CONFIG=/path/to/config.yaml SOCKET_PATH=/tmp/archive-wrapper.sock
```

The control socket accepts `PING` (responding with `PONG`) and `STOP`. The
process also shuts down gracefully on `SIGINT` or `SIGTERM`. The `stop`
command can use `--socket-path` directly; otherwise it resolves the socket
from `--config`, `ARCHIVE_WRAPPER_CONFIG`, or
`ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH`.

### Without Makefile

If you need to bypass the Makefile, the equivalent direct commands are:

```sh
./archive-wrapper start --config config.yaml --home /path/to/validator

./archive-wrapper proceed --config config.yaml --home /path/to/validator

./archive-wrapper stop --config config.yaml
```

For local development, you can also run the CLI without building first:

```sh
go run ./cmd/archive-wrapper start --config config.yaml --home /path/to/validator
```

The executable loads `.env` when present. The supported environment variables
are:

- `POSTGRES_URI` — archive PostgreSQL connection string; required for `start`
  and `proceed`.
- `ARCHIVE_WRAPPER_CONFIG` — default config path for `start`, `proceed`, and
  `stop`.
- `ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH` — fallback socket path for `stop`.
- `ARCHIVE_WRAPPER_LOG_PATH` — append-only log path; defaults to
  `archive-wrapper.log`.

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
must stay bound to a trusted local interface.

## Development checks

```sh
make fmt
make lint
buf lint
```

`make lint` runs `golangci-lint` with the `purego` build tag, while `buf lint`
checks the protobuf sources.
