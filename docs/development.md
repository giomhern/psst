# Development guide

## Package boundaries

`cmd/psst` and `cmd/psst-server` are executable packages named `main`. Their job is
argument parsing, wiring, and exit handling. Shared application code lives under
`internal/`, which Go prevents unrelated external modules from importing.

The foundation implements `internal/cli`, `internal/domain`, `internal/personal`
(including `sqlite` and `contacts`), `internal/tui`, `internal/server`, and
`internal/sessions/httpapi`. Add these planned packages with their first behavior:

| Planned location | Responsibility |
| --- | --- |
| `internal/personal/applescript` | Explicit automation requests and uncertain outcomes |
| `internal/personal/watcher` | Bounded polling and refresh coordination |
| `internal/sessions/service` | Room, membership, and authorization rules |
| `internal/sessions/store` | PostgreSQL transactions and queries |
| `internal/sessions/realtime` | Connection lifecycle and ordered log dispatch |
| `internal/sessions/cleanup` | Expiration notifications and retention purges |
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
`personal read <chat>` now supports bounded keyset pages with tested date
conversion, safe plain-text output, and an explicit unknown-time ordering.
`--before` continues from the token printed by the previous page. The first Bubble
Tea interface launches with `personal ui`, opens selected conversations, and
loads older pages explicitly with `o`.
Per maintainer preference, Personal UI work precedes the persisted Sessions slice.
Supported attributed-body archives now yield bounded backing text; full rich
formatting and other archive layouts remain future work.
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
single `-addr` flag still uses the standard library. Bubble Tea v2 provides
Personal chat-list and conversation screens.

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

A separate CLI-owned `messageReader` interface requires only `ListMessagePage` and
`Close`. `*personal.Reader` satisfies it implicitly while also satisfying the
chat-list interface. This keeps each consumer's requirements small. The facade
sanitizes driver failures and preserves the meaningful not-found sentinel with
`errors.Is`. The command owns closure and joins close errors into its named error
return. Contacts and native helper calls are not part of reading history.

## History pagination concepts

`domain.MessageCursor` stores a typed boundary; `*MessageCursor` lets nil mean
"start at the newest page." A non-nil cursor with zero `SentAt` instead means the
unknown-time segment. `domain.MessagePage.NextBefore` uses nil to mean no more
rows. SQLite requests `limit+1` and uses the extra row only to detect continuation.
It applies an exclusive timestamp/rowid comparison instead of skipping an OFFSET.
Returned slices reverse each dated/unknown segment for chronological display.

Only the CLI imports `encoding/base64` and `encoding/json` for cursor tokens;
SQL adapters receive typed values and parameterize their key comparisons. Tokens
have a version and strict length/field/value validation. The first-page
`ListMessages` convenience delegates to the same paginated implementation, so
ordering, read-only guards, and error handling do not diverge.

## Bubble Tea interface concepts

`internal/tui` depends on domain values and an injected `Source` containing
`LoadChats` and `LoadMessages` functions, not SQLite or macOS code. Cobra
validates `personal ui --limit` and
supplies a loader that opens, lists and closes a Personal reader for each request.
Production startup requires TTY input/output before starting any Personal access.

Bubble Tea's model satisfies its interface with `Init`, `Update`, and `View`:
Init returns a `tea.Cmd` function; the framework executes that work separately and
sends its `chatsLoaded` result back as a message. Update alone changes selection,
loading/error flags, loaded labels and viewport offsets. View renders a `tea.View`
with `AltScreen` enabled; it performs no I/O. Value receivers return the updated
model explicitly. The list retains IDs and titles. Enter opens a conversation
and a command loads
its latest 20 messages; `o` requests older pages through a typed cursor. Esc
cancels the read, discards the buffer, and returns to
the preserved list selection.

Each screen allows only one load at a time, including manual refresh. A monotonic
request ID travels with every history result: Update ignores results from a
conversation that has since been left or replaced. Cancellation alone cannot
prevent an already completed result from arriving late. The shared admission gate
bounds active callbacks to two, allowing a new screen to load while a canceled read
closes. Further requests fail for manual retry rather than queueing. The runner
captures an operation context in the command closure instead of storing request
contexts in model structs. Each read gets eight seconds; the existing optional
Contacts lookup retains its five-second child budget. On exit the runner cancels
its context and waits for active callbacks. A mutex closes the admission gate
before `WaitGroup.Wait`, preventing a late framework command from adding work
while shutdown waits. The CLI loader closes its reader before returning, so this
join includes connection cleanup. Callback implementations must honor context.

