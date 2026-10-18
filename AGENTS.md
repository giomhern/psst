# Psst contributor and agent guide

## Purpose and working style

Build clear, idiomatic, reliable Go while helping the maintainer learn the language.
Treat this file as repository-wide guidance for every implementation and review.

- Work in small, runnable increments with one clear behavior per change. Explain
  the next slice before implementing it; avoid completing whole milestones in a
  single opaque change.
- When introducing a Go concept, briefly explain what it does, why it fits here,
  and where it appears in the code. In particular, explain functions versus
  methods, receivers, method sets, implicit interface satisfaction, zero values,
  errors, contexts, goroutines, channels, and transactions when first used.
- Explain meaningful tradeoffs and recommend a concrete choice. Do not turn every
  routine implementation choice into a permission request or quiz.
- Summarize changed behavior, verification performed, and limitations afterward.
  Never claim that an unrun check passed.
- Keep explanations in conversation and focused documentation. Code comments
  should explain invariants, ownership, or surprising decisions rather than
  narrating obvious syntax.

## Commit and push workflow

- Commit small, coherent, verified changes frequently, including documentation
  improvements. Push each completed checkpoint to the configured working branch
  rather than accumulating an entire milestone locally.
- Inspect status and diffs before committing. Stage only files belonging to the
  change; preserve unrelated user edits. Use concise commit messages describing
  the resulting behavior or reason for the change.
- Run applicable checks before committing and report any unavailable checks.
  Never commit secrets, private Messages content, generated credentials, or
  machine-specific configuration.
- Use the existing repository and remote conventions. Set upstream tracking for
  a new working branch when needed. Never force-push, rewrite shared history,
  merge, or push directly to a protected branch without explicit authorization.
- If Git initialization, a remote, credentials, or network access is missing,
  finish the local work and report the exact blocker. Do not invent a remote or
  claim a commit/push happened. Ask only for the missing information needed.

## Architecture and scope

`architecture-design-v0.1.md`, main sections 1–12, describes the proposed Psst
v0.3 design. Appendix A is historical context; its local-only scope and Ping
naming do not override the main design. The filename still says v0.1.

- Keep Personal (local macOS Messages) and Sessions (hosted rooms) separate in
  identity, storage, and transport. Never automatically upload Personal content.
- Begin with a modular monolith, one Sessions server process, PostgreSQL, and
  in-memory connection management. Add distributed infrastructure only for a
  demonstrated requirement.
- Keep `cmd/psst` and `cmd/psst-server` thin: configuration, dependency wiring,
  startup, shutdown, and exit status. Keep reusable behavior under `internal/`.
- Keep domain rules independent of UI, HTTP, WebSockets, SQL driver details, and
  macOS automation. Keep transport DTOs and persistence details at boundaries.
- Create packages as behavior requires them. The proposed directory tree is a
  guide, not a requirement to create empty packages or layers.
- Define small interfaces in the consuming package when substitution is useful.
  Prefer concrete implementations otherwise. Do not force Personal and Sessions
  capabilities into one large provider interface with unsupported methods.
- Keep dependencies explicit through constructors. Avoid service locators,
  mutable package globals, import cycles, and miscellaneous `utils` packages.
- Document material departures from the architecture and their reasons.

## Go conventions

- Choose a supported stable Go release when bootstrapping. Record the minimum
  supported version in `go.mod` and align local instructions and CI with it.
  Confirm the repository/module path before publishing it; do not invent one.
- Use `gofmt`. Prefer standard library facilities when they fit, and add a
  dependency only when it solves a concrete problem. Pin tool versions in CI.
- Follow Go naming conventions: short lowercase package names, MixedCaps,
  consistent initialisms such as ID, URL, and HTTP, and no package-name stutter.
- Keep identifiers scoped narrowly; use descriptive names across longer scopes.
  Export only what another package needs. Document exported declarations and
  nonobvious contracts, including concurrency safety where relevant.
- Prefer useful zero values and explicit constructors when validation or required
  dependencies make zero values unsuitable.
- Use methods for behavior belonging to a type; use functions for independent
  operations. Choose pointer receivers for mutation, synchronization-containing
  structs, or costly copies; value receivers suit small immutable values. Keep
  receiver choices consistent and explain method-set implications when relevant.
