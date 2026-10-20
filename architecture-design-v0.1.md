# Psst — Architecture & Technical Design

**Version:** 0.3 · **Status:** Proposed · **Updated:** 17 October 2026

**Primary language:** Go · **Clients:** macOS terminal, optional web guests · **Design:** Hybrid messaging platform

## 1. Executive summary

**Psst** (`psst`) is a terminal-native hybrid messaging platform with two explicitly separate providers:

1. **Psst Personal:** a local macOS integration for viewing existing Messages/iMessage conversations and attempting basic sends via macOS automation. Apple does not offer a public general-purpose iMessage API. This integration is read-only with respect to `chat.db` and is best-effort.
2. **Psst Sessions:** an independent, hosted messaging network for short-lived, invite-only rooms. Hosts create rooms with expiration times, share invite codes or links, chat in real time, moderate participation, and explicitly end sessions. Guests do **not** need iMessage accounts; a browser guest client is a later milestone.

Both experiences appear in one Bubble Tea terminal interface, but they do not share identity, message storage, or transport by default. **Personal conversations are never copied into hosted sessions automatically.**

## 2. Product goals and boundaries

### Goals

- Browse locally synchronized iMessage conversations in a polished TUI.
- Create and join ephemeral Psst rooms from the same CLI; message over authenticated WebSockets.
- Allow invite-only participation with guest nicknames, host moderation, configurable TTL, and explicit room termination.
- Provide bounded message history and reconnect catch-up during a room's lifetime.
- Keep the architecture modular and testable with synthetic data, especially for private Messages content.
- Teach Go backend engineering: HTTP, WebSockets, goroutines, backpressure, SQL transactions, auth, and lifecycle state machines.

### Non-goals for initial release

- Reimplementing Apple's proprietary iMessage protocol or bridging iMessage contacts into rooms.
- End-to-end encryption claims; hosted sessions use TLS in transit, and server-side plaintext is possible in the initial release.
- Guaranteed delivery to offline participants after room retention ends.
- A multi-region microservice deployment, federation, video, or voice.
- Disabling macOS System Integrity Protection to access private APIs.

## 3. High-level architecture

```text
                         psst (Go CLI + Bubble Tea TUI)
                                      |
                          Small capability interfaces
                           /                       \
            Personal provider                  Sessions provider
            (local macOS)                     (hosted network)
          /                 \                    |
  chat.db SQLite         AppleScript        HTTPS / WebSocket
  read-only reader       send adapter             |
                                          Go API + realtime hub
                                                  |
                                         Session application layer
                                        /                     \
                                 PostgreSQL             in-memory hubs
                               sessions, invites,      connections,
                               messages, sequence      outbound queues

                 Future: browser guest UI and Redis/NATS fanout
```

**Deployment:** The personal provider stays on the user's Mac. The Sessions API is deployed as a separate Go server. Start as one server process and PostgreSQL; only add Redis/NATS after measuring a need for cross-instance fanout.

## 4. User experience and CLI contract

```bash
psst                        # Launch unified inbox / session TUI
psst doctor                 # Check local Messages access, no content printed
psst personal chats         # List local Messages conversations
psst personal read <chat>   # Read synchronized history
psst personal send <chat> "hello" # Explicit user-initiated send

psst session create "launch-night" --ttl 1h
psst session invite <session-id>
psst join <invite-code>
psst session end <session-id>
psst session list
```

Session creation stores the host credential in macOS Keychain and returns the room ID and a human-readable encoding of a high-entropy invite token. It does not print the host credential. `psst session list` lists locally known room IDs; there is no unauthenticated server-wide room directory. An optional web invite URL is added when the web client is available. CLI output must not log secret invite tokens outside the explicit create/invite response. Offer interactive invite entry so users can avoid putting secrets in shell history.

## 5. Go package layout

```text
psst/
├── cmd/
│   ├── psst/                 # CLI and TUI binary
│   └── psst-server/          # Hosted Sessions API binary
├── internal/
│   ├── domain/               # Conversation, message, session types
│   ├── tui/                  # Bubble Tea screens and update loop
│   ├── personal/
│   │   ├── sqlite/           # Read-only macOS Messages adapter
│   │   ├── applescript/      # Explicit send adapter
│   │   └── watcher/          # Poll for local changes
│   ├── sessions/
│   │   ├── httpapi/          # REST endpoints, auth middleware
│   │   ├── realtime/         # WebSocket clients and room hub
│   │   ├── service/          # Lifecycle and membership rules
│   │   ├── store/            # PostgreSQL repositories
│   │   └── cleanup/          # Expiration and retention sweeper
│   └── config/
├── migrations/
├── docs/
└── tests/
```

