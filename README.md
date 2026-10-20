# Psst

Terminal-native messaging, built in Go and developed one understandable step at a time.

Psst has two separate experiences:

- **Personal:** a read-only view of locally synchronized macOS Messages,
  with explicit, best-effort sending through macOS automation added later.
- **Sessions:** a planned hosted network of short-lived, invite-only chat rooms,
  independent of iMessage and backed by PostgreSQL.

Personal history stays on your Mac. It is never automatically copied into Sessions.
Hosted Sessions are not end-to-end encrypted in the initial design.

## Current status

This is an early client with read-only Personal browsing and a Sessions server scaffold.

Available now: Cobra CLI commands, read-only conversation listing and recent plain-text
message history, a Bubble Tea chat and conversation interface, optional local contact names, privacy-preserving
file-access and SQLite schema diagnostics, a local HTTP server with a liveness endpoint, graceful shutdown, development
PostgreSQL setup, tests, and CI. The server does not connect to PostgreSQL yet.
Rich message decoding, authentication, room APIs, WebSockets,
and older-page loading inside the TUI are not implemented.

## Quick start

Install Go **1.27.2 or newer**. Docker with Compose is optional until database work
begins. Personal requires macOS; the Sessions server targets Linux and macOS.
The CLI uses Cobra and the Personal adapter uses the pinned pure-Go
`modernc.org/sqlite` driver; no C compiler is required for ordinary builds. Dependencies download on first use.

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

## Interactive Personal browsing

```sh
go run ./cmd/psst personal ui
go run ./cmd/psst personal ui --limit 100
```

The Bubble Tea interface lists recent chats, with optional contact names,
in an alternate terminal screen. It loads at most 50 chats by default; `--limit`
accepts 1–100. Use an interactive terminal for both input and output.

| Key | Action |
| --- | --- |
| Up / Down or `k` / `j` | Move selection / scroll messages |
| Home / End or `g` / `G` | First / last loaded chat or message line |
| Page Up / Page Down | Move one visible page |
| Enter | Open selected conversation at the latest messages |
| `r` | Refresh the current screen |
| Esc | Return to chats; quit from the chat list |
| `q`, Ctrl-C | Quit |

The selection follows its chat ID across refreshes. Reads run outside the UI
update loop, have an eight-second deadline, and close their reader after each
load. Refresh is manual; repeated refresh keys do not queue extra reads. On a
refresh failure, previous results remain visible. Quit cancels and joins active
reads before the UI entry point returns, and Bubble Tea restores the terminal.
Titles and message content are escaped before styling; message text wraps to the
available display width. Returning to the list discards the loaded message page.
The UI writes no transcript to disk and performs no message sending or network upload.

Conversations show up to 20 recent messages in chronological order (UTC), with
optional contact names (falling back to sender identifiers) and placeholders for attachments or unsupported rich content.
Unknown times follow known times. Scroll within this page using the keys above;
older-page loading is a later increment. Use `personal read <chat ID>` and its
`--before` cursor for older history today. Plain CLI commands
continue to work without an interactive terminal. The UI itself does not request
Contacts access; use the explicit authorization command below when needed.

## Browse Personal chats

```sh
go run ./cmd/psst personal chats
go run ./cmd/psst personal chats --limit 25
go run ./cmd/psst personal chats --help
```

On macOS, `personal chats` prints local chat IDs and quoted titles. The default is
10 chats; `--limit` accepts 1–100. Explicit chat names take priority. Otherwise,
Psst uses participant contact names when available and phone/email identifiers as
fallbacks. Unnamed groups show up to five participants and an ellipsis for more.
Empty databases get a clear message. Titles are limited to 200 Unicode characters
and terminal controls are escaped.

Ordering uses the newest **locally stored message row ID**, with chat ID as a
stable tie-breaker; chats without messages follow. This is not sent-time ordering:
late synchronization can affect the list. History uses normalized timestamps instead. Group chats appear once, and titles can contain personal identifiers;
share command output only when intended. Message bodies are not selected, and
nothing is uploaded or logged.

The command validates schema within the same read transaction as the listing,
uses the existing read-only guards and an eight-second operation deadline, and
closes its reader even on query/output failure. Unsupported platforms do not open
the database. Run `psst doctor` if access or schema checks fail.

## Read recent Personal messages

Use an ID from `personal chats`:

```sh
go run ./cmd/psst personal read 19
go run ./cmd/psst personal read 19 --limit 50
go run ./cmd/psst personal read 19 --limit 20 --before '<cursor from the previous page>'
go run ./cmd/psst personal read --help
```

The default page contains up to 20 recent messages; `--limit` accepts 1–100.
Rows are selected by normalized stored date and message row ID, then printed
oldest-first within that page. Unknown dates follow dated rows and are selected
only if the page has room after dated messages. Times use UTC. `Me` marks outgoing
rows; incoming senders use optional local contact names, falling back to phone/email
identifiers. Names remain display labels, not verified identities.
Missing chats produce a clear error; existing empty chats get an empty-history message.

