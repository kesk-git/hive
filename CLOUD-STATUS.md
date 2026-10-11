# Cloud status: the command harness lane

Lane: `contract/envelope/` and `contract/cli/` of the stage 1 definition.
Toolchain: Go 1.27.0, standard library only.

## What was built

### `contract/envelope`

The request and response envelopes of `spec/commands.md`, the receipt
envelope of `spec/receipts.md` and the capability declaration, as typed
values with strict decoding and canonical encoding.

- `status.go`: the eleven exit states as one table. Each `Status` has one
  exit code and each exit code one status; every common error code of the
  table in `spec/commands.md` maps to its status. `Failure` is the Go error
  an operation returns to end an invocation with a status other than `ok`;
  it carries the error fields the status requires (`limit_bytes` and
  `actual_bytes` with `too_large`, `hold` with `held`) and the receipts a
  refusal or a hold leaves. `Check` holds a failure to the schema's rules,
  and `unknown` can never be made retryable.
- `request.go`, `response.go`, `receipt.go`, `capabilities.go`: the four
  documents. Decoding reads the bytes as a Hive record, tells
  `malformed_request` (not a record), `unsupported_version` (a newer
  `schema_version`, major or minor) and `schema_violation` apart, validates
  against the embedded schema and only then fills the typed value, keeping
  every body verbatim with whole numbers. Encoding writes canonical form
  with one final line feed and checks the document against its schema
  first, so a command never answers with a response it would not accept.
- `Accept` is the caller's rule: a response is accepted only if the whole
  output parses, follows the schema, echoes the caller's `request_id` and
  agrees with the exit code; anything else, including empty output and a
  process ended by a signal, is `unknown`.
- `codec.go`: version comparison, `ValidateBody` for an operation's body
  against the schema its declaration names, and `Digest` of a request's
  canonical form for replay.

### `contract/cli`

The harness: every command is invoked, answered and failed the same way.

- `command.go`: a package describes its `Executable` once (package, name,
  version, commands, each with limits and operations carrying their
  capability declaration and a handler). `Check` holds the description to
  the contract. `Call` is what a handler sees, with `NewReceipt` to make a
  receipt bound to the request's scope and authority; `Result` is what it
  returns.
- `invoke.go`: `Main` reads the arguments and answers `usage` (exit 2)
  before anything is read for an unowned command name, an unknown or
  malformed operation name, a missing or repeated option, a positional
  argument or an argument over `max_argument_bytes`; prints `--version`
  as one line; answers `capabilities` with a null `request_id` and the
  declaration as the body; reads the request from `--request-file` or
  standard input up to `max_request_bytes` and answers `too_large` with
  the whole size counted before the request is parsed; writes the one
  response document to `--response-file` (owner only, flushed) or standard
  output; prints a diagnostic line on standard error for every status but
  `ok`; and runs a resident operation's continuation after the response is
  out.
- `handle.go`: `Handle` runs one invocation through the whole pipeline and
  is also the entry for a convenience form that builds a request: size;
  the whole request validated before anything else (`malformed_request`,
  `unsupported_version`, `schema_violation`, `envelope_mismatch`);
  `unsupported_operation` and `operation_parked` by the declared state;
  the body version against the versions the operation accepts and the
  body against its request schema; the home (`home_not_found`,
  `home_inside_repository`, `home_mismatch`, `record_malformed`); the
  scope; then, for a mutating operation, the store's lock (`lock_held`,
  retryable) and the replay check; then the handler, with a panic turned
  into `failed`; the response sized before anything is written, so a
  response over `max_response_bytes` is `too_large` in its place and
  leaves nothing; and the records, receipts and events of the handler
  committed in one transaction with the recorded outcome. A refusal or a
  hold that leaves receipts is recorded the same way; an unknown outcome
  is never recorded as complete. A response body is checked against the
  operation's response schema before it is returned.
- `outcome.go`: the recorded outcome at `<package>/requests/<request_id>.json`,
  holding the digest of the request's canonical form and the response as
  returned. A replay with the same digest returns the recorded response
  with `replayed` true and writes nothing; a different digest is
  `conflict` with `replay_mismatch`.
