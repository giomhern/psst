# Local contact names

Explicit chat names take priority. Unnamed chats resolve up to five participants
through Apple's Contacts framework, falling back to phone/email identifiers.
Groups with more participants get an ellipsis. Chats with no usable participants
keep their previous fallback. The CLI bounds titles and escapes control characters.

## Setup and consent

On macOS with Xcode Command Line Tools, `make contacts` builds an ad-hoc-signed
`bin/psst-contacts` with an embedded Contacts usage explanation and copies only
that executable to `~/Library/Caches/psst/psst-contacts` with mode 0700.
No contact data or credentials are installed there.

Run `psst personal contacts authorize` (or `go run ./cmd/psst personal contacts
authorize`) to explicitly request access. Only this action invokes `requestAccess`;
normal listing and `personal contacts status` do not prompt. Psst does not change
privacy settings. Full Disk Access and Contacts access are separate permissions.
Depending on macOS attribution, settings may identify the helper or the responsible
terminal/app. Rebuilding/relocating the binary may require reauthorization.

The helper uses Apple's phone/email predicates and full-name formatter, not custom
suffix matching. It reads formatting keys for matching contacts only, and stops
after two unified matches to detect ambiguity. Ambiguous, absent, inaccessible,
or nameless contacts keep the identifier. These names may differ from Messages'
nicknames and name/photo-sharing presentation. No contacts are saved or modified.

## Limits and privacy

- Messages access remains read-only. Its transaction finishes before helper I/O.
- Only unnamed displayed chats contribute identifiers: at most 500 per batch,
  at most 320 UTF-8 bytes each, in a versioned JSON request capped at 1 MiB.
- Lookup gets a 1.5-second sub-deadline inside the three-second chat deadline.
  Optional failure uses fallback titles; parent cancellation stops the command.
  Large listings can exhaust the lookup budget.
- Responses are capped at 2 MiB. Go accepts known statuses and requested identifiers
  only; names are bounded to 200 Unicode scalars.
- Identifiers travel on stdin, never shell source or process arguments. No names
  or handles are logged, uploaded, or saved in a contact cache. Helper stderr and
  raw process errors are suppressed in favor of safe failures.
- Discovery uses an absolute `PSST_CONTACTS_HELPER`, a sibling of the Go executable,
  or the Psst user-cache location. It never searches PATH/current directory.
  `PSST_CONTACTS_HELPER=off` opts out of native lookups.
- The helper is optional. Go builds and Linux server builds do not require Swift.

## Validation

Go tests use synthetic participant metadata, fake resolvers, and temporary fake
executables. They cover precedence, fallbacks, bounds, permission states, cancellation,
subprocess execution, and safe errors. `make contacts-helper-test` covers native
ambiguity and Unicode truncation using synthetic names. CI builds/signs the helper
on macOS and exercises help without requesting access.

No automated test fetches real contacts or Messages data. Real permission attribution
and matching require explicit user-run validation after installation. The valid-input
native lookup is not exercised against a real contact store in automated tests.

References: [CNContactStore](https://developer.apple.com/documentation/contacts/cncontactstore),
[phone matching](https://developer.apple.com/documentation/contacts/cncontact/predicateforcontacts(matching:)),
[email matching](https://developer.apple.com/documentation/contacts/cncontact/predicateforcontacts(matchingemailaddress:)),
and [name formatting](https://developer.apple.com/documentation/contacts/cncontactformatter).