This command explicitly prints private message text. It neither sends nor uploads
anything. Sender labels and text are quoted to escape terminal controls.
Text is capped at 4,000 Unicode code points per row with a visible truncation label.
Attachments, rich bodies and embedded content use placeholders; attachment files
are not opened and rich bodies are not decoded. Some modern messages will therefore
show a placeholder instead of their text.

When more history exists, output ends with `Next --before: <token>`. Copy that token
into `--before` on the next invocation for the same chat. No token is printed at
the end of local history. The token carries a version, chat ID, timestamp boundary
and message ID; it is encoded metadata, not encryption or an access credential.
Malformed tokens and tokens for another chat are rejected before database access.
You can change `--limit` between pages.

Pagination visits dated rows newest-to-oldest, then unknown-time rows by decreasing
message ID. Each page displays dated rows chronologically and unknowns afterward.
Newer inserts do not shift the older-page boundary; pages are separate snapshots,
so backfilled messages, timestamp edits, deletion or local resynchronization can
change later results. Restart without `--before` to refresh recent history.
See [history limits and date policy](docs/sqlite.md#message-history).

## Optional contact names

Contact-name lookup uses Apple's Contacts API through a small local Swift helper.
On macOS, build and install it once (requires Xcode Command Line Tools with Swift):

```sh
make contacts
go run ./cmd/psst personal contacts authorize
go run ./cmd/psst personal chats
```

`authorize` explicitly requests macOS Contacts permission. Full Disk Access for
Messages is a separate permission. Check status without prompting with:

```sh
go run ./cmd/psst personal contacts status
```

Normal listing never requests permission. Missing permission/helper, no match,
ambiguous matches, lookup failure, or its timeout use identifier fallbacks. Lookup
requests names only for bounded participants of unnamed displayed chats. No contacts
are modified, uploaded, logged, or saved in a name cache. Explicit chat names skip
lookup. Apple's full-name formatter may differ from Messages nicknames or shared
profile names.

`make contacts` installs only the signed helper executable under
`~/Library/Caches/psst/`, allowing `go run` to find it. It does not request access or
copy contacts there. A built CLI also finds a helper beside its executable.
`PSST_CONTACTS_HELPER` can specify an absolute helper path; set it to `off` to disable
native lookups. Plain Go builds still require no Swift installation. Rebuilding or
relocating the helper may require reauthorization depending on macOS identity
tracking. See [Contacts integration](docs/contacts.md) for development and tests.

## Doctor diagnostics

On macOS, `psst doctor` checks `~/Library/Messages/chat.db`, opens SQLite with
`mode=ro` and `query_only=ON`, and inspects known tables/columns in a read transaction.
It does not query conversation rows, print private paths, or expose raw driver errors.
The database pool stays private inside the adapter; its API exposes no write operation.

The command exits 0 with `compatible` when the basic schema baseline passes. It
exits 1 for `unsupported_schema`, missing/denied access, invalid or busy databases,
other failures, or an unsupported platform. Non-macOS platforms do not probe the
user's files. An incompatible schema lists only expected names from our allowlist.
A passing check does not validate timestamps, rich message formats, or send permissions.
Doctor queries have a three-second context deadline and a 250 ms busy timeout;
Ctrl-C cancels the command's context. Filesystem calls follow OS cancellation behavior.

The adapter uses the live database with SQLite's normal WAL handling; it never
copies just `chat.db`, changes journal mode, or enables `immutable` on a live file.
Read-only database access can still involve SQLite coordination through shared-memory
sidecars. See [SQLite adapter safety and tests](docs/sqlite.md) for the exact limits.

For access denial, the output suggests reviewing filesystem permissions and
Full Disk Access for the terminal/app running Psst. It cannot determine which
permission caused the denial and never changes permissions or opens Settings.
See [Apple's Privacy & Security guide](https://support.apple.com/guide/mac-help/mchl211c911f/mac)
for the scope of Full Disk Access. Tests use synthetic files and injected failures;
no real Messages data is needed.

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
  psst-contacts/           Optional native macOS Contacts helper source
internal/
  cli/                     Cobra commands, diagnostics, and safe table output
  domain/                  Shared conversation summaries and listing limits
  personal/                Safe Messages diagnostics and title resolution
    contacts/              Bounded local Contacts helper protocol
    sqlite/                Read-only schema/chat queries and synthetic fixtures
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
3. Personal Bubble Tea interface: chat list first, then conversation viewing and
   history scrolling; keep its state local and disposable.
4. A persisted Sessions slice: credentials, invites, transactional sends, ordered
   fanout, reconnect catch-up, moderation and cleanup; then unified navigation.
5. Explicit Personal sends, browser guests, and measured scaling work.

The [architecture and protocol](architecture-design-v0.1.md) define the complete
plan and acceptance criteria. The filename is historical; the document is v0.3.
[AGENTS.md](AGENTS.md) defines Go conventions, the learning workflow, privacy
constraints, and frequent verified commits and pushes.

History resolves incoming sender names locally, including participants in named
groups. Contacts lookup is optional and bounded; denied access or a timeout falls
back to the sender identifier. It never prompts for permission automatically.
