# Development guide

## Package boundaries

`cmd/psst` and `cmd/psst-server` are executable packages named `main`. Their job is
argument parsing, wiring, and exit handling. Shared application code lives under
`internal/`, which Go prevents unrelated external modules from importing.

The foundation implements `internal/cli`, `internal/server`, and
`internal/sessions/httpapi`. Add these planned packages with their first behavior:

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

Add macOS-safe permission and schema probes to `doctor` without printing content.
Then introduce a read-only SQLite adapter with synthetic fixtures. Build session
persistence before delivery acknowledgements, using real PostgreSQL integration
tests for transaction and locking behavior. Add dependencies and migrations only
when the corresponding slice needs them.

The current health endpoint does not require a database. When persistence arrives,
add a separate readiness contract instead of turning liveness into a database probe.
Before WebSockets arrive, update HTTP timeouts and shutdown ownership to account
for long-lived upgraded connections.
