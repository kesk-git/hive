# Cloud status: lane c, the home's journal

## What was built

`contract/journal/` implements the home event journal of `spec/events.md`, behind the `store.Journal` seam the storage lane landed:

- **Append, sequence and lock.** `Append` takes `locks/journal.lock` through the store's lock code, learns the next `home_sequence` from the last intact line, sets `home_sequence` and `recorded_at` on each event, writes each line in canonical form with its line feed in one write and flushes it before the next. It refuses, before writing anything, an event whose kind does not begin with the opening package's name, whose `event_id` differs from the one given, that already carries a member the journal sets, that belongs to another home, or that fails the envelope schema or its kind's body schema. An event whose `event_id` is already in the journal is skipped, so the store's roll-forward may hand it the same events again.
- **Segments.** Files named `events-<first home_sequence>.jsonl` with twenty digits. A segment is closed when the next line would take it past the configured size (`Options.SegmentSize`, with a default), and a line larger than the size stands alone. Closed segments are never written again; retention is left to the home, and a cursor before the kept segments is reported with `ErrGone`.
- **Torn-tail repair.** A final line without its line feed is ignored by readers and by `Head`. The next `Append` removes it and appends one `hive.journal.tail-repaired` event ahead of its own, with `segment`, `removed_bytes` and `removed_digest` in the body, a basis of kind `record` with schema `hive.journal` and the segment's file name as id, `source.package` set to the opening package and `source_id` `journal`. The repair line is written over the torn bytes before the file is cut, so an interruption between the two leaves at worst another torn tail and never an unrecorded removal.
- **Reader and cursor.** `Reader(cursor)` resumes after the last handled `home_sequence`, takes no lock, validates every line through the embedded schemas, requires the sequence to rise by exactly one across segments, continues when more lines arrive, and reports a line it cannot read as `record_malformed` and a newer envelope version as `unsupported_version`.

Decisions taken inside what the specification leaves open: the journal sets `home_sequence` and `recorded_at` itself and refuses content that carries them; the repair event's `correlation_id` is that of the first event of the append that found the tail, and its `home_id` is the home record's, else the last intact line's, else that of the event being appended.

Tests and test data live under `contract/journal/`: sequence and canonical form, segment rollover, append-once across recovery, the store committing through the journal under both locks, lock contention by a controlled clock, every refusal, malformed and newer-version tails, repair in the middle of a segment, at a segment's first line, in a full segment, of a journal's first line, of a fixture segment, cutting of tails shorter and longer than the repair line, the body fixtures of the repair event, reader resumption and retention, and the reader's refusals.

No file outside `contract/journal/` was changed.

## What the checks printed

`make check`:

```
go vet ./...
```

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.034s
ok  	github.com/kesk-git/hive/contract/clock	0.004s
ok  	github.com/kesk-git/hive/contract/ids	0.010s
ok  	github.com/kesk-git/hive/contract/journal	0.172s
ok  	github.com/kesk-git/hive/contract/schema	1.327s
ok  	github.com/kesk-git/hive/contract/store	2.639s
?   	github.com/kesk-git/hive/spec	[no test files]
```

Both exited 0 on the cloud machine with the Go 1.27 toolchain the module requires.

done claude/contract-journal-mgp5o3
