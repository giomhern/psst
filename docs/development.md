# Development guide

## Package boundaries

`cmd/psst` and `cmd/psst-server` are executable packages named `main`. Their job is
argument parsing, wiring, and exit handling. Shared application code lives under
`internal/`, which Go prevents unrelated external modules from importing.

The foundation implements `internal/cli`, `internal/personal` (including `sqlite`),
`internal/server`, and `internal/sessions/httpapi`. Add these planned packages with their first behavior:

| Planned location | Responsibility |
| --- | --- |
| `internal/domain` | Shared values genuinely needed by more than one capability |
| `internal/personal/applescript` | Explicit automation requests and uncertain outcomes |
| `internal/personal/watcher` | Bounded polling and refresh coordination |
| `internal/sessions/service` | Room, membership, and authorization rules |
| `internal/sessions/store` | PostgreSQL transactions and queries |
| `internal/sessions/realtime` | Connection lifecycle and ordered log dispatch |
| `internal/sessions/cleanup` | Expiration notifications and retention purges |
| `internal/tui` | Bubble Tea model, commands, and update loop |
| `internal/config` | Validated configuration once settings need a shared owner |

Dependencies flow from entry points and adapters toward application rules. Avoid
one generic provider with unsupported methods; interfaces belong with the code
that consumes them. Keep test files next to their packages and synthetic fixtures
under a package's `testdata/` when needed.

## Go concepts in this slice

- A **function** such as `cli.Run` groups an operation. A **method** belongs to a
  receiver type: `srv.Shutdown(ctx)` operates on a particular `http.Server`.
- `io.Writer` is a small standard interface. Passing one into the CLI keeps output
  explicit and allows a buffer in tests instead of hardcoding terminal access.
- `context.Context` carries cancellation. The server listens for OS signals and
  uses a separate bounded context to finish shutdown after cancellation.
- The HTTP serving **goroutine** runs concurrently. A buffered channel returns its
  result; the owner waits for it before returning, preventing an abandoned worker.
- `http.Handler` is an interface implemented by the standard router. We return
  that interface at the HTTP boundary without building an extra custom abstraction.

## Next runnable slices

The macOS doctor now checks filesystem access and the SQLite schema read-only.
Next, introduce bounded conversation listing through a small consumer-owned reader
interface and Cobra commands, backed by the adapter and synthetic fixtures.
Build session persistence before delivery acknowledgements, using real PostgreSQL integration
tests for transaction and locking behavior. Add dependencies and migrations only
when the corresponding slice needs them.

The current health endpoint does not require a database. When persistence arrives,
add a separate readiness contract instead of turning liveness into a database probe.
Before WebSockets arrive, update HTTP timeouts and shutdown ownership to account
for long-lived upgraded connections.

## Filesystem diagnostic concepts

`personal.DatabaseCheck` carries a typed status and safe explanation, including
`compatible` or `unsupported_schema` after SQLite inspection.
`errors.Is` recognizes missing/permission errors even inside `fs.PathError`, so
raw paths and error text do not need to reach the terminal. The probe accepts
`fs.FS`, whose API opens files for reading; macOS supplies `os.DirFS(home)`.
Tests use temporary directories and simulated open failures, including EPERM and
EACCES, and fail if the probe tries to read any file content.

`diagnostics_darwin.go` is selected by Go's filename platform convention.
`diagnostics_other.go` uses `//go:build !darwin` for the unsupported-platform path.
The CLI accepts a context-aware probe function dependency internally, so CLI tests
never fall through to a user's real Messages directory. Filesystem metadata and opening
alone do not establish SQLite compatibility or the cause of a permission denial.

## SQLite concepts in this slice

- `database/sql` manages a pool of driver connections. `sql.Open` is lazy; the
  adapter runs a query to establish a connection and verify `query_only` before
  returning. Connection options apply again when the pool replaces a connection.
- The blank import `_ "modernc.org/sqlite"` registers the driver with `database/sql`.
  This is an intentional registration side effect, not an unused ordinary import.
- `Reader` owns the private pool. Pointer-receiver methods `InspectSchema(ctx)`
  and `Close()` operate on that reader; callers must close it when finished.
- `QueryRowContext(...).Scan(&value)` decodes one result into a variable. For
  multiple rows, use `QueryContext`, iterate with `rows.Next`, scan fields, and
  check `rows.Err`. Close rows promptly so the one-connection pool can continue.
- `?` placeholders bind table names as values to SQLite's metadata functions.
  We do not concatenate user input into SQL or connection options.
- A read transaction gives the metadata checks one consistent snapshot. Deferred
  rollback releases it on early failure; commit releases it after successful
  inspection. No data-changing SQL is part of this transaction.
- The CLI passes a signal-cancellable context with a bounded probe deadline.
  Database cancellation and the busy timeout keep lock contention bounded.

See [SQLite safety and test contracts](sqlite.md) before extending this adapter.
