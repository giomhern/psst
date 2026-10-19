# Personal SQLite adapter

The adapter checks a minimum schema baseline, lists bounded chat summaries, and
reads a bounded recent page of message history when explicitly requested.
Listing reads chat titles and message row IDs for ordering, but never selects
message bodies. Contact matching is performed separately by the optional native
helper after the read transaction is released. SQLite does not resolve contacts. It does not validate Apple's undocumented
formats across every macOS release. The doctor still inspects metadata only.

## Safety boundary

- Only existing regular files at absolute filesystem paths can be opened.
- The path is URI-encoded and combined with fixed `mode=ro` options. Filename
  characters cannot inject SQLite settings. Missing databases are not created.
- Every pool connection enables `query_only`, a 250 ms busy timeout, and
  `trusted_schema=OFF`. The pool allows one connection; it is not exported.
- The public API provides `Open`, `InspectSchema`, `ListConversations`, `ListMessages`, and `Close`.
  There is no public generic query/exec handle, migration, repair, checkpoint, or
  filesystem-delete API.
- Inspection uses SQLite metadata in a read transaction, with context cancellation
  and bounded column enumeration. It accepts extra columns, but fails closed when
  a table exceeds 512 columns or required fields are absent. Views do not stand
  in for required tables.
- The doctor prints only static guidance and missing expected identifiers from a
  fixed allowlist. Unknown schema names, raw driver errors, paths, and message
  contents never become diagnostic output.

`mode=ro` protects database writes through that connection. It does not change
filesystem permissions or make the file undeletable by other code or applications.
`query_only` is an additional SQL guard, not an OS sandbox. Normal WAL readers may
coordinate through the `-shm` sidecar, and SQLite can create required sidecars if
its directory permissions allow. We do not promise that every sidecar byte is
untouched. Never delete or manually alter Apple's database or sidecars.

The connection opens the live database with its normal locking and WAL behavior.
Do not use `immutable`, copy only the main database, alter journal mode, or run
cleanup/checkpoints against Messages. An inaccessible WAL database fails the
probe with safe guidance rather than attempting a repair.

## Baseline

| Table | Required columns | Additional requirement |
| --- | --- | --- |
| `chat` | `guid` | Rowid table |
| `message` | `guid`, `text`, `date`, `is_from_me`, `handle_id` | Rowid table |
| `handle` | `id` | Rowid table |
| `chat_message_join` | `chat_id`, `message_id` | Table |
| `chat_handle_join` | `chat_id`, `handle_id` | Table |

Compatibility means these prerequisites exist. Future query slices must validate
actual date encodings, nullable text, rich/attachment-only messages, joins, and
pagination against synthetic fixtures before enabling those behaviors.

## Conversation queries

`ListConversations(ctx, limit)` validates a 1–100 limit, starts a read transaction,
checks the baseline, detects the optional `chat.display_name` column, then selects
at most `limit` summaries. Display names fall back to GUIDs and an untitled label;
SQL bounds titles to 200 characters. The command escapes terminal control characters
before rendering any title. No message-body column is selected.

A correlated aggregate finds each chat's highest existing message row ID without
duplicating chats through group membership or duplicate join rows. Chats are ordered
by that local row ID, then chat row ID; empty chats follow. Do not equate this order
with sent timestamps or implement timestamp guessing here. The result bound does
not imply only that many database rows are examined; the deadline bounds query
execution and future tuning should use measured query plans without modifying
Apple's indexes.

The `personal.Reader` application boundary resolves the local database on macOS
and converts backend errors into safe messages. The CLI owns a small reader interface
and is responsible for closing the opened reader. No raw driver error crosses into
terminal error output. Schema inspection and data selection share one snapshot.
Unnamed chats include at most five distinct participant identifiers of at most
320 bytes each, plus an overflow marker. Named chats skip this lookup. Contact
matching happens after commit, so no transaction is held across helper I/O.

## Verification