The TUI depends on domain-level provider interfaces, not SQLite queries or WebSocket internals. The session server owns authoritative room state; client UI state is disposable.

## 6. Sessions domain and database schema

### 6.1 Identity and authority

The initial release uses **room-scoped bearer credentials**, with no global user accounts. A participant ID identifies one membership, not a verified person. Display names never confer authority.

- `POST /v1/sessions` is a rate-limited, quota-controlled bootstrap endpoint. It atomically creates the room, its host participant, and an initial single-use invite, and returns that participant's secret credential once. The server chooses all IDs and the host role; the request cannot assert a `host_id` or role. Production exposure requires creation limits and a global capacity limit, not just limits per room.
- Invites grant permission to create a member participant. Redemption returns that participant's credential once. An invite is never a host credential and never authorizes history, messages, or administration by itself.
- Generate credentials and invites using 32 random bytes from Go's `crypto/rand`, with an unambiguous URL-safe encoding. Store SHA-256 token hashes with unique indexes; keep plaintext out of database rows and logs. Tokens are independent secrets, not derivatives of public IDs. Revocation invalidates a credential immediately for future authorization decisions. Room closure blocks new work; retained credentials can still resolve stored-send retries and repeat host end requests until purge, as specified below.
- The CLI stores participant credentials in macOS Keychain, indexed by server origin, room ID, and participant ID. Persist only IDs and nonsecret metadata in regular configuration. If secure storage fails, report the failure and keep the credential only in memory; do not silently write a plaintext fallback. An authenticated reconnect reuses the same participant credential.
- Anyone holding the host credential controls that room. Host transfer, account login, and credential recovery are outside the initial release. Losing the credential means losing administrative access; TTL still ends the room. Never recover authority from a nickname, local room listing, or invite. A lost creation/redemption response may leave an inaccessible room/membership; do not blindly retry these bootstrap requests. Bound pending/inaccessible room costs through TTL and creation quotas.
- Kick revokes a participant credential and disconnects all its connections. Hosts cannot kick themselves through the member endpoint. Host credentials authorize their own room only. There is **no persistent person-level ban** in the initial release: a kicked person can redeem another usable invite. The host can revoke all outstanding invites and issue single-use replacements to reduce re-entry. Stronger bans require a later identity design; do not use nicknames or IP addresses as proof of identity.

### 6.2 Persistent model and transaction rules

| Table | Essential fields | Constraints |
|---|---|---|
| `sessions` | `id`, `host_participant_id`, `name`, `status`, `expires_at`, `ended_at`, `delete_after`, `next_seq` | status active/ended; mandatory expiry; `next_seq` starts at 1 |
| `participants` | `id`, `session_id`, `display_name`, `role`, `credential_hash`, `joined_at`, `revoked_at` | unique credential hash; role host/member; one host per session |
| `session_invites` | `id`, `session_id`, `token_hash`, `expires_at`, `max_uses`, `uses`, `revoked_at` | unique token hash; `0 <= uses <= max_uses`; positive max uses |
| `session_messages` | `id`, `session_id`, `participant_id`, `client_message_id`, `seq`, `body`, `created_at` | unique `(participant_id, client_message_id)` and `(session_id, seq)`; positive seq |

Use `timestamptz` for database instants, and represent protocol timestamps in UTC. Add composite foreign keys so a message sender and the host participant belong to the referenced session. Enforce one host with a partial unique index; use a deferred foreign key for the room/host insertion cycle. Index expiry/purge work and each room's ordered message history. Purge dependents in one bounded room transaction, accounting for the host reference cycle.

Use PostgreSQL READ COMMITTED transactions with an explicit locking order: session row first, then affected participant/invite rows in stable ID order. All sends, invite creation/redemption/revocation, kicks, and ending operations follow that policy. Locking only in the send path would not protect it from concurrent moderation.

After acquiring required locks, sample `clock_timestamp()` once for the operation's decision time. PostgreSQL `now()` represents transaction start and can be stale after waiting for a lock. Reject a new operation if the room is ended, expired at decision time, or the credential is revoked. This decision point determines acceptance; an accepted transaction may commit after the TTL boundary. Never hold database locks across socket writes.

