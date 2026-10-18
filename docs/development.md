# Development guide

## Package boundaries

`cmd/psst` and `cmd/psst-server` are executable packages named `main`. Their job is
argument parsing, wiring, and exit handling. Shared application code lives under
`internal/`, which Go prevents unrelated external modules from importing.

The foundation implements `internal/cli`, `internal/personal`, `internal/server`,
and `internal/sessions/httpapi`. Add these planned packages with their first behavior:

| Planned location | Responsibility |
| --- | --- |
| `internal/domain` | Shared values genuinely needed by more than one capability |
| `internal/personal/sqlite` | Read-only Messages queries and schema detection |
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

The macOS filesystem probe now reports read-only file access without reading content.
Next, introduce a read-only SQLite adapter and schema probes with synthetic fixtures.
Build session persistence before delivery acknowledgements, using real PostgreSQL integration
tests for transaction and locking behavior. Add dependencies and migrations only
when the corresponding slice needs them.

The current health endpoint does not require a database. When persistence arrives,
add a separate readiness contract instead of turning liveness into a database probe.
Before WebSockets arrive, update HTTP timeouts and shutdown ownership to account
for long-lived upgraded connections.

## Filesystem diagnostic concepts

`personal.DatabaseCheck` is a struct carrying a typed status and safe explanation.
`errors.Is` recognizes missing/permission errors even inside `fs.PathError`, so
raw paths and error text do not need to reach the terminal. The probe accepts
`fs.FS`, whose API opens files for reading; macOS supplies `os.DirFS(home)`.
Tests use temporary directories and simulated open failures, including EPERM and
EACCES, and fail if the probe tries to read any file content.

`diagnostics_darwin.go` is selected by Go's filename platform convention.
`diagnostics_other.go` uses `//go:build !darwin` for the unsupported-platform path.
The CLI accepts the probe as a function dependency internally, so CLI tests never
fall through to a user's real Messages directory. Filesystem metadata and opening
alone do not establish SQLite compatibility or the cause of a permission denial.