- Never copy a used mutex or another value whose API forbids copying. A pointer
  receiver alone does not make an object safe for concurrent use.
- Prefer simple control flow and early returns. Use generics, embedding, and
  reflection only when they simplify a real requirement. Avoid speculative
  frameworks, redundant wrappers, and interfaces added solely for mocking.
- Return errors for expected failures. Wrap with useful context using `%w` when
  callers should inspect the cause; use `errors.Is` and `errors.As`, not string
  matching. Introduce sentinel or typed errors only for meaningful distinctions.
- Handle errors once at an appropriate boundary. Avoid logging and returning the
  same error at every layer. Do not panic, call `log.Fatal`, or exit from library
  packages; let resources close before the command exits.
- Pass `context.Context` as the first parameter for cancellable operations. Do
  not store request contexts in structs or use context values as optional args.
  Propagate cancellation and deadlines; always release derived cancel functions.
- Close owned resources, stop tickers, check iteration errors, and handle close
  errors when they affect correctness. Make ownership explicit.

## Concurrency and lifecycle

- Every goroutine must have an owner, an exit condition, and a shutdown path that
  can be awaited. Cancellation signals termination; waiting proves it finished.
- Bound queues, goroutines, request sizes, query results, and connection counts.
  State the backpressure policy. Disconnect slow WebSocket consumers safely.
- Protect shared maps and mutable state with a clear mutex or goroutine ownership
  model. Use channels for coordination where helpful, not by default for all state.
- Document channel senders, receivers, and who closes each channel. Prevent sends
  racing with closure and make blocking operations respond to cancellation.
- Use one application read loop and one serialized write loop per WebSocket,
  following the chosen library's control-frame and close-handshake requirements.
  Set read limits, write deadlines, and heartbeat timeouts.
- Keep Bubble Tea model changes in its update loop. Perform blocking I/O through
  commands and return results as messages; do not mutate UI state in workers.
- Graceful shutdown must stop accepting work, cancel workers, close upgraded
  WebSocket connections explicitly, await goroutines, and close database pools.
  HTTP server shutdown alone does not manage upgraded connections.

## Sessions correctness

Main sections 6–8 of the architecture define the selected initial contracts.
Implement them incrementally; the documented design is not evidence that the
behavior already exists. Resolve any new ambiguity explicitly before relying on it.

- Follow the room-scoped host credential, secure storage, and loss policy.
  A caller-supplied `host_id` is not proof of authority. Authorize every join,
  history request, send, and administrative action; enforce revocation on active
  connections as well as new requests.
- Keep moderation scoped to participant credentials. A revoked credential blocks that credential;
  a nickname or newly issued guest token does not establish a persistent person.
  Document the limits of guest bans and invite reuse.
- Implement PostgreSQL persistence in the first Sessions vertical slice. A
  committed acknowledgement, retry deduplication, and reconnect history depend
  on it; do not defer their foundations until the reliability milestone.
- Allocate each session's sequence and validate membership, revocation, status,
  and expiry in a transaction with a consistent locking policy. End, revoke,
  redeem, and send operations need compatible locking to enforce their ordering.
- PostgreSQL `now()` is fixed at transaction start. For expiry decisions after
  waiting on locks, sample database wall-clock time after acquiring the relevant
  lock. Specify the acceptance point; do not promise expiry checks at commit.
- Use database constraints for uniqueness, references, and valid values. Keep
  invite redemption and usage limits atomic. Use parameterized SQL, bounded
  keyset pagination, and explicit transaction boundaries; never mix pool calls
  into an operation that must run entirely inside its transaction.
- Deduplicate by participant and client message ID. Define whether reusing an ID
  with different content returns a conflict, and test it. A retry of a committed
  send must not create another message. Acknowledge storage only after commit.
- Serialize or reconcile fanout by sequence: concurrent callers can commit in
  order and publish in reverse order. Do not hold database locks during network
  writes. Specify recovery for commit-success/publish-failure and missing acks;
  use the ordered database-log dispatcher selected in architecture section 6.3.
- Define a history/live handoff with a sequence watermark or buffering strategy
  so events arriving during catch-up are not lost. Clients deduplicate, detect
  sequence gaps, and request bounded history; define behavior for expired cursors
  and deleted history. Presence and typing need no durable delivery guarantee.
