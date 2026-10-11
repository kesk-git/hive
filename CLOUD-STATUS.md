# Cloud status: the durable-outcome lane of the command harness

## What was built

The open finding of the harness review is closed at the store and mapped in one
place in the harness, as the lane fixed it.

Store (`contract/store/tx.go`, `contract/store/errors.go`):

- `LockFor(requestID)` is the open-with-recovery for the request in hand. Every
  error it can fail with, taking the lock, enumerating the intents, the first
  flush of `tx/`, an intent that cannot be completed (this request's or an
  earlier one's), and the cleanup after recovery, is an `*InterruptedError`
  naming the request whenever that request's own transaction is durable: its
  intent is on disk, or its outcome is. `Lock` keeps its behaviour and its
  marker for the intent that stopped it.
- `Commit` reports the same way for the write of the intent.
- Two recovery steps are named for a test's fault: the enumeration of the
  intents and the cleanup after recovery. `Fault` installs a test's fault on a
  store; the harness's tests need it because they cannot reach the store's
  unexported hook, and nothing else in the lane could make the first flush of
  `tx/` fail by control.

Harness (`contract/cli/handle.go`, `contract/cli/command.go`):

- `settle` is the one function that turns the store's report into a response,
  and every path of a mutating operation after the store is touched answers
  through it, from `Handle` and so from `Main`. Once the request's transaction
  is durable it answers only the response as recorded, when it fits the limit,
  or `unknown` with `outcome_unknown`, not retryable and with no receipts; a
  replay whose recorded response no longer fits the present limit is answered
  so too. It sizes the response itself.
- The floor is checked per operation, by `Check` for every supported mutating
  operation and again in the harness for the operation in hand before its
  store is touched. `Handle` refuses a mutating operation of a command whose
  limit is below the floor with an error and no response, as `Main` refuses
  the executable: no status would fit such a limit without promising that
  nothing changed.
- `storeFailure` maps only errors that left nothing of the request behind.

Tests by control, each shown to fail without its fix:

1. `TestRetryThatFailsRecoveryIsUnknown`: a retry whose intent is durable that
   fails the enumeration, the first flush of `tx/`, or the cleanup answers
   `unknown`, never `failed`.
2. `TestHandleHoldsTheFloor`: an interrupted commit through `Handle` under a
   limit that admits a small success but not the unknown answer is refused at
   the floor before anything is written, never `too_large`; at the floor
   exactly it answers `unknown`.
3. `TestReplayThatNoLongerFitsIsUnknown`: a put interrupted at the journal
   append, recovered on retry with the limit lowered below its recorded
   response, answers `unknown`, never `too_large`, and the response as recorded
   once the limit is restored.
4. `TestLockForReportsTheRequestInHand` in the store: the classification at
   every step of recovery, against a held lock, and when another intent stops
   the recovery first. The regression assertion requiring the interrupted
   marker is kept.

One judgment call: a lock held by another command while the request's own
transaction is durable is also reported as interrupted, so the harness answers
`unknown` rather than `conflict`, which would promise that nothing changed.

## What the targets printed

`make check`:

```
go vet ./...
```

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.021s
ok  	github.com/kesk-git/hive/contract/cli	0.997s
ok  	github.com/kesk-git/hive/contract/clock	0.005s
ok  	github.com/kesk-git/hive/contract/envelope	0.031s
ok  	github.com/kesk-git/hive/contract/ids	0.012s
ok  	github.com/kesk-git/hive/contract/journal	0.532s
ok  	github.com/kesk-git/hive/contract/schema	1.790s
ok  	github.com/kesk-git/hive/contract/store	4.972s
?   	github.com/kesk-git/hive/spec	[no test files]
```

The cloud machine fetched the Go toolchain the module requires and ran both
targets with it.

done claude/outcome-unknown-recovery-swhmha
