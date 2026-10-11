# The Mind's word

`word` is the single decision intake: every question the Hive Mind owes is raised through it, every answer enters through it, and nothing else writes a decision. It keeps no store. It mints the identities of a question, a decision round and a card, validates each answer against the records as they stand, and persists through the Pool: each of its mutating operations is exactly one `hive-pool` mutation. This file is the contract of the `word` package for stage 2.

Owner: `word`, through `hive-word`. Schemas: the operation bodies under [`schemas/ops/`](schemas/ops/) whose names begin `hive-word.`; the records it persists are [`records/pool.question.v1`](schemas/records/pool.question.v1.schema.json) and [`records/pool.decision-round.v1`](schemas/records/pool.decision-round.v1.schema.json), written by `pool` through the three operations of [pool.md](pool.md) named `hold-question`, `answer-question` and `complete-question`. Proved by `test-word` for the intake's behavior, with a fake of `hive-pool` built from the declared schemas, and by `test-integration` for the real call.

## How word persists through the Pool

- **Records.** A question and a decision round are Pool records with `pool.` kinds, at `pool/records/question/` and `pool/records/decision-round/`. `word` never opens them: it reads them through `hive-pool read` and writes them through the three operations above. The names carry the owner of the file, `pool`; the identities carry the minter, `word`.
- **Receipts.** `word` has no part of the home and writes no receipt. The receipts of a `hive-word` operation are the receipts of the one `hive-pool` request it made, returned in its own response's `receipts` in full. A reader that sees "word's receipt" in the design reads the receipt `hive-word` returned: `pool.question.held`, `pool.answer.accepted`, `pool.answer.unreconciled`, `pool.answer.superseded`, `pool.question.deferred`, `pool.question.completed`, and `pool.hold.placed` and `pool.hold.released` beside them.
- **Events.** `word` appends no event: it has no transaction. The Pool's `pool.question.*` events report what `word` persisted.
- **Replay.** `word` has no `requests/` store, so its replay is the Pool's. Each `hive-word` mutation sends its one `hive-pool` request with the **same `request_id`** as its own request, the same `correlation_id` and `issued_at`, its own request as `causation`, and `caller` `{package, word, hive-word}`. A retry of the same `hive-word` request therefore reaches the Pool as the same request and is replayed by it; `hive-word` returns the Pool's recorded outcome with `replayed` true. Before an operation that mints identities, `word` first asks `hive-pool read` for kind `request-outcome` with its own `request_id`, and returns the recorded outcome if there is one, so that a retry never mints a second set of identities. Two identical requests racing past that read are serialized by the Pool: the second is `replay_mismatch`, and its caller's retry then finds the outcome.
- **Errors.** `word` refuses on its own grounds with its own codes, below, before it calls the Pool. A refusal, conflict or hold the Pool returns is passed back unchanged, with the Pool's code.

## Operations

### `hold`

Raises a question and holds the item it gates. The request names the held item, or a `new_item` for a question with its own scope, in which case the Pool adds a decision item and holds it; one of the two, never both (`word.held_item_ambiguous`). It carries the question's text, the consequence of leaving it unanswered, the options with the recommendation named first, an `amount` for a spend ask, the origin, and the round to join or open. `word` mints `question_id`, `card_id` and, for a new round, `decision_round_id`, and calls `hive-pool hold-question`, which writes the question at revision 1, the round with the card at revision 1, the `word` hold and the receipts in one transaction. The response returns every identity and record.

- A question prefers to hold the work it gates; a separate decision item is for a question with its own scope.
- A round that is not `complete` is recorded with its `incomplete_reason` and its cards are not presented as answerable until a later `hold` or the owner marks it complete through a new round; nothing in an incomplete round is ruled on.
- A spend ask is an ordinary question with `amount` stated; there is no separate spend path.
- A legacy `word` hold without a question is bound to the question `hold` raises for it: the request names the held item, the Pool finds the active questionless `word` hold and binds it instead of placing a second.

### `answer`

The one intake for an answer. The request carries the card binding the Hive Mind answered against, with the card, question, held item and decision round each at the revision shown; the option, if one was chosen, with its label as displayed; the free text exactly as typed, which may qualify the option and is always kept whole; or `later` with a revisit date; the `source` that carries the words; and the time the answer was given.

`word` reads the question, round and held item through `hive-pool read` and checks, in order:

| Check | Refused as |
| --- | --- |
| The caller is the operator, a supervisor, or the `ui` package relaying a submission; a worker or a reviewer cannot answer | `refused`, `word.not_authorized` |
| The card exists in the round and belongs to the question | `not_found` |
| `card_revision`, `decision_round_revision` and `held_task_revision` are the current ones | `conflict`, `word.card_stale`: the card is stale and cannot submit |
| The option, if any, is an option of the question revision answered | `invalid`, `word.option_unknown` |
| `supersedes` names an applied answer, and the envelope's authority is the Hive Mind's word or the caller is the operator | `refused`, `word.not_authorized` |

Then it calls `hive-pool answer-question` once. The Pool records the answer exactly as given, with its source, and applies it in the same transaction:

