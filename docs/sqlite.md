# Personal SQLite adapter

The adapter checks a minimum schema baseline for planned conversation/message
queries. It does not read message rows, list contacts, or validate Apple's
undocumented formats across every macOS release.

## Safety boundary

- Only existing regular files at absolute filesystem paths can be opened.
- The path is URI-encoded and combined with fixed `mode=ro` options. Filename
  characters cannot inject SQLite settings. Missing databases are not created.
- Every pool connection enables `query_only`, a 250 ms busy timeout, and
  `trusted_schema=OFF`. The pool allows one connection; it is not exported.
- The public API provides `Open`, `InspectSchema`, and `Close`. There is no public
  generic query/exec handle, migration, repair, checkpoint, or filesystem-delete API.
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

## Verification

Tests create temporary synthetic SQLite databases from `testdata/baseline.sql`.
Only test setup opens a writable connection. Tests attempt INSERT, UPDATE, DELETE,
CREATE, DROP, and persistent PRAGMA changes through the reader and verify rejection.
They also disable `query_only` on a synthetic reader to verify `mode=ro` independently.

Additional tests cover new/replacement connections, safely encoded filenames,
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