**Send transaction:** authenticate and validate membership first, then look up `(participant_id, client_message_id)` before applying lifecycle checks. For a still-unrevoked participant, an identical previously committed request returns its original stored acknowledgement while the row is retained, even after ending/expiry; it creates no new message. Reusing the ID with a different body returns `idempotency_conflict`. A revoked credential receives no acknowledgement or message data. For a new ID, enforce active state, TTL, size/rate/room limits, allocate `seq = next_seq`, increment `next_seq`, and insert the message in the same transaction. A rollback consumes no sequence. The room lock serializes committed sequence allocation. A stored acknowledgement means committed, not received by every recipient. Uncertain commit outcomes are resolved by retrying the same ID and body, never by inventing a new ID.

**Invite transaction:** lock the room and invite, verify active state, invite expiry/revocation, capacity, and remaining uses, then increment uses and insert the member atomically. Invite expiry cannot exceed room expiry. Default invites are single-use; multiple uses require an explicit host option. Redeem is not automatically retried after an uncertain response.

**Lifecycle:** `active → ended` by host action or expiry. End is idempotent for the authenticated host, including when already expired. Logical expiry takes effect at `expires_at` even before a sweeper records `ended`; use `ended_at = expires_at` for expiry, or the earlier explicit end decision time. Every new operation enforces this rule. End/revoke commits invalidate future authorized work; already authorized or transmitted data cannot be recalled. Send `session.ended` or `participant.revoked` on a best-effort basis and close affected sockets. Clients must tolerate closure without a final event. Authenticated history is available only while active; ended/expired rooms return `session_closed`, with the narrow stored-ack retry exception above.

**Retention and bounds:** initially allow TTLs from 1 minute to 24 hours, at most 32 total participant memberships over the room lifetime (including the host; kicked memberships still count), 10,000 messages per room, and 4 KiB of UTF-8 message body per message. These are explicit configurable limits with hard deployment ceilings. Hitting the message cap rejects new messages with `room_full`; do not delete an active room's older messages. All committed sequences remain available during the room lifetime. Set `delete_after` to the effective end time plus a one-hour purge grace period, including an initial value of `expires_at + 1 hour`; shorten it on early end. Every minute, the sweeper processes bounded batches of expired rooms and purge candidates. Purge room content, credentials, invites, and metadata together. Grace time is for cleanup and retry resolution, not continued history access. Service outages may delay physical purge; participant copies and backups have separate deletion limits.

### 6.3 Ordered fanout and recovery

The committed `session_messages` table is the initial durable delivery log. Do not broadcast directly from concurrent send handlers: commit order does not imply handler scheduling or broadcast order.

One dispatcher per locally active room reads committed rows in ascending `seq` with bounded queries and serializes `message.created` enqueueing. Its cursor advances only after each row has been offered to eligible connections; an overflowing connection is closed and must catch up. New commits signal the dispatcher as a latency optimization; a database poll at most one second apart is the correctness fallback. No durable outbox is needed for this message-only stream because the persisted rows are themselves replayable. Presence and typing are not part of this ordered log.

The dispatcher also checks room state/expiry and connected participants' revocation on each poll; send/history paths always perform their own database authorization. End/kick handlers signal the hub after commit, and polling repairs missed signals. Authorization is sampled before enqueueing a bounded batch: data authorized before a concurrent revocation can still be in flight. On observed revocation/end, discard pending content and close the connection. Database failure stops content fanout until authorization can be re-established; prolonged failure closes connections.

A crash after commit but before acknowledgement or broadcast does not lose the stored message. The sender retries its ID; recipients reconnect and replay the database log. Server restart drops sockets, and new dispatchers recover from retained rows through the reconnect protocol. Delivery may be repeated; clients deduplicate and maintain a contiguous sequence cursor. Neither acknowledgements nor terminal lifecycle events are ordered message-log entries; they never advance that cursor.

## 7. API and realtime protocol

### 7.1 HTTP contract

| Method | Route | Purpose |
|---|---|---|
| `POST` | `/v1/sessions` | Bootstrap room, host credential, and initial single-use invite atomically; rate-limited |
| `POST` | `/v1/sessions/{id}/invites` | Create invite (host only) |
| `POST` | `/v1/sessions/{id}/invites/revoke` | Revoke all outstanding invites (host only, idempotent) |
| `POST` | `/v1/invites/redeem` | Redeem invite and obtain member credential |
| `POST` | `/v1/sessions/{id}/participants/{participant-id}/revoke` | Kick member (host only, idempotent) |
| `POST` | `/v1/sessions/{id}/end` | End room (host only, idempotent) |
| `POST` | `/v1/sessions/{id}/messages` | Send or resolve an idempotent retry using the same service as WebSocket sends |
| `GET` | `/v1/sessions/{id}/messages?after_seq=N&through_seq=H&limit=100` | Authorized, ascending bounded catch-up |
| `GET` | `/v1/sessions/{id}/ws?after_seq=N` | Authenticate and open reconnect stream |

