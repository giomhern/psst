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

The helper first uses Apple's phone/email predicates and full-name formatter.
It accepts exactly one unified match. When a predicate returns no match, a single
bounded framework enumeration compares complete normalized identifiers for the
remaining participants. This fallback reads phone/email and formatting keys for
up to 10,000 contacts, retaining at most two candidate records per requested key.
It stops and discards all fallback candidates if the scan is incomplete, exceeds
100 phone/email values on any record, or fails. Existing predicate matches survive
fallback errors. Normal command cancellation/deadlines still apply.

Normalization removes phone formatting and compares complete digit sequences,
including explicit country codes. A country-less valid ten-digit NANP number
(or eleven digits starting with 1) gains `+1` only on devices configured for US/CA.
Other national numbers stay national. Email addresses compare case-insensitively.
There is no suffix matching or removal of international country codes; extensions
and unsupported strings are not guessed. This intentionally leaves some formats
unmatched. Distinct matching unified records remain ambiguous even with identical
names. Only a complete successful scan can establish a unique fallback match.

The framework's direct predicate API returns all matches without a caller-supplied
result limit; ambiguity is rejected before formatting. No contacts are modified,
uploaded or cached. Names can differ from Messages' nicknames and name/photo
sharing presentation. One failed lookup does not discard other successful names.

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

## Troubleshooting missing names

Authorization and a successful name lookup are separate. If permission is granted
but chat titles still show identifiers, run:

```sh
go run ./cmd/psst personal contacts diagnose
```

This explicitly reads participant metadata for the ten most recent chats and
performs the same bounded Contacts lookup as listing. It prints only counts and
fixed statuses, never names, identifiers, message text, or native error details.
It does not request permission or modify Messages/Contacts.

- `Lookup: timeout`: the lookup exceeded its 1.5-second budget.
- `Lookup: unavailable`: the helper or Contacts lookup failed. The authorization
  status above it can reveal missing helpers or permission problems.
- `Lookup: completed` with zero matches: the helper completed but returned no
  usable unambiguous names for the requested participants.
- `Lookup: skipped`: there were no eligible participants or no resolver.
- A high stored-name count with few identifiers means stored chat display names
  bypassed contact matching; these counts help identify that separate case.

Share this summary to investigate further; there is no need to share private
phone numbers, email addresses, or contact records. Automated checks use synthetic
fixtures; this command runs against real local data only when explicitly invoked.

Native lookup failures now report a fixed category such as `invalid_predicate`,
`unauthorized_keys`, `contacts_access`, `contacts_communication`, or `contacts_data`.
`helper_process` means the helper exited unsuccessfully; `helper_response` means
its protocol response was invalid. `request_invalid` means request validation failed.
Unknown native errors become `lookup_failed`. Categories contain no native error
messages or record data. Successful matches survive per-identifier failures.
Rebuild/install the helper with `make contacts` after updating native source.

The `contacts_data` category represents `CNErrorDomain` code 2, a framework
read failure. It does not by itself establish data corruption or a permission
problem. The helper now uses Apple's direct unified-contact predicate fetch
for the initial phone/email search, with a bounded enumeration fallback for
missing matches. Synthetic store
injection verifies this path without opening the real contact store; whether it
resolves a particular machine's framework failure requires local validation.

Diagnostics now include aggregate counts for missing, ambiguous, nameless,
unsupported, and failed identifiers. These optional version-1 response fields are
validated by Go against known keys and the requested batch size. Older helpers
remain usable but prompt for a rebuild to obtain detailed counts. This distinguishes
successful fetches with unusable matches without revealing any contact strings.
