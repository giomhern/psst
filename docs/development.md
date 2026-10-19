# Development guide

## Package boundaries

`cmd/psst` and `cmd/psst-server` are executable packages named `main`. Their job is
argument parsing, wiring, and exit handling. Shared application code lives under
`internal/`, which Go prevents unrelated external modules from importing.

The foundation implements `internal/cli`, `internal/domain`, `internal/personal`
(including `sqlite`), `internal/server`, and `internal/sessions/httpapi`. Add these planned packages with their first behavior:

| Planned location | Responsibility |
| --- | --- |
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
Bounded conversation listing now runs through a small CLI-owned reader interface
and Cobra commands, backed by the adapter and synthetic fixtures. A first
`personal read <chat>` now returns one bounded recent page with tested date
conversion and safe plain-text output. Next, add keyset pagination under the same
normalized timestamp/message-ID ordering, specifying how unknown dates continue.
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

## Cobra and conversation-listing concepts

Cobra represents each command as a `cobra.Command` value. `AddCommand` builds the
`psst → personal → chats` tree. `Args: cobra.NoArgs` rejects positional arguments
before work starts; `RunE` executes an operation and returns its error. The command
binds `--limit` to a local variable using `IntVar(&limit, ...)`, then validates it
before opening a database. Commands are constructed afresh per execution; there
are no global command objects, mutable global flags, or `init` registration hooks.

The CLI defines the interface it needs:

```go
type conversationReader interface {
    ListConversations(context.Context, int) ([]domain.Conversation, error)
    Close() error
}
```

`*personal.Reader` satisfies it implicitly. Tests supply a stub implementing the
same methods, so help and validation tests never inspect real Messages data.
An opener function delays database acquisition until the chosen command runs.
`ExecuteContext` passes cancellation down to the operation and SQLite queries.

The adapter converts SQL rows into `domain.Conversation` values. `sql.NullInt64`
can represent the absent latest-message row for an empty chat. The data model
contains no SQL driver types, so a later TUI can consume it independently.

`listChats` has a named error result so a deferred function can combine a close
failure with an existing output/query failure using `errors.Join`. `tabwriter`
aligns output columns; `strconv.QuoteToGraphic` escapes title control characters
while preserving printable Unicode. Titles are intended command output, never logs.
Cobra's error and usage printing are silenced so `main` reports failures once.

See [Cobra's user guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)
for command and flag APIs. We use Cobra for the client; the server's existing
single `-addr` flag still uses the standard library. Bubble Tea is not added yet.

## Contact-name resolution

The domain summary carries an `ExplicitTitle` marker and bounded participant
handles for unnamed chats. SQLite fetches at most five distinct handles (plus one
to detect overflow) in the read transaction, without selecting message bodies.
Explicit names skip participant enumeration. Contact matching happens after commit.

`personal` owns a `nameResolver` interface containing one `Lookup` method. Tests
supply a fake; production uses `contacts.Client`. A Go `map[string]string` maps
requested handles to names. Optional lookup failures fall back; parent cancellation
still stops the command.

The Swift helper is a small platform boundary for Apple's Contacts framework.
Go uses `exec.CommandContext` with a fixed executable and action, plus JSON on
stdin/stdout. Handles never become shell source, arguments, environment values,
logs, or persisted name-cache entries. Buffers and deadlines are bounded. A custom
`io.Writer` caps output without embedding a buffer API that could bypass that cap
through `io.ReaderFrom`. Raw stderr and process errors are not shown.

Only `personal contacts authorize` invokes the native permission request. Normal
lookup checks authorization and returns a status if unavailable. Native synthetic
name tests cover ambiguity and Unicode bounds without opening the contact store.
CI builds the helper and tests on macOS; ordinary Go checks also run on Linux.

## Recent message history concepts

`domain.Message` uses `time.Time`; the zero value represents an unknown timestamp,
and UTC formatting happens at the CLI boundary. `sql.NullInt64` and
`sql.NullString` distinguish SQL NULLs while scanning rows. The SQLite adapter
normalizes supported date representations before choosing a page, then reads only
bounded plain text for those selected messages. Its transaction owns all queries;
it ends before the result leaves the adapter.

A separate CLI-owned `messageReader` interface requires only `ListMessages` and
`Close`. `*personal.Reader` satisfies it implicitly while also satisfying the
chat-list interface. This keeps each consumer's requirements small. The facade
sanitizes driver failures and preserves the meaningful not-found sentinel with
`errors.Is`. The command owns closure and joins close errors into its named error
return. Contacts and native helper calls are not part of reading history.