The CLI supplies `Authorization: Bearer <participant-credential>` on authenticated HTTP requests and upgrades. Never put bearer credentials or invites in WebSocket query parameters. Validate membership in the URL's room rather than trusting IDs in a payload. Disable caching of credential and history responses. Validate all query parameters and reject unknown protocol versions, invalid sizes, negative cursors, and malformed JSON without logging the payload.

Errors use stable codes and safe messages. HTTP mapping: `401 unauthorized` for missing/invalid/revoked credentials, `403 forbidden` for a valid member attempting host actions, `404 not_found` for unavailable resources (including purged rooms), `409` for `session_closed`, `room_full`, `idempotency_conflict`, or `cursor_ahead`, `410 invite_unavailable` for expired/revoked/exhausted invites without distinguishing the reason, `413 payload_too_large`, `429 rate_limited` with retry guidance, and `503 temporarily_unavailable` for database/authorization outages. Invalid inputs use `400 invalid_request`. Do not reveal room state before authenticating a room-scoped request. A revoked credential always loses to idempotency lookup; an authorized stored retry wins over lifecycle closure as described in section 6.2. Use HTTP POST messages to resolve an uncertain send after the room closes, because a closed room refuses a new WebSocket upgrade. HTTP and WebSocket sends share the same validation, transaction, and deduplication code.

History requires `0 <= after_seq <= through_seq <=` the room's highest committed sequence. It returns `messages`, `next_after_seq` (last returned sequence or the input cursor), `through_seq`, and `has_more`; cap limit at 100. If no watermark is supplied, capture the current high sequence once for that response. Reuse it across pages. Cursors contain message sequence numbers only; they grant no access. A cursor beyond the current high sequence returns `cursor_ahead`. There is no partial active-history eviction in this version: ended rooms are closed and purged rooms are unavailable, rather than silently skipping missing history.

### 7.2 WebSocket envelope

Every envelope contains `v: 1`, `type`, and `payload`. Client requests require a bounded `request_id`, echoed on responses; unsolicited events omit it. JSON examples:

```json
{"v":1,"type":"message.send","request_id":"r-17","payload":{"client_message_id":"uuid-from-client","body":"hello room"}}
```

```json
{"v":1,"type":"message.ack","request_id":"r-17","payload":{"message_id":"uuid-from-server","seq":12,"status":"stored"}}
```

```json
{"v":1,"type":"message.created","payload":{"message_id":"uuid-from-server","seq":12,"sender_id":"participant-id","body":"hello room"}}
```

Acks and `message.created` may arrive in either order. Only contiguous `message.created` or history rows advance the receive cursor. Keep pending sends keyed by `client_message_id` until their outcomes are resolved.

Other events: `connection.ready`, `participant.joined`, `participant.left`, `participant.revoked`, `typing.started`, `typing.stopped`, `session.ended`, and `error`. Presence describes current connections and is not a membership ledger; losing the last connection does not revoke a participant. Typing and presence are ephemeral.

### 7.3 Reconnect handoff

A watermark is the highest committed sequence captured at a particular point. Use this explicit handoff:

1. Client reconnects with its last **contiguous applied** sequence `N` (zero for no history) and authenticates with its existing participant credential.
2. Hub registers the connection in bounded buffering mode **before** reading the database high sequence. The dispatcher serializes connection registration with fanout. The server revalidates active membership/room state, captures watermark `H = next_seq - 1`, and queues `connection.ready` containing `through_seq: H` ahead of the buffered content. The writer then drains that buffer and continues live streaming; it does not wait for a client acknowledgement. If validation fails, unregister and close; if `N > H`, return `cursor_ahead`.
3. Client buffers live `message.created` events and fetches ascending history for `(N, H]` using the HTTP endpoint, always passing the same `through_seq=H`. Membership/expiry is checked on every page. Duplicates at or below `H` are harmless.
4. After applying every sequence through `H`, client merges buffered events above `H` in order, deduplicating by sequence/message ID. It then consumes live events. A gap pauses cursor advancement and triggers bounded history repair. An empty interval (`N = H`) requires no history fetch.
5. Server/client buffers are bounded (initially 256 events and 2 MiB each, whichever is reached first). Overflow closes the socket and restarts handoff from the last contiguous applied sequence. Use bounded reconnect backoff with jitter; never grow buffers indefinitely. Dispatcher lag may replay sequences already covered by HTTP; deduplication handles them.