Tests create temporary synthetic SQLite databases from `testdata/baseline.sql`.
Only test setup opens a writable connection. Tests attempt INSERT, UPDATE, DELETE,
CREATE, DROP, and persistent PRAGMA changes through the reader and verify rejection.
They also disable `query_only` on a synthetic reader to verify `mode=ro` independently.

Conversation tests cover group-chat deduplication, orphan links, deterministic
ordering and limits, empty chats, optional/missing columns, nullable titles, Unicode
truncation, WAL updates, and unchanged source bytes. Cobra tests verify argument
validation before opening the database, title escaping, cancellation, and cleanup
on output or query failures.

Additional adapter tests cover new/replacement connections, safely encoded filenames,
missing and malformed files, unsupported schemas, cancellation, lock contention,
WAL schema visibility, and later commits on a live connection. Synthetic main
and WAL bytes are compared around inspection; shared-memory locking bytes are
not treated as immutable. CLI and report tests verify safe error output.

Tests never open the user's actual Messages database. Local schema validation
against a real database is a separate user-run `psst doctor` step.

References: [driver documentation](https://pkg.go.dev/modernc.org/sqlite),
[SQLite URI mode](https://www.sqlite.org/uri.html),
[query_only](https://www.sqlite.org/pragma.html#pragma_query_only), and
[read-only WAL databases](https://www.sqlite.org/wal.html#read_only_databases).

## Message history

`ListMessages(ctx, chatID, limit)` accepts a positive canonical decimal local chat
ID and a 1–100 limit. Invalid input is rejected before a transaction. The schema
check, chat existence check and history selection share a read transaction. Missing
and existing empty chats are distinct outcomes. An `IN` subquery selects
linked message row IDs without duplicating messages through repeated join rows;
orphan links and other chats do not produce rows. The page is selected before
body fields are fetched. No source indexes or schema are changed. Result bounds
do not guarantee that only that many rows are examined; cancellation bounds work.

Selection uses descending normalized timestamp and message row ID; the selected
page is returned in ascending order. Unknown times are lower priority than dated
messages and appear after them when selected. There is no cursor or OFFSET yet.

The current explicit date policy accepts positive integer Apple-epoch seconds
through `2200-01-01T00:00:00Z` (1–6,279,811,200), or integer Apple-epoch nanoseconds
from 1,000,000,000,000 through 6,279,811,200,000,000,000. Both are normalized to
integer nanoseconds before sorting, preserving subsecond precision without int64
overflow. UTC conversion adds the Apple epoch offset of 978,307,200 Unix seconds.
Zero, NULL, negative, fractional and out-of-range values become unknown time.
This is magnitude-based inference for those two representations, not proof of an
arbitrary timestamp's units. Other encodings such as microseconds are unsupported
and can resemble accepted magnitudes; no universal macOS-format claim is made.
Apple documents the [2001 reference epoch](https://developer.apple.com/documentation/foundation/date/timeintervalsincereferencedate-swift.property),
not this private Messages schema. Synthetic tests establish our conversion contract;
this checkpoint does not validate date units against a real user's message rows.

Plain text is selected only for SQLite TEXT values, capped in SQL at 4,001 code
points so Go can retain 4,000 and indicate truncation. Sender identifiers are capped
at 320 code points. Optional `cache_has_attachments` and `attributedBody` columns
provide presence flags; rich body bytes are not returned. U+FFFC object markers
set an embedded-content flag and are removed from display text. CLI placeholders
make absent text, unrendered attachments/rich content and truncation explicit.
Flags are best-effort metadata, not a complete classification of Apple's message
types: reactions, edits, deletions, delivery state and attachment rendering are
not decoded. The command renders the available database rows, not a reconstructed
Messages transcript. Incoming sender names remain identifiers in this slice.

Tests cover mixed seconds/nanoseconds, precision and policy boundaries, chronological
selection, tie-breaks, unknown dates, empty/missing chats, duplicate/orphan joins,
WAL changes, unchanged database bytes, Unicode bounds, nullable/optional fields,
validation, cancellation, safe failures, output escaping and resource cleanup.
