# Cloud status: identifiers and storage, lane b (`contract/store/`)

Go 1.27.0 on Linux. Both targets pass from the repository root.

## What was built

Everything is under `contract/store/`, on the standard library and the landed packages `canon`, `clock`, `ids` and `schema`.

- `errors.go`: the store's errors and `Code`, which maps each to the common error code of spec/commands.md (`record_malformed`, `revision_conflict`, `already_exists`, `lock_held`, `record_too_large`, `home_inside_repository`). `SizeError` carries the limit and the actual size.
- `home.go`: `CheckHome`, the home layout check of spec/records.md "The home": a directory, not inside a Git working tree (a `.git` entry in it or above it), open to its owner only, every known top-level entry of the kind the table fixes, `home.json` read through the embedded `home.v1` schema, and every unknown entry listed and left alone. `Open` builds on it and creates `<home>/<package>/` and `locks/` with owner-only access, for the six packages the table gives a directory (`word` has none).
- `file.go`: the four steps of "Writing one record" (temporary file named for the request, flush, rename, flush the directory), the sweep of temporary files by the next writer, the strict reader of a record file, and the fixed paths `records/<kind>/<id>.json`, `receipts/<id>.json`, `requests/<request_id>.json`, `tx/<request_id>.json`.
- `lock.go`: `Acquire`, the transaction lock as an advisory file lock on `locks/<name>.lock`, exclusive or shared, waited for a bounded time by a clock input with an injectable pause so a test runs the wait by control. A lock file left by a crash is locked afresh; the file is never removed.
- `store.go`: `Store`, `Writer`, `Replace` (sets `record_revision` to expected+1 and `updated_at` from the clock, writes canonical form, refuses a wrong revision or a present record with nothing written), `WriteOnce` for receipts and outcomes, `ReadRecord` and `Read` without a lock, and the size limit a store may be given.
- `tx.go`: `Tx` and `Commit` for "Writing several records": every check before anything is written, the intent under `tx/` holding every file with its expected state (absent, or a revision) and the digest and content of the new file, then records, receipts, events, the outcome, and the removal of the intent. `Lock` rolls every intent forward by what it finds: a file at the intended digest is left, a record at its expected revision or absent as expected is written, anything else stops the store with `record_malformed` naming the intent, which is never discarded. Events go through a `Journal` interface the caller supplies, because the home journal is the next lane's.

## What each command printed

```
$ make check
go vet ./...
exit 0

$ make test-contract
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.019s
ok  	github.com/kesk-git/hive/contract/clock	0.004s
ok  	github.com/kesk-git/hive/contract/ids	0.013s
ok  	github.com/kesk-git/hive/contract/schema	1.274s
ok  	github.com/kesk-git/hive/contract/store	1.294s
?   	github.com/kesk-git/hive/spec	[no test files]
exit 0
```

The store's tests also pass under the race detector and when run as an unprivileged user (the cloud container runs as root, under which one case, a write into a directory closed to the writer, cannot be exercised; that case logs that it is not run and runs on an ordinary account, as on the Mac).

## What the tests prove

- Atomic replacement interrupted at each of its five steps: the record is the whole old one or the whole new one, a reader never sees a half, and the next writer removes the temporary file. Whole, partial and empty temporary files are never read as records.
- A transaction interrupted at each of its steps (about forty for a transaction with three records, two receipts, two events and an outcome): before the intent is durable nothing happened; from then on the next lock completes it to exactly the state of an uninterrupted run, with every event once, and a further lock changes nothing. Recovery itself interrupted at each of its steps is completed by the lock after it.
- A torn or partial intent (cut in half, cut before its line feed, empty, not JSON, another schema, version, package or request, a digest that does not match, an unknown member, a missing outcome, a record without an expected state, an event without its identifier) stops the store with `record_malformed` naming the intent, writes nothing, appends nothing and keeps the intent; a torn temporary file of an intent is removed. A record at another revision, present where absence was expected, absent where a revision was expected, or malformed, and a receipt or outcome present with other content, stop the store the same way; a file already at the intended digest is left unwritten.
- Locks: contention under a fake clock with the bounded wait and `lock_held`, shared readers against each other and against a writer, a lock file left by a crash taken at once without changing its content, and the lock released when recovery stops so a shared reader is not kept out.
- Size: a store given a limit refuses a record, a receipt, an outcome or a whole transaction over it with the limit and actual size, writing nothing and leaving no intent; without a limit the same record is written.
- The layout check against a full home, an empty directory, a relative path and a symbolic link, and against eighteen invalid layouts: missing, a file, open beyond the owner, a known directory that is a file or a symbolic link, `home.json` that is a directory, torn, without its line feed, empty or failing its schema, and `.git` in the home or above it as a directory or a file.

## Open questions

- The intent record (`schema` `hive.tx-intent`, version `1.0`) has no schema file: spec/schemas/ is fenced and spec/records.md fixes the intent's content, not its schema. Its shape is fixed in `tx.go` and read strictly; a later change may add `spec/schemas/tx-intent.v1.schema.json` and validate it through the registry.
- The bound on the wait for a lock is not fixed by the specification. The store waits five seconds by default; `Waiting` sets another.
- The store validates no record against its schema except `home.json` in the layout check. The validated reader of the shared reads (identity records, review files) is left to the lane that builds it; `ReadRecord` gives it the whole record and its revision and digest.
- `Journal.Append` must skip an event whose `event_id` is already in the journal, as spec/events.md "Appended inside the transaction" requires; the store relies on that for idempotent recovery and the fake journal in the tests does it.

No file outside `contract/store/` needed a change, and no contradiction was found in the specification for these files.

done claude/contract-store-atomic-writes-32xaow