- `scope.go`: the callee's scope check of `spec/identities.md`: the
  workspace, project and clone named exist in `dispatch/registry/`, are
  active and belong to each other and to the home, read through a
  validated reader that takes no lock and calls no package. A workspace
  is in scope of its Fleet home and of the Hive's home that is its parent.

### Tests

`contract/envelope/envelope_test.go` with fixtures under `testdata/`: the
eleven states and the code table, failure rules, version ordering, round
trips of all four documents to their canonical form, every rejection code,
the acceptance rule, and body validation.

`contract/cli/cli_test.go` with a test executable (`hive-dispatch` owning
`hive-fleet` with read-only, mutating, refusing, holding, failing,
resident, provisioning, unsupported and parked operations, and `hive-park`
with none): every one of the eleven exit states is produced through
`Main` and accepted by the caller's rule; usage errors before anything is
read; invalid requests with the request identity echoed where readable;
refusals of every common kind including each scope mismatch; replay by
canonical digest with spacing ignored, `replay_mismatch`,
`revision_conflict` inside the transaction and `lock_held` driven by a
fake clock; held with its receipt and its replay; size errors by standard
input and by file, at and over the limit, and a response too large that
leaves nothing; unavailable homes, registry records, outcomes and request
files; file and standard-stream transport; a resident operation; and that
the recorded outcome is the response as returned.

## What each command printed

`make check`:

```
go vet ./...
```

exit 0.

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon
ok  	github.com/kesk-git/hive/contract/cli
ok  	github.com/kesk-git/hive/contract/clock
ok  	github.com/kesk-git/hive/contract/envelope
ok  	github.com/kesk-git/hive/contract/ids
ok  	github.com/kesk-git/hive/contract/schema
ok  	github.com/kesk-git/hive/contract/store
?   	github.com/kesk-git/hive/spec	[no test files]
```

exit 0. The two new packages also pass under the race detector.

## Decisions the specification leaves open, and how this lane took them

None of these changes a file outside the lane; each is a reading a later
change can fix in one place.

1. **The recorded outcome has no schema file.** `spec/records.md` says
   every record carries `schema` and `schema_version`, and
   `spec/commands.md` says a mutating command records the outcome under
   `requests/`, but no file under `spec/schemas/` defines that record.
   `contract/cli/outcome.go` writes it as `hive.request-outcome` version
   `1.0` with `request_id`, `request_digest` and `response`, and reads it
   by hand, refusing a newer version. A schema file at
   `spec/schemas/request-outcome.v1.schema.json` can be added by the
   owner of `spec/` without changing the records written.
2. **A directory with no `home.json`.** The table gives `home_not_found`
   for the thing named not existing and `home_mismatch` for a home that
   does not match. An existing directory without a home record is
   answered `not_found` with `home_not_found`, for every operation but
   the one marked `Provisions` (the identity slice of `hive-fleet
   provision`), which is admitted and creates the record.
3. **A malformed registry record during the scope check.**
   `spec/identities.md` says a failure of the scope check is refused with
   `scope_mismatch`; `spec/records.md` says a record that fails its schema
   stops the mutation with `record_malformed` and a read of it reports the
   same. A record that is missing, retired or bound elsewhere is
   `scope_mismatch`; one that cannot be read or breaks its schema is
   `unavailable` with `record_malformed`. The two sentences are not read
   as contradicting each other, since the second is the narrower.
4. **An unreadable `--request-file`.** No row names it. It is
   `unavailable` with `source_unreadable` ("a source cannot be read").
5. **The replayed response.** It is the recorded response with `replayed`
   true and nothing else changed, so its `completed_at` is the original's.
6. **The scope check's validated reader** lives inside `contract/cli`
   (`scope.go`) for now. The contract library's declaration names "the
   validated reader of the shared reads" as one piece; when a later lane
   builds it as its own package, `scope.go` can call it instead.
7. **An unknown outcome with receipts.** The harness returns them in the
   response and writes nothing, since an unknown outcome is never
   recorded as complete; what it leaves durable is the intent the
   operation records before an outside effect, which belongs to the
   execution lane.
8. **`capabilities` is subject to `max_response_bytes`** like every other
   response, so a command's declared limit must admit its own
   declaration.

Nothing outside `contract/envelope/`, `contract/cli/` and this file was
changed.

done claude/contract-envelope-cli-yhu0uq