- Specify protocol version representation, validation, error codes, pagination,
  retention limits, and the precedence of lifecycle errors and retry responses.
- Define `delete_after`, purge timing, indexes, batch limits, and cascading cleanup
  for all related data. Enforce expiry in request paths, independently of cleanup.

## Privacy, security, and platform adapters

- Never log message bodies, personal handles, credentials, invite URLs, raw
  database rows, or script payloads. Keep secrets out of errors, metrics labels,
  fixtures, screenshots, and version control. Use bounded-cardinality telemetry.
- Use cryptographically secure randomness for tokens, hashed server-side token
  storage, HTTPS/WSS, scoped credentials, and rate limits. Validate inputs and
  sizes at transport boundaries and enforce business rules in the service layer.
- Design browser WebSocket authentication explicitly; browsers cannot generally
  set arbitrary authorization headers. Avoid long-lived credentials in URLs,
  validate allowed origins, and account for CSRF if using cookie authentication.
- Keep Messages SQLite access read-only with `mode=ro`, query-only enforcement,
  WAL-aware access, schema detection, bounded queries, and permission diagnostics.
  Do not migrate or modify Apple's data. Never enable SQLite immutable mode for
  a live changing database as a shortcut around locking or WAL behavior.
- Keep macOS-specific code behind appropriate files/build constraints and provide
  clear unsupported-platform behavior. The Sessions server must build on Linux.
- Invoke fixed executables with `exec.CommandContext` and safe argument passing.
  Never interpolate user text into shell or executable AppleScript source.
- Personal sends require an explicit user action and confirmation. Report send
  requested, observed locally, and delivery unknown honestly. Do not blindly
  retry ambiguous automation outcomes.
- Use synthetic fixtures only. Access real Personal content only as part of an
  explicitly requested local validation; never expose it in diagnostics.

## Verification and efficiency

Once the relevant Go packages exist, run the applicable checks and report results:

- Format changed Go files with `gofmt`; keep module metadata tidy.
- Run `go test ./...`, `go vet ./...`, and build the affected binaries.
- Run `go test -race ./...` for concurrency changes and in supported CI. State any
  platform or toolchain limitation rather than silently skipping the check.
- Add pinned Staticcheck and `govulncheck` checks to CI when the module and tooling
  are established; investigate findings rather than adding blanket suppressions.
- Keep unit tests next to their packages in `_test.go` files. Use descriptive
  subtests and table-driven cases when they clarify behavior. Reserve a separate
  integration suite for tests needing PostgreSQL or external processes.
- Test observable contracts and failure paths rather than private call sequences.
  Add meaningful regression coverage for fixes. Do not add tests for prose-only
  edits or trivial implementation details.
- Use real PostgreSQL for locking, transaction, and constraint tests. SQLite is
  not a substitute for PostgreSQL semantics. Use synthetic SQLite databases for
  the Personal adapter and `httptest` for HTTP behavior.
- Cover concurrent sends, retries, end/send races, expiry while waiting on locks,
  invite exhaustion, revocation, slow readers, reconnect handoff, and restart
  recovery as those behaviors are introduced.
- Prefer explicit synchronization and injected clocks at application boundaries
  over sleep-based tests. Database expiry tests must exercise database time.
- Keep integration tests reproducible with documented setup, isolated data, and
  cleanup. Do not make network calls or require real Messages access in unit tests.
- Optimize measured bottlenecks. Start with bounded queries, appropriate indexes,
  connection-pool limits, and simple data structures. Use benchmarks with
  allocation reporting and profiling before adding caches, pools, or complexity.

## Reference standards

Use these sources with documentation for the project's selected Go version:

- Go Code Review Comments: https://go.dev/wiki/CodeReviewComments
- Effective Go (core idioms; not a complete modern Go guide): https://go.dev/doc/effective_go
- Go module reference: https://go.dev/ref/mod
- Go transactions: https://go.dev/doc/database/execute-transactions
- Go race detector: https://go.dev/doc/articles/race_detector
- Go diagnostics and profiling: https://go.dev/doc/diagnostics
- Go vulnerability checking: https://go.dev/doc/security/vuln/
- PostgreSQL time semantics: https://www.postgresql.org/docs/current/functions-datetime.html
