# Psst

Terminal-native messaging, built in Go and developed one understandable step at a time.

Psst has two separate experiences:

- **Personal:** a planned read-only view of locally synchronized macOS Messages,
  with explicit, best-effort sending through macOS automation added later.
- **Sessions:** a planned hosted network of short-lived, invite-only chat rooms,
  independent of iMessage and backed by PostgreSQL.

Personal history stays on your Mac. It is never automatically copied into Sessions.
Hosted Sessions are not end-to-end encrypted in the initial design.

## Current status

This is the **foundation scaffold**, not a working messaging client yet.

Available now: CLI help, content-free platform diagnostics, a local HTTP server
with a liveness endpoint, graceful shutdown, development PostgreSQL setup, tests,
and CI. The server does not connect to PostgreSQL yet. Messages access, permission
probes, authentication, room APIs, WebSockets, and the TUI are not implemented.

## Quick start

Install Go **1.27.2 or newer**. Docker with Compose is optional until database work
begins. Personal will require macOS; the Sessions server targets Linux and macOS.
The scaffold has no third-party runtime dependencies.

```sh
git clone https://github.com/giomhern/psst.git
cd psst
go run ./cmd/psst --help
go run ./cmd/psst doctor
```

Run the development server:

```sh
go run ./cmd/psst-server
```

In another terminal:

```sh
curl --fail http://127.0.0.1:8080/healthz
# {"status":"ok"}
```

Stop with Ctrl-C. Change the listening address with `-addr 127.0.0.1:9090`.
The default listener is loopback HTTP for local development. A production deployment
will require HTTPS/WSS and the authentication/lifecycle controls in the architecture.
`/healthz` indicates process liveness only, not database readiness.

## Development

```sh
make check      # Formatting check, vet, tests, and package builds
make race       # Tests with the Go race detector (requires a C toolchain)
make lint       # Pinned Staticcheck tool
make security   # Pinned govulncheck tool; queries the vulnerability database
make build      # Produce bin/psst and bin/psst-server
```

`make fmt` formats Go source. Ordinary builds and tests need only Go; Make wraps
standard commands. Lint/security tools download on first use. CI runs Go checks
on Linux and macOS and validates the Compose configuration.

Optional local database:

```sh
make db-up
# Work on PostgreSQL integration when that slice is implemented.
make db-down
```

Compose binds PostgreSQL to `127.0.0.1:5432`. The database/user are `psst`; the
password `psst_local_only` is disposable development configuration, never a
production credential. Data survives `make db-down` in a named Docker volume.
To choose another port, copy `.env.example` to `.env` and change
`PSST_POSTGRES_PORT`. The Go binaries do not load `.env` files.

## Repository layout

```text
cmd/
  psst/                    CLI executable
  psst-server/             Server executable
internal/
  cli/                     Command dispatch and foundation doctor output
  server/                  HTTP startup and shutdown ownership
  sessions/httpapi/        HTTP router and endpoint tests
migrations/                Migration conventions; schema work comes next
tests/integration/         Integration-test conventions
docs/                      Development guide and planned package boundaries
.github/workflows/         CI checks
```

Packages are added when they have behavior; we avoid empty interfaces and placeholder
implementations. See the [development guide](docs/development.md) for planned packages.

## Implementation path

1. Foundation and privacy-preserving `doctor` probes.
2. Read-only Personal queries against synthetic SQLite fixtures.
3. A persisted Sessions slice: credentials, invites, transactional sends, ordered
   fanout, and reconnect catch-up.
4. Moderation, cleanup, failure testing, and the unified Bubble Tea interface.
5. Explicit Personal sends, browser guests, and measured scaling work.

The [architecture and protocol](architecture-design-v0.1.md) define the complete
plan and acceptance criteria. The filename is historical; the document is v0.3.
[AGENTS.md](AGENTS.md) defines Go conventions, the learning workflow, privacy
constraints, and frequent verified commits and pushes.