Registration before the watermark closes the history/live gap; ordered database tailing covers missed commit notifications. If end, revocation, or purge interrupts catch-up, stop with the corresponding error and discard inaccessible pending data. After a process restart, a client without persisted history starts at zero; never persist a cursor without the local state it describes. The initial CLI keeps session history in memory and does not silently create a second on-disk transcript.

### 7.4 Future browser authentication boundary

The first release authenticates CLI upgrades with the Authorization header. Before browser guests ship, add an HTTPS credential-exchange endpoint issuing an HttpOnly, Secure, appropriately SameSite cookie, plus CSRF defenses for cookie-authenticated mutations and strict allowed-Origin validation for upgrades. Define cookie scope and multiple-room behavior in that milestone. Do not weaken CLI authentication or introduce long-lived URL credentials to work around browser WebSocket header restrictions. Invite links should place the secret in a URL fragment consumed by the guest page and redeemed via POST; redact link material from analytics and referrers.

## 8. Security and privacy

- **Local Messages:** open `chat.db` in read-only SQLite mode; account for SQLite WAL; never modify Apple data; handle Full Disk Access and automation permissions transparently. Do not upload local Messages history to Psst's server.
- **Invites:** generate high-entropy random tokens, store hashes, enforce expiry and usage counts transactionally, rate-limit redemption, and avoid leaking tokens in logs.
- **Sessions:** use HTTPS/WSS, scoped participant credentials, host-only administrative actions, authorization on every message/history request, and revocation checks.
- **Abuse prevention:** message size limits, per-participant rate limits, max room size, connection quotas, credential-scoped kicks, invite revocation, and origin checks for browser WebSockets.
- **Privacy:** display clear retention semantics; minimize server logs and redact message bodies and tokens; support session purge.

## 9. Reliability, concurrency, and testing

- One read loop and one write loop per WebSocket; serialize writes and use `context.Context` for cancellation.
- Store messages before fanout. Acknowledgement is sent only after commit; retry is safe through idempotency keys.
- Make host-end and expiration races deterministic with transactional state checks.
- Test with synthetic chat.db fixtures rather than real personal conversations.
- Use Go unit tests, race detector (`go test -race ./...`), PostgreSQL integration tests, and multi-client WebSocket tests.
- Exercise disconnect/reconnect during watermark capture and every history page; assert no sequence gaps when commits race with registration.
- Test duplicate IDs with equal/different bodies, ack loss, crash after commit before fanout, reversed handler completion, and missed dispatcher notifications.
- Test concurrent invite exhaustion, end/send and kick/send races, expiry while waiting for locks, revoked history access, slow readers, and server restart. Use real PostgreSQL for locking and clock behavior.
- Instrument active sessions, connected clients, send-to-commit latency, reconnects, dropped connections, and errors; avoid message content in telemetry.

## 10. Implementation plan and acceptance criteria

**Implementation ordering update:** At the maintainer's request, the Personal
Bubble Tea interface is being built after read-only history/pagination and before
the persisted Sessions slice. The UI now has a bounded chat list and a read-only
conversation view opening with the latest 20 messages. Explicit older-page
loading retains up to 200 messages with stable viewport anchoring; refresh
returns to the latest page. The UI now also supports explicitly confirmed text
sends to existing chats through Messages automation, with conservative outcome
reporting; the proposed standalone send command remains unimplemented. This changes delivery order,
not Personal/Sessions separation or the Sessions correctness contracts below.
The standalone Personal UI initially launches with `psst personal ui`; the default
`psst` command continues to show help until broader navigation is ready.

| Phase | Deliverable | Acceptance test |
|---|---|---|
| 0 — Foundation | Go module, config, Docker Compose, CI, `psst doctor` | Local checks reveal no private message content |
| 1 — Personal read | SQLite adapter, chat list, history | Browse existing synced conversations read-only |
| 2 — Session vertical slice | PostgreSQL migrations, room-scoped host/member credentials, create/redeem/join, transactional sends, deduplication, ordered database dispatcher, watermark reconnect, basic TTL/end checks and resource limits | Two terminals chat; reconnect catches up; retry creates one row; restart preserves history; expired/ended rooms reject new work |
| 3 — Lifecycle and moderation | Kick, outstanding-invite revocation, expiry notifications, bounded purge sweeper | Revoked members cannot send/read; exhausted invites reject joins; ended data purges after grace |
| 4 — Reliability hardening | Fault injection, backpressure, shutdown, integration/race coverage and metrics | Commit/fanout crashes, reconnect races and slow readers recover within documented guarantees |
| 5 — Unified TUI | Personal inbox and Sessions navigation | Switch providers without mixing data |
| 6 — Personal send | AppleScript adapter, explicit confirmation | Verified supported sends, honest delivery status |
| 7 — Product polish | Browser guests, reactions, notifications | Guest joins from a link without CLI installation |
| 8 — Scale exploration | Multi-instance fanout, load tests | Measure behavior and document failure modes |