Lip Gloss supplies presentation styles; `x/ansi` supplies display-cell-aware
truncation and `x/term` checks terminal capabilities. Source labels and message
bodies are escaped
before applying trusted styles. Conversation bodies wrap using `x/ansi`, with
scrolling over the retained history and initial position at its bottom. The viewport
and rendered dimensions are bounded;
small terminals show a resize prompt. No pointer to the model is shared with I/O
workers. A cancellable two-second timer now drives live refresh; no database
reader is held for the whole UI session and Sessions remains separate. Confirmed text sends use a separate
optional callback described below.

Tests use synthetic chats for state transitions, navigation, viewport bounds,
refresh/selection retention, safe errors and escaping. A headless program test
runs the actual event loop. Loader cancellation tests exercise blocked cleanup
and admission after shutdown; Cobra tests verify help/invalid arguments do not
launch the UI or open Personal files. Manual pseudo-terminal validation also uses
only synthetic data, including Unicode titles and message bodies.

References: [Bubble Tea v2](https://github.com/charmbracelet/bubbletea),
[v2 declarative views](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md).

History pages now use the same optional Contacts lookup budget as chat titles.
`Message.SenderName` carries a bounded display label separately from `Sender`;
UI and CLI prefer that label while outgoing messages remain “Me.” Each page
looks up only distinct incoming handles. Named groups keep their stored title
while their individual senders resolve independently.

The Messages-inspired layout stays entirely in `internal/tui`. `messageLines`
wraps escaped text into bounded bubbles, then aligns outgoing messages to the
right using display-cell widths. Lip Gloss supplies rounded borders and blue/gray
outlines without background fill. `contentWidth` reserves 26 columns plus a divider for the sidebar when
the terminal is at least 96 columns wide; resizing reflows messages and clamps
the scroll position. The sidebar is context only: Esc returns to chat selection.
These rendering methods perform no I/O and introduce no additional workers.

The TUI formats timestamps in the local timezone while CLI history retains UTC.
`formatMessageTime` is a pure function: it receives both the stored instant and
the current time, making date boundaries and daylight saving behavior testable.
The model receives `time.Now` as a clock function; each render samples it once.
Today/yesterday use calendar dates rather than a 24-hour duration. Older messages
show month/day and include the year when it differs from the current local year.


Attributed-body text uses a small parser inside the SQLite boundary. A `[]byte`
is a slice of raw bytes, distinct from a UTF-8 Go string. `bodyReader` uses pointer
receivers to advance its remaining slice, with bounds checked before every read.
`decodeAttributedBody` returns `(string, bool)`: false means the supported text
field could not be decoded, so the caller retains a placeholder rather than
failing the history page. Byte lengths describe the archive; rune counts enforce
the existing 4,000-character display limit after decoding. Neither UI nor CLI
needs a separate decoder because both consume the same domain message text.
See [the format contract and synthetic fixture setup](sqlite.md#attributed-body-text-extraction).


Older-history commands snapshot the cursor and pass an explicit page limit to the
CLI reader. `history.go` merges slices of domain messages, deduplicates by local
row ID, and sorts by timestamp with numeric row-ID ties; unknown timestamps stay
last. It anchors the viewport by message ID plus the line within that message,
so prepending or moving unknown-time rows does not jump the reader's position.
A read that fails leaves both messages and cursor intact for `o` to retry.

The buffer retains at most 200 messages; each command requests the smaller of
20 or the remaining capacity. At capacity `o` stops, with a visible notice;
`r` replaces the buffer with the latest page. Explicit older-page loading does
not evict messages or automatically fetch when scrolling. Live followers can roll to a new latest window at capacity; readers
keep their buffer and receive a new-messages notice. End-of-history follows the
returned nil cursor, including an empty older page after local deletions. Pages are separate
read snapshots. Live refresh updates matching rows in its latest bounded window;
older rows outside that window remain snapshots.

Pane width follows terminal resize events, subtracting only the outer margins
and the sidebar/divider when present. The former 88-column pane cap is removed.
Message bodies still wrap at at most 56 text cells; outgoing bubbles align to the
right edge of the expanded pane. Resize captures and restores the history anchor
before clamping the viewport. Reported width is bounded at 4,096 columns to
protect rendering allocations from implausible size reports.


## Confirmed Personal sends

`domain.SendStatus` is a small enum made with typed constants and `iota`. Its zero
value means no send was requested; success never defaults to a delivery claim.
`personal.Send` validates the text and local chat ID, opens a read-only target
reader, resolves exactly that row's GUID, and closes it before invoking automation.
Target lookup or close failure prevents the send entirely. No Contacts lookup,
participant guessing, new-chat creation, or fallback routing occurs.

`internal/personal/applescript` uses a fixed script supplied on stdin to the fixed
`/usr/bin/osascript` executable. GUID and text are argv data, never interpolated
into source or a shell. Output is capped at 64 bytes, stderr is discarded, and
only allowlisted response tokens become outcomes. The initial target-lookup
phase may fail as definitely not requested; any unclassified failure after the
send phase starts is unknown. macOS permission denial is a separate safe error.
Process startup failure is known unsent; timeout or process failure after launch
is conservatively unknown. Contexts bound automation to 30 seconds and own
process cleanup; the adapter never retries or exposes script/error payloads.

The UI's optional `Source.Send` capability keeps platform code outside the model.
The composer handles text and paste in Update, with a bounded in-memory draft.
Enter freezes the draft for review; only Ctrl+S on that screen creates a send
command. A pending send blocks other navigation or sends; Ctrl+C can still quit.
The operation gate joins send callbacks as well as reads during shutdown.
Quitting during a pending send can leave an unknown outcome; check Messages
before sending again. No automatic resend or delivery inference from automation acceptance is implemented.

A requested send clears its draft and triggers one recent-history read. A known
unsent failure restores the draft for editing and a fresh confirmation. Unknown
outcomes discard the pending draft and show a check-Messages notice. Safe static
notices never display raw process errors.

The composer stores its cursor as a byte offset because Go string slicing uses
bytes. That offset must always be a grapheme boundary: `rune` represents a Unicode
code point, while an accented character, flag, or joined emoji may contain several
code points. `editor.go` uses the existing pinned `uniseg` dependency directly to
find these boundaries, including after insertions or deletions that join adjacent
clusters. The send limit still counts code points, independently of cursor units.
Like other TUI handlers, these value-receiver methods return the next model to
Bubble Tea's Update loop, which owns all draft changes.

Left/Right and deletion follow graphemes. Word shortcuts traverse Unicode
whitespace-delimited runs in one scan; punctuation remains part of a run. Home/End
follow logical lines, Ctrl+Home/End follow the whole draft, and Up/Down choose the
nearest non-overshooting display column on the adjacent logical line. Soft wraps
are display-only; Up/Down do not treat them as separate logical lines or remember
a preferred column after crossing a shorter line. Ctrl+J provides newline input
when a terminal cannot distinguish Shift+Enter from Enter.

Rendering preserves LF line breaks and emoji zero-width joiners, while escaping
terminal and bidirectional controls before wrapping.
The cursor marker participates in wrapping, and editing or resizing reveals its
row. Review remains read-only, shows the whole draft without the marker, and Esc
returns to the same editing position. Synthetic regression and fuzz tests cover
cluster boundaries, invalid/oversized paste, line navigation, and viewport bounds.
Tests and terminal previews use fake senders; the fixed AppleScript was compiled
for syntax validation without being executed. The maintainer subsequently confirmed a real send worked locally.


## Live Personal refresh

The runner owns one `time.Ticker` and one small goroutine. The goroutine sends
`liveRefreshTick` events to Bubble Tea; only Update starts I/O or changes model
state. The ticker's bounded channel and synchronous `Program.Send` provide
backpressure, and a pending read causes subsequent ticks to be skipped. Shutdown
cancels the parent context, stops the ticker, joins its worker, then joins active
I/O callbacks. Tests inject a tick channel rather than waiting on wall-clock time.

When the list is visible, ticks refresh chat summaries while retaining selection
by chat ID. An open conversation queries up to 100 latest rows through the
existing read-only loader. It merges newly inserted row IDs, updates matching
rows, and preserves chronological/unknown-time ordering. It does not refill the
buffer with older rows merely because the live query has a larger page size.
At the bottom, the viewport follows arrivals; otherwise the message/line anchor
is restored. Composing, confirming, sending, and manual history loads pause polls.
Starting composition or a manual read cancels an active poll and invalidates its
request ID so an already queued result cannot interrupt the new state.

Retained history remains bounded at 200 messages. If the latest snapshot has no
overlap with the buffer after a large burst, or merging would exceed capacity,
a bottom follower replaces the buffer with the fresh latest window and cursor.
An older-history reader keeps the buffer and sees “New messages available · r
latest” rather than having content evicted or a gap silently joined. Poll errors
keep the buffer/cursor intact and retry automatically; sends are never retried.

This is bounded polling, not a complete change log. New rows outside the latest
100 by the reader's existing date ordering may not appear until history is
reloaded. Deletions and edits outside that window are not reconciled, and matching
a local row never establishes delivery or proves which send attempt created it.
No real Messages data is needed in tests or terminal previews.

The minimum toolchain is Go 1.27.3, including the standard-library certificate
verification update in [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809).
CI selects the same patch version from `go.mod`.


## Personal send receipts

`domain.SendResult` keeps automation acceptance separate from `SendReceipt`.
`DeliveryStatus` is a small typed enum: pending, sent, delivered, failed, or unknown
local evidence. A database observation never changes whether automation was
accepted and never authorizes another send.

Before automation, the Personal adapter captures the global maximum message ROWID
and resolves the exact selected chat GUID, then closes its reader. If checkpoint
capture fails, the confirmed send can still proceed without tracking. After an
accepted request, it reopens a read-only reader for six checks, 500 ms apart, with
a five-second observation deadline under the existing overall send deadline. The
small window allows bodies and join links to appear and competing identical rows
to become visible. At most 100 new outgoing rows in the original chat are decoded;
a 101st row or multiple matches makes observation unavailable. Matching requires
exact plain or supported attributed text and a known timestamp between send-start
minus two seconds and observation time plus two seconds. Two seconds is a clock
and timestamp-granularity tolerance, not a delivery timeout. No match remains
unconfirmed. GUID changes between observations also discard the association.

This is deliberately a best-effort correlation, not proof of send causality:
AppleScript exposes no message identifier. Identical sends from another device,
late synchronization, backdated rows, unsupported rich-text encodings, database
replacement, or changes after the short observation window can defeat matching.
The checkpoint is an insertion filter, not an exact insertion timestamp. No schema
changes, indexes, or writes are made to Apple's database. Message text stays only
in the existing in-memory command for this bounded comparison; no receipt stores
or logs it. GUIDs remain in memory and must not be logged.

Once matched, the UI's optional `Source.Receipt` capability follows the GUID in
its chat. All SQL values are bound; status columns are detected from a fixed
allowlist on each read. A nonzero integer error means failed unless it contradicts
delivery evidence; `is_sent=1` means sent; `is_delivered=1` or a supported positive
`date_delivered` means delivered. Missing, null, malformed, or contradictory data
is unknown. Pending requires usable `is_sent` and `error` fields with no sent/error
evidence. No timestamp absence or timeout implies failure. Group status is not a
per-recipient receipt. Private schema semantics may change with macOS.

Receipt commands wait two seconds, then perform a read with a three-second limit,
for at most 30 checks and within a one-minute deadline. A separate operation gate
owns waiting and I/O so it cannot occupy the history/send gate, and shutdown
cancels and joins both gates. Update owns receipt state, the deadline, a generation
counter, and the cancellation function. Starting a new draft or leaving the chat
cancels tracking and invalidates late results. Terminal delivered/failed evidence
ends polling; tracking errors retain prior sent evidence without claiming failure.
Only the current send footer is tracked; this is not persistent receipt history.
Synthetic tests cover selection ambiguity, rich text, WAL-visible changes, schema
fallback, contradictory evidence, finite polling, cancellation, and shutdown joins.
No real Messages access or automated real send was used to validate this change.
