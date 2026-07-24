# archive-wrapper

`archive-wrapper` is a local sidecar for a Mina validator node. It reads a
configured zkApp's actions from the archive PostgreSQL database, follows the
confirmed best chain, stores indexed actions and cursor metadata in LevelDB,
and exposes read-only gRPC queries for the validator application.

The sidecar is intended to remain local to the validator host. The sample
configuration binds gRPC to `127.0.0.1:9090`; keep it on a loopback address
unless the deployment has an equivalent local access boundary.

## Requirements

- Go 1.26 for building the binary.
- A Mina chain home whose `config/app.toml` contains a positive
  `confirmation_depth`.
- An archive PostgreSQL database reachable through `POSTGRES_URI`.

The archive database must provide the tables and relationships queried by
[`fetchmina/sql/queries.sql`](fetchmina/sql/queries.sql).

## Configuration

[`config.yaml`](config.yaml) is the example configuration file. The required
fields are:

| Field | Purpose |
| --- | --- |
| `contract_address` | Mina zkApp address whose actions are indexed. |
| `block_height_database_key` | LevelDB key for the latest processed cursor. |
| `db_path` | Local LevelDB directory. |
| `grpc_listen_address` | TCP address for the local query server. |
| `control_socket_path` | Unix socket used by the `stop` command. |
| `max_action_range_heights` | Maximum number of inclusive block heights accepted by one range query. |

The first start persists the requested `--start-block-height` as the earliest
indexed height. Later starts must use the same value with the existing LevelDB
directory. The latest cursor is advanced only after a block has been fetched
successfully; action-bearing blocks are stored before the cursor update. A
missing best-chain block or a temporary PostgreSQL failure leaves the cursor
available for retry after restart or reconnection.

## Build and run

Build the pure-Go binary with:

```sh
make build
```

Start the sidecar by providing the validator chain home and the first height
to index:

```sh
POSTGRES_URI='postgres://user:password@127.0.0.1:5432/archive?sslmode=disable' \
  ./archive-wrapper start \
  --config config.yaml \
  --home /path/to/validator \
  --start-block-height 537276
```

`start` reads `confirmation_depth` from
`/path/to/validator/config/app.toml`, catches up to the archive tip minus that
depth, and then follows the PostgreSQL `blocks_inserted` notifications. Query
and notification connection failures are retried while the process is
running.

Stop the running process through its Unix control socket:

```sh
./archive-wrapper stop --config config.yaml
```

The control socket accepts `PING` (responding with `PONG`) and `STOP`. The
process also shuts down gracefully on `SIGINT` or `SIGTERM`. The `stop`
command can use `--socket-path` directly; otherwise it resolves the socket
from `--config`, `ARCHIVE_WRAPPER_CONFIG`, or
`ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH`.

The executable loads a `.env` file when present. The supported environment
variables are:

- `POSTGRES_URI` — archive PostgreSQL connection string; required for `start`.
- `ARCHIVE_WRAPPER_CONFIG` — default config path for `start` and `stop`.
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
  and latest indexed heights, and cannot exceed `max_action_range_heights`.

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