**First meaningful demo:** Host creates a one-hour room, shares a code, two clients join and chat, one reconnects and catches up, and host ends the room. Separately, `psst personal chats` lists the Mac's synced conversations.

## 11. Architectural decisions and risks

| Decision | Rationale / tradeoff |
|---|---|
| Separate local Personal and hosted Sessions providers | Protects private data and separates Apple integration from independent network |
| Modular monolith for server | Easier to understand, test, deploy, and evolve than premature microservices |
| PostgreSQL for session messages | Durable, transactional ordering; requires lifecycle cleanup |
| In-memory WebSocket hub first | Minimal moving parts; only one server instance until cross-node fanout exists |
| No end-to-end encryption promise initially | Avoids misleading security claims; TLS and access control still mandatory |
| Undocumented macOS database adapter | Useful locally but fragile; version checks and fallback required |

**Naming note:** “Psst” is a working product name. Verify command/package, domain, and trademark availability before public launch.

## 12. Immediate next steps

1. Initialize Git and confirm the intended GitHub remote/module path; use `psst` for new binaries and current documentation. Commit and push verified checkpoints frequently.
2. Implement `psst doctor` and a read-only Personal adapter using synthetic fixtures.
3. Build the persisted Sessions vertical slice from phase 2 in small runnable steps; establish auth and transaction contracts before WebSocket fanout.
4. Ship the two-terminal session demo before introducing Redis or complex UI features.

