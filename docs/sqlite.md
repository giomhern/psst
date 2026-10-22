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
- The public API provides `Open`, `InspectSchema`, `ListConversations`, `ListMessages`, `ListMessagePage`, and `Close`.
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

`ListMessagePage(ctx, chatID, limit, before)` accepts a positive canonical decimal
local chat ID, a 1–100 limit, and an optional typed `domain.MessageCursor`.
`ListMessages(ctx, chatID, limit)` remains a first-page convenience returning
only messages. Invalid input is rejected before a transaction. The schema
check, chat existence check and history selection share a read transaction. Missing
and existing empty chats are distinct outcomes. An `IN` subquery selects
linked message row IDs without duplicating messages through repeated join rows;
orphan links and other chats do not produce rows. The page plus one lookahead row
is selected before body fields are fetched. No source indexes or schema are changed.
Result bounds
do not guarantee that only that many rows are examined; cancellation bounds work.

Selection uses descending normalized timestamp and message row ID; the selected
page is returned in ascending order within the dated and unknown-time segments.
Unknown times are lower priority than dated messages and appear after them when
selected. Positive message rowids are supported.

Traversal uses the total order `(unknown ascending, timestamp descending, rowid
descending)`. A known-time cursor admits smaller normalized timestamps, smaller
rowids at the same timestamp, and then unknown-time rows. An unknown-time cursor
admits only unknown-time rows with smaller rowids. This explicit NULL branch
prevents unknown dates from getting lost through SQL's three-valued comparisons.
There is no OFFSET. The cursor comes from the last retained row in traversal order,
not necessarily the first displayed row when a page mixes dated and unknown rows.
The lookahead row is excluded from output and determines whether `NextBefore` is
present; an exactly full final page has no continuation.

Cursors are typed domain values inside the adapter and encoded only by the CLI.
The version-1 token is raw URL-safe base64 JSON, at most 256 bytes, containing chat
ID, positive message ID, and UTC timestamp (null for unknown time). Go rejects
invalid versions, unknown fields, invalid dates/IDs, oversized payloads, trailing
JSON and chat mismatches before opening the database. The cursor is an untrusted
position, not proof of identity or authorization. It contains no message text.
A boundary row may be deleted without invalidating its key; no boundary-row lookup
is needed. Changing the page size is supported. Separate requests have separate
read snapshots: newer inserts do not shift an older keyset boundary, while older
backfills, timestamp changes, deletion and resynchronization can affect traversal.
No stable multi-request database snapshot is promised.

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
provide presence flags. When plain text is empty, at most 64 KiB of a BLOB body
is fetched for the bounded decoder described below. Larger bodies remain
placeholders, without transferring them into Go. U+FFFC object markers
set an embedded-content flag and are removed from display text. CLI placeholders
make absent text, unrendered attachments/rich content and truncation explicit.
Flags are best-effort metadata, not a complete classification of Apple's message
types: reactions, edits, deletions, delivery state and attachment rendering are
not decoded. The command renders the available database rows, not a reconstructed
Messages transcript. The Personal facade optionally resolves incoming sender
names; this SQLite adapter returns identifiers.

Tests cover mixed seconds/nanoseconds, precision and policy boundaries, chronological
selection, tie-breaks, unknown dates, empty/missing chats, duplicate/orphan joins,
WAL changes, unchanged database bytes, Unicode bounds, nullable/optional fields,
validation, cancellation, safe failures, output escaping and resource cleanup.

Pagination regression tests traverse mixed seconds/nanoseconds, identical timestamps,
unknown-time segments and page sizes 1–100 without duplicates or omissions. They
also cover exact page boundaries, duplicate/orphan joins, newer WAL inserts,
deleted boundary rows, end-of-history detection, cross-chat/invalid cursors,
round-trip subsecond precision, and the CLI continuation flow.


## Attributed-body text extraction

`attributed_body.go` supports the common macOS v4, little-endian, system-version
1000 typedstream prefix: an `NSAttributedString` or `NSMutableAttributedString`
root with its first backing `NSString` or `NSMutableString` (string version 1).
The parser checks class names, versions, expected references and the typed UTF-8
string field. It reads direct, two-byte or four-byte unsigned byte lengths,
checks the payload boundary and string end marker, and rejects invalid UTF-8.
It never scans for printable text or guesses a string from formatting metadata.
The layout reference is the upstream
[typedstream reader](https://github.com/dgelessus/python-typedstream/blob/master/src/typedstream/stream.py)
and its [NSString implementation](https://github.com/dgelessus/python-typedstream/blob/master/src/typedstream/types/foundation.py).
This implementation follows the format; it does not import that library.

Only the backing text field is decoded. The attribute graph after it is neither
interpreted nor fully validated; text styles, app objects and additional object
layouts are unsupported. Binary plists, big-endian streams, other versions,
truncated string fields, invalid UTF-8 and archives larger than 64 KiB keep the
rich-content placeholder. No archived objects are instantiated or executed.
Nonempty plain text always wins; decoded text uses the same 4,000-code-point
limit, truncation label, U+FFFC handling and terminal escaping as ordinary text.
Decoding failures are local to the message and expose no raw archive in errors.

The four `testdata/attributed-*.bin` files contain synthetic values produced by
macOS Foundation, including Unicode, mutable strings, a multi-byte length and
multiple attribute runs with decoy metadata. Ordinary Go tests use these files
on every platform and require no Foundation, Contacts or Messages access.
To regenerate them on macOS from the repository root:

```sh
fixture_tool=$(mktemp -d)
xcrun clang -Wno-deprecated-declarations -framework Foundation \
  internal/personal/sqlite/testdata/generate-attributed-body.m \
  -o "$fixture_tool/generate"
"$fixture_tool/generate"
rm "$fixture_tool/generate"
rmdir "$fixture_tool"
```

The generator only archives the hardcoded synthetic strings in its source. The
production decoder is pure Go. `FuzzDecodeAttributedBody` exercises arbitrary
bytes; integration tests cover precedence, malformed/oversized bodies, decoded
Unicode truncation, embedded markers and unchanged source database bytes.


## Outgoing send observation

The optional send observer captures `MAX(message.ROWID)` before automation, then
reads at most 101 new outgoing candidates in the selected chat (100 plus overflow
detection). Exact text and a bounded normalized timestamp window establish a
best-effort unique match; supported attributed bodies use the same bounded decoder
as history. Duplicate chat joins are deduplicated with `IN`. Ambiguous, oversized,
unknown-time, or unsupported observations remain unconfirmed.

After matching, the observer follows the message GUID in the original chat using
optional `is_sent`, `is_delivered`, `date_delivered`, and `error` columns. Missing
columns do not break chat/history reads or prohibit a confirmed send. Queries use
short read transactions and close them between observations so WAL updates remain
visible. Receipt reads never migrate or modify the database. See
[send receipt contracts](development.md#personal-send-receipts) for status
precedence, limits, timeouts, and correlation limitations.
