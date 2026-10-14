# Events

An event says that something happened, for the benefit of whoever watches: the Shadow, History and the map. A receipt is the proof; an event is the notice of it. Every event therefore points at the receipt, observation or record that establishes it, and an event with nothing behind it is never written.

Owner: the package that writes an event owns its kind. The journal is shared and written only through the contract library. Schema: [`event.v1`](schemas/event.v1.schema.json). Proved by `test-contract` for the envelope and the journal, and by the owning package's target for each kind.

## The envelope

| Field | Meaning |
| --- | --- |
| `schema`, `schema_version` | `hive.event` and the envelope's version. |
| `event_id` | Stable for the event. A replay, a recovery or a reconnect presents the same identity. |
| `kind`, `kind_version` | What happened, and the version of that kind's body. |
| `source.package` | The package that wrote the line. It equals the first segment of `kind`. |
| `source.source_id` | The registered source: the package's own name, or a source it observes, written as the package and up to four further parts, such as `shadow:github:<owner>/<repository>`. |
| `source.source_sequence` | The source's own order, where it has one. |
| `home_sequence` | The event's position in its home's journal. |
| `scope_level`, `home_id`, `workspace_id`, `project_id`, `clone_id`, `task_id`, `generation` | The [scope](identities.md#scope). |
| `source_revision` | The Git revision the event concerns, where it concerns one. |
| `correlation_id`, `causation` | The chain it belongs to and what directly caused it. |
| `occurred_at` | When the thing happened at its source. |
| `observed_at` | When Hive read it. |
| `recorded_at` | When the line became durable. |
| `occurred_at_known` | False when the source gave no time and `occurred_at` repeats `observed_at`. |
| `basis` | The receipt, observation or record that establishes the event, at its revision. |
| `body` | The kind's own content. |

## Kinds

A kind is the owning package's name, a subject and a fact in the past tense: `dispatch.launch.accepted`, `synapse.readiness.invalidated`. A kind whose content needs more than the envelope has a body schema at `spec/schemas/events/<kind>.v<major>.schema.json`; a kind that registers none has an empty body. Stage 1 registers no event kind beyond the journal's own, below, whose body schema is [`events/hive.journal.tail-repaired.v1`](schemas/events/hive.journal.tail-repaired.v1.schema.json), and the diagnostic kind, `<package>.diagnostic.recorded`. The diagnostic kind is the one kind common to every package: each package owns its own instance of it and writes only that, and all of them have the one body schema [`events/diagnostic.recorded.v1`](schemas/events/diagnostic.recorded.v1.schema.json), which the journal resolves for the subject and fact `diagnostic.recorded` whichever of the seven packages writes it. Each stage registers its kinds as it builds them: stage 2 registers the Pool's twenty-four, each with an empty body, in [pool.md](pool.md#receipts-and-events), seven of them for the questions the Mind's word persists.

## The three times

`occurred_at`, `observed_at` and `recorded_at` are always present and never merged. A consumer adds its own time of receipt and keeps that apart too.

- A local receipt is observed when it is written, so the three are close.
- A remote change is observed on the Shadow's bounded cadence, so `observed_at` may trail `occurred_at` by the length of a poll.
- An event recorded after a later event of the same source was already recorded is **late**. Lateness is computed by the reader from these times and the source's order. A late event is kept and shown as late; it is never dropped and never re-dated.

## The journal

Each private home has one event journal, in `journal/`.

- It is a sequence of segment files named `events-<first home_sequence>.jsonl`, the number written as twenty digits. Each line is one event in canonical form.
- **Append only.** A line is never changed or removed. A segment is closed at a configured size and a new one begins; closed segments are kept for the retention the home configures, which is never shorter than the life of any task whose events they hold.
- **One order per home.** `home_sequence` starts at 1 and rises by exactly one with each line, with no gap, across segments. It is the order of that home and of no other: the journals of two homes are never merged into one order.
- **One writer at a time.** A package appends through the contract library, which takes `locks/journal.lock`, reads the last line to learn the next `home_sequence`, writes the line with its line feed in one write, flushes it and releases the lock. A package appends only events whose kind begins with its own name.
- **A torn tail is repaired, never read.** A final line without its line feed is an append that was never acknowledged. A reader ignores it. The next writer, holding the lock, removes it and appends one `hive.journal.tail-repaired` event, so the repair is itself in the journal. Its body holds the `segment` the bytes were removed from and the length (`removed_bytes`) and digest (`removed_digest`) of what was removed. Its `basis` is the journal's own record: kind `record`, a ref with schema `hive.journal` and the segment's file name as `id`; the last intact `home_sequence` before the repair is the event's own `home_sequence` minus one. There is no other evidence store: the removed bytes are not kept. This is the one event kind that belongs to no package: the contract library writes it, with `source.package` set to the package whose command made the repair and `source.source_id` `journal`.
- **Appended inside the transaction.** An event is listed in its transaction's intent and appended after the records and receipts it reports. Recovery appends an event the intent names only if no line with that `event_id` is already present. A diagnostic event is the one exception: a failed request has no transaction, so it is appended on its own, as [below](#diagnostic-events) states.

Reading the journal is open to every package of the same home, through the contract library's reader and never by opening the files directly: the journal is the home's shared notice board, and holds nothing that is not already recorded by its owner, apart from a diagnostic event, whose detail is recorded nowhere else. A reader keeps a cursor, the last `home_sequence` it handled, and resumes after it. No package reads the journal of another home; a Fleet's events reach its parent only as the summary the Fleet writes, as the design requires.

## Diagnostic events

The detail of a failed command lives in one place: the home's event journal. The response's `error.message` is one sentence naming the field and shape that failed, and standard error is one line ([commands.md](commands.md#invocation)); what a failure holds beyond that, the lower-layer error chains, paths, versions, step names and a panic's stack, goes where a home is named into the journal as one diagnostic event of the answering package. It is written through the contract library's journal like every event, with no new store and no new owner.

- **Kind.** `<package>.diagnostic.recorded`, `kind_version` 1.0, `source.package` the answering package and `source.source_id` the package's own name.
- **When.** Only when the failure has detail beyond its message, that is a lower-layer error chain or a panic. A failure whose message already says everything, such as a usage or validation refusal naming the field and shape, a plain `not_found`, `refused` or `held`, writes no event. No `ok` response and no replayed response writes one. The status, the code and the message of the failure are unchanged by it: only the sentences move.
- **Where a home is named.** The event is written only when `--home` names a directory whose home record reads, so that `home_id` is known, and the home journal opens. Every executable is constructed with the home journal; a caller of the library that gives none writes none. Otherwise the detail is dropped, nothing is written, and standard error still gets its one line.
- **No free text before the no-value check.** The event is appended only through a journal whose own validation applies the no-value check of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section, and the journal says whether it does. The event's writing begins with that check: the journal landed in stage 1 does not apply it yet, so with it nothing is appended and the detail is dropped, the response, its status, its exit code and its one line of standard error standing. When the fixed list of credential shapes enters the schema package with stage 2's first writer and the journal's validation applies it, the event is written with no further change.
- **Scope, correlation and basis.** When the request was read, the event takes the request's scope and `correlation_id`, its `causation` is the request, a reference of schema `hive.request`, and its `basis` is the observation of that request: kind `observation`, a ref of schema `hive.request` whose `id` is the failed request's `request_id`. When the request as a whole could not be read but its `request_id` could, the response carries that `request_id` and so does the event: its `causation` and `basis` are that request, as when the request was read, while its `correlation_id` is minted for it and its `scope_level` is `hive` with the `home_id`, since neither was read. When not even a `request_id` could be read, so that there is no `request_id`, no `correlation_id` and no scope, the detail is still kept: the event's `correlation_id` is minted for it, its `scope_level` is `hive` with the `home_id`, it has no `causation`, and its `basis` is kind `observation`, a ref of schema `hive.invocation` whose `id` is the event's own `correlation_id`.
- **Body.** A closed object, [`events/diagnostic.recorded.v1`](schemas/events/diagnostic.recorded.v1.schema.json): `command`, `operation` (absent when none), `status`, `code`, `request_id`, the one the response carries (absent when no `request_id` could be read), `detail`, an array of at most 16 strings holding the lower-layer error chain, outermost first, each clipped to at most 1,024 bytes and `stack`, a string of at most 8,192 bytes, present for a panic only and never holding the panic's value. It never holds the text of a request, an argument or an option, and, like every body, it is held to the fixed list of credential shapes of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section; a body that check refuses is dropped whole and the response stands.
- **Written by.** The harness, through the journal's own `Append`, after the response is decided and before it is written. A failed request has no transaction, so the event is not part of one. A failed append changes nothing in the response, its status or its exit code, and adds nothing to standard error.

## Rules

1. **A basis for every event.** `basis` names a durable receipt, a recorded observation or a record at a revision. Typed text, optimistic state and a display's continuity are never a basis. A diagnostic event's basis is the observation of the request that failed.
2. **Deduplicated by source, event and generation.** A consumer treats two lines with the same `source.source_id`, `event_id` and `generation` as one. A vendor's reused handle under a new generation is a new event.
3. **A snapshot is not a replay.** A consumer that starts or recovers establishes current state from the owning packages' records and resumes the journal after the cursor that snapshot names. It never presents the backlog as fresh activity.
4. **Status is evidence, not input.** An event carrying what a worker or a vendor reported is recorded as that source's statement, validated for shape, size and identity and, like every body, for the fixed list of credential shapes of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section, a match refusing the event whole. Nothing executes its content.
5. **Nothing private leaves.** The journal stays in its home. An event's body holds references to records, never a canonical Imperative, a private report, a credential or the Hive Mind's words beyond what its kind's schema allows.
6. **An event changes nothing.** Authority, state and readiness live in records and receipts. A consumer that needs to act reads the record the event points at and acts on that. A diagnostic event is a notice of a failure and never a ground for acting on it.