Implementation references: [Go cryptographic randomness](https://pkg.go.dev/crypto/rand), [PostgreSQL locking](https://www.postgresql.org/docs/current/explicit-locking.html), and [PostgreSQL database-clock semantics](https://www.postgresql.org/docs/current/functions-datetime.html).

---

## Appendix A — Previous local-only architecture (Ping v0.1)

The following is retained as historical design context for the macOS Personal provider. References to Ping and local-only scope in this appendix describe the **superseded** version, not the current product architecture.

Terminal-native client for existing macOS Messages conversations \| Version 0.1 \| 17 October 2026

Status: Proposed design • Primary language: Go • Target: macOS • Audience: solo developer / portfolio reviewers

# 1. Executive summary

Ping is a local-first terminal messaging client that reads conversations synchronized to the macOS Messages application and sends basic messages through a supported macOS scripting bridge. It does not implement the iMessage protocol, operate an independent messaging network, or claim access to an official public iMessage API. The initial version runs entirely on the user’s Mac.

The design prioritizes separation between the terminal UI, domain logic, and fragile platform integrations. A read-only SQLite adapter accesses the local Messages database; a send adapter invokes macOS automation; a polling-based watcher provides near-real-time updates. All integration behavior is best-effort because Apple does not guarantee these undocumented database structures or automation behaviors.

# 2. Goals, non-goals, and constraints

## Goals

- Browse existing chats and paginated message history in a Bubble Tea terminal interface.

- Send basic text messages to supported existing conversations with explicit confirmation and clear error reporting.

- Refresh the inbox after incoming or outgoing messages; search local synchronized text history.

- Keep message data on-device by default; never write to Apple’s Messages database.

- Provide an adapter-based Go architecture that can be tested without access to real personal messages.

## Non-goals for v0.1

- Implementing Apple’s iMessage transport or running an independent messaging service.

- Guaranteed instant delivery, typing indicators, read receipts, edits, reactions, or attachment sending.

- Cross-platform support, cloud sync, headless remote access, or server-side storage.

- Disabling System Integrity Protection or using private framework injection.

## Constraints and assumptions

- The user is signed into Messages on a Mac and relevant chats are locally synchronized.

- macOS Full Disk Access may be required for the terminal or packaged executable; automation permissions may be required to send.

- The chat.db schema and date encoding can vary across macOS versions.

- Messages may use WAL files; a read-only SQLite connection should be opened against the live database rather than copying only chat.db.

- AppleScript sending can have ambiguous completion and may not support every chat type.

# 3. Context and component architecture

User / keyboard

\|

Ping TUI (Bubble Tea) ---- Ping CLI (Cobra)

\| \|

+------- Application Service -------+

\| \|

MessageProvider SendProvider

\| \|

SQLite read adapter AppleScript adapter

\| \|

~/Library/Messages/ Messages.app

chat.db (macOS)

\|

Polling watcher / cursor

\|

TUI update events

The application service owns chat listing, history pagination, recipient resolution, send validation, and update coordination. Adapters isolate macOS-specific assumptions. The TUI consumes normalized models and never issues SQL or executes AppleScript directly.

## Responsibility boundaries

| Component           | Responsibilities                                                                             |
|---------------------|----------------------------------------------------------------------------------------------|
| TUI                 | Keyboard input, views, status/error states, asynchronous commands.                           |
| Application service | Use-case orchestration, authorization checks where possible, input validation, cancellation. |
| SQLite adapter      | Read-only queries, schema capability detection, date conversion, pagination.                 |
| Send adapter        | Escaped/parameterized script invocation, target selection, timeout, error mapping.           |
| Watcher             | Periodic change detection, deduplication, incremental refresh.                               |
| Config              | Local preferences, feature flags, permission diagnostics.                                    |

# 4. Go repository layout

ping/

cmd/ping/main.go

internal/app/{service.go,errors.go}

internal/domain/{conversation.go,message.go}

internal/provider/{reader.go,sender.go}

internal/imessage/{sqlite.go,queries.go,dates.go,applescript.go,watcher.go}

internal/tui/{model.go,update.go,view.go,commands.go}

internal/config/config.go

internal/testutil/fixtures.go

tests/integration/

docs/architecture.md

go.mod

## Core interfaces

type Reader interface {

ListConversations(ctx context.Context, limit int) (\[\]Conversation, error)

ListMessages(ctx context.Context, chatID string, before \*Cursor, limit int) (\[\]Message, error)

}

type Sender interface {

SendText(ctx context.Context, target SendTarget, body string) error

}

type Watcher interface {

Watch(ctx context.Context) (\<-chan Change, \<-chan error)

}

SendTarget must represent a validated recipient or conversation handle; do not assume that a local chat row ID is a valid AppleScript destination. Explicitly test group-chat behavior and disable unsupported paths.

# 5. Data model and local database access

| Domain object | Fields                                                                 |
|---------------|------------------------------------------------------------------------|
| Conversation  | ID, title, participant handles, service type, last activity, preview.  |
| Message       | ID/GUID, chat ID, sender, body, isFromMe, sentAt, attachment metadata. |
| Cursor        | Stable sort key: timestamp + message row ID (or tested equivalent).    |
| Change        | Conversation ID, latest known cursor, change type.                     |

Read tables such as chat, message, handle, chat_message_join, and chat_handle_join. These are implementation details, not a stable contract. Detect schema columns at startup using PRAGMA table_info and return an actionable unsupported-version error if required columns are missing.

Use SQLite URI mode=ro and PRAGMA query_only=ON. Do not issue INSERT, UPDATE, DELETE, schema migrations, or PRAGMA statements that modify the source database. Open with a short busy timeout and context cancellation. Do not log raw message text or phone numbers.

Pagination: fetch a bounded number of rows ordered by normalized timestamp and a stable tie-breaker. Prefer keyset pagination to OFFSET for long histories. Decode Apple timestamps carefully; verify whether stored values are seconds or nanoseconds relative to the Apple epoch on the actual machine.

# 6. Message flows

## A. Open conversation

TUI -\> App.ListMessages(chatID, cursor)

App -\> Reader.ListMessages(...)

Reader -\> SQLite SELECT (read-only)

SQLite -\> normalized \[\]Message

App -\> TUI render; maintain scroll cursor

## B. Send text

TUI -\> App.SendText(target, body)

App -\> validate nonempty text, destination, size, timeout

App -\> Sender.SendText(...)

Sender -\> macOS automation -\> Messages.app

Sender -\> success / error / uncertain outcome

TUI -\> show status; refresh from SQLite to confirm appearance

An AppleScript call returning without error does not prove delivery to a recipient. The UI should distinguish “send requested,” “observed in local history,” and “delivery unknown.” Avoid blind automatic retries after timeouts, since the first send might have succeeded.

## C. Incoming messages

Watcher -\> poll most recent chat/message keys

Watcher -\> compare against per-chat cursor

Watcher -\> emit Change(chatID)

App -\> fetch incremental rows

TUI -\> merge by stable message ID and redraw

Start with polling every 2–3 seconds while Ping is active, with configurable intervals and backoff on SQLite errors. Do not assume filesystem change notifications correspond to committed rows. Handle edits, deletions, and late synchronization as refreshes rather than only append-only events.

# 7. Security, privacy, and reliability

- Local-first: no telemetry or external API calls by default; avoid persisting a second copy of personal message content in v0.1.

- Principle of least privilege: explain Full Disk Access and Automation prompts; never request disabling SIP.

- Read-only database adapter: enforce mode=ro and query_only, plus tests ensuring no writes.

- No arbitrary shell command execution from message text; use exec.CommandContext with fixed executable/arguments and safe AppleScript parameter passing.

- Do not log chat contents, handles, authentication tokens, or full AppleScript payloads.

- Bound SQL result sizes, message length, watcher queues, and execution timeouts.

- Gracefully degrade when Messages is closed, permissions are revoked, chat.db is locked, or the schema changes.

- Treat AppleScript as best-effort; surface uncertain sends and avoid duplicate resend loops.

# 8. Testing and observability

## Test pyramid

- Unit tests: date decoding, message normalization, cursor ordering, validation, and view state transitions.

- SQLite integration tests: synthetic fixture database with joins, group chats, null text, attachment-only rows, Unicode, and pagination.

- Adapter contract tests: verify read-only mode, capability detection, cancellation, and error mapping.

- macOS manual tests: permission denial, real direct-chat send, incoming message refresh, app restarts, and group-chat limitations.

- Race tests: go test -race ./... for watcher/TUI coordination; test goroutine shutdown and bounded channels.

## Observability

Use structured logs with event names and durations but no personal message payloads. Track local counters such as query errors, refresh latency, watcher lag, and send-attempt outcomes. Offer a diagnostics command (ping doctor) to report platform version, database readability, schema capabilities, and automation permission status without exposing private data.

# 9. Implementation milestones and acceptance criteria

| Milestone   | Deliverable / acceptance                                                           |
|-------------|------------------------------------------------------------------------------------|
| M0 — Probe  | CLI reports OS, permissions, tables/columns without printing personal data.        |
| M1 — Read   | List ten chats and page through one conversation in a read-only CLI.               |
| M2 — TUI    | Keyboard-driven inbox, history, scroll, and loading/error states.                  |
| M3 — Send   | Explicit send action to a verified 1:1 destination; no duplicate retry on timeout. |
| M4 — Watch  | New messages appear after polling; reconnect/restart has no duplicates.            |
| M5 — Polish | Search, packaging, documentation, diagnostics, fixture-based CI tests.             |

Suggested sequencing: implement M0 and M1 before styling the TUI. Build mock Reader/Sender implementations to develop the UI without exposing real conversations. Only attempt M3 after validating AppleScript behavior on the target macOS release.

# 10. Architectural decisions and tradeoffs

| Decision                       | Reason / tradeoff                                                         |
|--------------------------------|---------------------------------------------------------------------------|
| Local-only v0.1                | Minimizes security surface; does not teach cloud scaling yet.             |
| SQLite read-only               | Direct access to local history; fragile undocumented schema.              |
| AppleScript sender             | Avoids SIP changes; limited functionality and delivery certainty.         |
| Polling watcher                | Portable/simple within macOS; adds latency and query load.                |
| Interfaces at adapter boundary | Enables fixture tests and future BlueBubbles provider.                    |
| No separate cache initially    | Avoids storing another sensitive copy; search performance may be limited. |

# 11. Risks and fallback paths

- macOS schema change: version-gated query implementations; disable unsupported features and show diagnostics.

- Automation cannot target desired group: limit send support to verified 1:1 contacts; optionally investigate BlueBubbles later.

- Full Disk Access friction: clear onboarding, permission checks, and a privacy explanation.

- Sync lag or missing history: state that Ping reflects only locally available Messages data.

- Send uncertainty: do not equate script completion with confirmed recipient delivery.

- Future remote access: require a separate threat model, authentication, transport encryption, and opt-in exposure.

# 12. Future evolution

After shipping a dependable local client, consider an optional BlueBubbles provider, attachments, searchable local indexes, a macOS background agent with Unix-domain-socket IPC, and a securely authenticated remote terminal client. If the goal later shifts toward distributed backend engineering, a separate Ping-native messaging service could be built behind the same provider interface; that would be a distinct product rather than an iMessage transport implementation.

# 13. Immediate next steps

- Initialize the Go module and folder structure.

- Implement ping doctor to inspect chat.db schema without revealing content.

- Create Reader interface and SQLite adapter with synthetic test fixtures.

- Implement conversation listing and paginated history before starting Bubble Tea.

- Document the exact macOS version and supported/unsupported message types.