| Answer | Disposition | What changes | Receipts |
| --- | --- | --- | --- |
| Against the current question revision, no `later` | `released` when the question holds work: the hold is released; `completed` when the question holds a decision item made for it | The card is answered, the round closes when every card is, the question becomes `answered` | `pool.answer.accepted`, and `pool.hold.released` for a release |
| `later` | `deferred` | The question becomes `deferred` with its revisit date; the hold stays; the card stays open | `pool.question.deferred` |
| Against an earlier question revision | `unreconciled` | Recorded, applied to nothing; the hold stays | `pool.answer.unreconciled`; `hive-word` returns `held` with the hold in `error.hold` and code `word.question_changed` |
| With `reconciles` naming an unreconciled answer, against the current revision | as the first row | The earlier answer is marked reconciled by this one | as the first row |
| With `supersedes` naming an applied answer | as the first row | The earlier answer is marked `superseded`; a released hold is not re-placed, and the owner re-holds if the new word requires it | `pool.answer.superseded` beside `pool.answer.accepted` |

- **Idempotent by source.** Two answers with the same `source` (the same review submit event, the same session input, or the same operator request) are one answer: the Pool returns the first's receipts with `duplicate` true and writes nothing. A replay of the same `hive-word` request is the Pool's replay.
- **One answer, exactly.** An option and the text typed with it are one answer; an option is never applied ahead of its text; text with no option is a complete answer. `word` never rewrites, trims or summarizes a word of it.
- **A deferral is not approval.** `later` leaves the work held, lists the revisit date for the Shadow and the Scheduled view, and reaching the date re-presents the card and runs nothing.
- **Acceptance is not release.** The response says which disposition the answer got; a reader shows the acceptance of the answer and the release or completion of the hold as two facts, each with its receipt.

### `complete`

Records that an answered question needs nothing further: its consequence was carried out, or the owner closes it in its decision inventory, with evidence where there is any. It requires an applied answer (`released` or `completed`); otherwise it is `refused` with `word.hold_unresolved`. Completing a Lictor report, a review round or a card round never completes a question: each has its own receipt, and none closes the others by inference. One `hive-pool complete-question` call; receipt `pool.question.completed`.

### `verify`

Read-only. It answers whether the Mind's word stands, from the record and never from the reference:

| Subject | `stands` when |
| --- | --- |
| An `authority` of kind `word` | The reference names a question of this home with an applied, unsuperseded answer, in the envelope's scope |
| A `task` | No active `word` hold on it is unresolved: every such hold's question has an applied answer |
| A `hold` | It is released |
| A `question` | It has an applied answer |

The response lists the reasons when the word does not stand (unanswered, deferred, unreconciled, superseded, completed without an answer, out of scope, not found, malformed) and returns the holds and questions it read. An `authority` of another kind is `invalid` here: `verify` reads the Pool and nothing else. Assimilation at stage 5 and admission's `approve` call it or read the same record; a reference is never itself authority.

## Error codes

| Code | Status | Meaning |
| --- | --- | --- |
| `word.not_authorized` | `refused` | The caller cannot answer or supersede. |
| `word.held_item_ambiguous` | `invalid` | Both a held item and a new item were given, or neither. |
| `word.card_stale` | `conflict` | A revision the card was shown against is no longer current. |
| `word.option_unknown` | `invalid` | The option is not one of the question revision answered. |
| `word.question_changed` | `held` | The answer was given against an earlier revision and awaits reconciliation; `error.hold` names the hold. |
| `word.hold_unresolved` | `refused` | The question has no applied answer. |
| `word.round_incomplete` | `refused` | The card belongs to a round that was not collected whole. |

## What `test-word` proves

With a fake `hive-pool` built from the declared schemas: a round of several cards answered to completion and closed; an option qualified by later free text kept as one answer; free text alone as a complete answer; `later` recorded as a deferral with the hold intact; a stale card refused; an answer against a changed question held for reconciliation and settled by a reconciling answer; a worker's answer refused; two answers with one source deduplicated; a retry of a `hold` request returning the recorded outcome without a second set of identities; `complete` refused without an answer; and `verify` reading the record for each subject kind, including a superseded answer. `test-integration` proves the real `hive-word` to `hive-pool` call, each exit state the Pool can return through `word`, replay through both, and a record of 2,097,152 bytes crossing whole.

## Decisions taken here

1. The decision records are Pool records with `pool.` kinds; `word` mints their identities and writes nothing. The alternative, `word.` kinds written by `pool`, breaks the rule that a package never writes a name that begins with another package's, and `word` has no part of the home to write a receipt in.
2. `word`'s replay is the Pool's, by the shared `request_id`, with a read of the recorded outcome before any identity is minted.
3. A card is an entry of the decision round, with its own revision, not a record of its own.
4. Answers are deduplicated by their source, the evidence that carries the Hive Mind's words, and not only by request identity.
5. An answer to a changed question is recorded and returned as `held`, so that no word of the Hive Mind is lost and none is applied to a question it was not given to.

## Left undecided

| Open point | What settles it |
| --- | --- |
| Which `source` kinds the UI's Waiting on you submits under, beyond a review submission | Stage 6, when the card component is built; `operator` serves the CLI now |
| Whether a supervisor may supersede an answer under a recorded posture | Stage 4, which defines postures; the operator and the Hive Mind's word suffice now |
