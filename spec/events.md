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

A kind is the owning package's name, a subject and a fact in the past tense: `dispatch.launch.accepted`, `synapse.readiness.invalidated`. A kind whose content needs more than the envelope has a body schema at `spec/schemas/events/<kind>.v<major>.schema.json`; a kind that registers none has an empty body. Stage 1 registers no event kind beyond the journal's own, below, whose body schema is [`events/hive.journal.tail-repaired.v1`](schemas/events/hive.journal.tail-repaired.v1.schema.json). Each stage registers its kinds as it builds them.

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
- **Appended inside the transaction.** An event is listed in its transaction's intent and appended after the records and receipts it reports. Recovery appends an event the intent names only if no line with that `event_id` is already present.

Reading the journal is open to every package of the same home, through the contract library's reader and never by opening the files directly: the journal is the home's shared notice board, and holds nothing that is not already recorded by its owner. A reader keeps a cursor, the last `home_sequence` it handled, and resumes after it. No package reads the journal of another home; a Fleet's events reach its parent only as the summary the Fleet writes, as the design requires.

## Rules

1. **A basis for every event.** `basis` names a durable receipt, a recorded observation or a record at a revision. Typed text, optimistic state and a display's continuity are never a basis.
2. **Deduplicated by source, event and generation.** A consumer treats two lines with the same `source.source_id`, `event_id` and `generation` as one. A vendor's reused handle under a new generation is a new event.
3. **A snapshot is not a replay.** A consumer that starts or recovers establishes current state from the owning packages' records and resumes the journal after the cursor that snapshot names. It never presents the backlog as fresh activity.
4. **Status is evidence, not input.** An event carrying what a worker or a vendor reported is recorded as that source's statement, validated for shape, size and identity and, like every body, for the fixed list of credential shapes of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section, a match refusing the event whole. Nothing executes its content.
5. **Nothing private leaves.** The journal stays in its home. An event's body holds references to records, never a canonical Imperative, a private report, a credential or the Hive Mind's words beyond what its kind's schema allows.
6. **An event changes nothing.** Authority, state and readiness live in records and receipts. A consumer that needs to act reads the record the event points at and acts on that.
