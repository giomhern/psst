# Integration tests

No PostgreSQL or multi-client tests exist yet. Add them alongside the first
persisted Sessions behavior, with reproducible setup, isolated test data, and
cleanup. Ordinary unit tests must not require Docker, a network service, or a
user's Messages database.

Use real PostgreSQL for transactions and locking, synthetic SQLite fixtures for
Personal, and multi-client HTTP/WebSocket tests for delivery and reconnect.
Keep package-level integration tests next to their implementation where practical;
reserve this directory for cross-package scenarios and their shared harness.
