# PostgreSQL migrations

No schema is installed by the scaffold. Add the first migration with the persisted
Sessions slice and choose the migration runner in that change.

Use ordered, versioned SQL migrations. Keep an applied migration immutable; add a
new migration for changes. Preserve the architecture's constraints, composite
foreign keys, consistent locking policy, and indexes. Document how to migrate a
fresh database and how to upgrade an existing one. Exercise migrations against a
real PostgreSQL instance in integration tests. Never run these against Messages SQLite.
