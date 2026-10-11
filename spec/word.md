# The Mind's word

`word` is the single decision intake: every question the Hive Mind owes is raised through it, every answer enters through it, and nothing else writes a decision. It keeps no store. It mints the identities of a question, a decision round and a card, validates each answer against the records as they stand, and persists through the Pool: each of its mutating operations is exactly one `hive-pool` mutation. This file is the contract of the `word` package for stage 2.

Owner: `word`, through `hive-word`. Schemas: the operation bodies under [`schemas/ops/`](schemas/ops/) whose names begin `hive-word.`; the records it persists are [`records/pool.question.v1`](schemas/records/pool.question.v1.schema.json) and [`records/pool.decision-round.v1`](schemas/records/pool.decision-round.v1.schema.json), written by `pool` through the four operations of [pool.md](pool.md) named `hold-question`, `revise-question`, `answer-question` and `complete-question`. Proved by `test-word` for the intake's behavior, with a fake of `hive-pool` built from the declared schemas, and by `test-integration` for the real call.

## How word persists through the Pool

- **Records.** A question and a decision round are Pool records with `pool.` kinds, at `pool/records/question/` and `pool/records/decision-round/`. `word` never opens them: it reads them through `hive-pool read` and writes them through the four operations above. The names carry the owner of the file, `pool`; the identities carry the minter, `word`.
- **Receipts.** `word` has no part of the home and writes no receipt. The receipts of a `hive-word` operation are the receipts of the one `hive-pool` request it made, returned in its own response's `receipts` in full. A reader that sees "word's receipt" in the design reads the receipt `hive-word` returned: `pool.question.held`, `pool.question.revised`, `pool.answer.accepted`, `pool.answer.unreconciled`, `pool.answer.superseded`, `pool.question.deferred`, `pool.question.completed`, and `pool.hold.placed` and `pool.hold.released` beside them.
- **Events.** `word` appends no event: it has no transaction. The Pool's `pool.question.*` events report what `word` persisted.
- **Replay.** `word` has no `requests/` store, so its replay is the Pool's. Each `hive-word` mutation sends its one `hive-pool` request with the **same `request_id`** as its own request, the same `correlation_id` and `issued_at`, `caller` `{package, word, hive-word}`, and as `causation` its own request: schema `hive.request`, its `request_id` and the digest of its canonical form. The Pool records that causation with the request, and the recorded digest is the fingerprint of the original `hive-word` request.
- **Replay is verified first.** For every mutation (`hold`, `revise`, `answer`, `complete`), before any check that reads the current state of a record and before any identity is minted, `word` asks `hive-pool read` for kind `request-outcome` with its own `request_id`. If the Pool recorded one, `word` compares the recorded causation digest with the digest of the request in hand. Equal: the request is a retry, and `word` returns the recorded outcome with `replayed` true, adapted as below, without running one check against the present records, so a retry of an answer is never refused because the first answer changed the round. Different: `conflict`, `replay_mismatch`, and nothing is written, so a reused `request_id` with other words never returns the old outcome. None recorded: the checks below run, identities are minted and the Pool request is made.
- **Adapting the outcome.** The recorded response is the Pool's. For `hold`, the body is the Pool's `question`, `round` and `hold` with `question_id`, `decision_round_id`, `card_id` and `hold_id` read from them, the card being the round's card of that question; for `answer` it is the Pool's body, and an outcome whose disposition is `unreconciled` is returned as `held` with the hold in `error.hold` and code `word.question_changed`, as it was when first made; for `revise` and `complete` it is the Pool's body unchanged. The receipts are the recorded ones.
- **Racing retries.** Two identical requests racing past that read are serialized by the Pool: the second mints other identities, differs in digest from the first and is `replay_mismatch` at the Pool, and its caller's retry then finds the outcome and is returned it.
- **Who may persist the word.** `hold-question`, `revise-question`, `answer-question` and `complete-question` refuse at the Pool, with `authority_insufficient`, any request that is not made by the `hive-word` command on a `hive-word` request: the caller names the package `word` and its command `hive-word`, and the `causation` is a `hive-word` request of the same `request_id`. The restriction is the Pool's, not the caller's to apply. It is attribution too, and "What this does not prevent" says so.
- **Errors.** `word` refuses on its own grounds with its own codes, below, before it calls the Pool. A refusal, conflict or hold the Pool returns is passed back unchanged, with the Pool's code.

## No secret value

No question, option, consequence, amount, answer, source, receipt or recorded outcome the decision intake defines holds a secret value. Where one must name a secret, it holds a `secret_ref` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json), the secret's store, its host, the account identity and its purpose, all four and nothing else, which is neither the value nor a transferable authorization and grants nothing to whoever holds it.

- **No field is a place for a value.** A request whose text carries a value that Hive's secret-handling contract recognizes is refused whole, before anything is persisted, and nothing of it is applied.
- **An answer is never redacted or rewritten.** It is refused and given again, with the reference in place of the value.
- **A value found later is a found copy.** A value later found in a record is handled as that contract says, by a sanitized incident that waits for the Hive Mind's word.

How a value is recognized is that contract's and is left undecided below. `test-word` proves that a request with a value where a reference belongs is `invalid`, and that free text without one passes unchanged.

## Operations

### `hold`

Raises a question and holds the item it gates. The request names the held item, or a `new_item` for a question with its own scope, in which case the Pool adds a decision item and holds it; one of the two, never both (`word.held_item_ambiguous`). It carries the question's text, the consequence of leaving it unanswered, the options with the recommendation named first, an `amount` for a spend ask, the origin, and the round to join or open. `word` mints `question_id`, `card_id` and, for a new round, `decision_round_id`, and calls `hive-pool hold-question`, which writes the question at revision 1, the round with the card at revision 1, the `word` hold and the receipts in one transaction. The response returns every identity and record.

- A question prefers to hold the work it gates; a separate decision item is for a question with its own scope.
- A round that is not `complete` is recorded with its `incomplete_reason` and its cards are not presented as answerable until a later `hold` or the owner marks it complete through a new round; nothing in an incomplete round is ruled on.
- A spend ask is an ordinary question with `amount` stated; there is no separate spend path.
- A legacy `word` hold without a question is bound to the question `hold` raises for it: the request names the held item, the Pool finds the active questionless `word` hold and binds it instead of placing a second. How such a hold is shown before that is left to the stage that builds the UI, below.

### `revise`

Writes the next revision of an outstanding question, whole: its text, consequence, options and recommendation, with the `amount` of a spend ask. It exists so that an owner who corrects a question changes it and does not raise a second one. `word` calls `hive-pool revise-question` once; in the one transaction the Pool appends the revision to the question and makes it current, raises the card's presentation revision in its round and sets the card's question revision to the new one, and writes `pool.question.revised`. The question keeps its identity, its round and its `word` hold; nothing is released, no answer changes and no card is answered.

- Only a question with no applied answer can be revised, whether it is `open` or `deferred`: for a question that has one, `pool.question_answered`, `refused`; an applied answer is changed by a superseding answer, as `answer` says. Nothing closes a question without an answer here, and no withdrawal exists.
- An answer given against the earlier revision is recorded unreconciled and the hold stays, as `answer` defines; with this operation that path is reachable.
- It is a mutation like the others: replay is verified first, and the Pool refuses it from anything but `hive-word` on a `hive-word` request.

### `answer`

The one intake for an answer. The request carries the card binding the Hive Mind answered against, with the card, question, held item and decision round each at the revision shown; the option, if one was chosen, with its label as displayed; the free text exactly as typed, which may qualify the option and is always kept whole; or `later` with a revisit date; the `source`; and the time the answer was given.

**What an answer rests on is its source.** The `caller` of the envelope is attribution, as [commands.md](commands.md) says: it authenticates nobody and grants nothing, and an answer is not the Hive Mind's because a caller says so. The source is required, names evidence, is recorded with the answer and is returned by every read, so a reader always sees where the words came from:

| Source | Names | Verified by |
| --- | --- | --- |
| `review-submission` | The review's task, generation and `review_id`, the round and the `feedback_event_id` of the submit event | `hive-word answer`, below |
| `session-input` | The `input_id` and the input receipt of the supervisor session the words were typed into | The stage that builds session input; left undecided below |
| `operator-request` | The `request_id` of this `hive-word answer` request, typed at a terminal | Nothing further: an operator request has no further evidence, and the record says exactly that. Its `request_id` must be the request's own, else `invalid` |

An answer holds an option, or `later`, or text that is not empty. An answer with none of the three is `invalid` and applies nothing; the schemas say so at `hive-word answer`, at `hive-pool answer-question` and in the record, and the Pool enforces it as well as `word`.

`word` first verifies replay (above). It reads the question, round and held item through `hive-pool read` and checks, in order:

| Check | Refused as |
| --- | --- |
| The caller is the operator, a supervisor, or the `ui` package relaying a submission; a worker or a reviewer cannot answer | `refused`, `word.not_authorized`. Attribution only: it stops an honest mistake and proves nothing about who typed |
| For a `review-submission`: the round file, read through the contract library's validated reader, holds the submit event, and that event's submission carries this card's answer with the same option, label, text and `later` | `refused`, `authority_insufficient` when the event is absent or carries other words |
| The card exists in the round and belongs to the question | `not_found` |
| The question revision answered is not the current one | Not refused: the answer is recorded unreconciled, below |
| Otherwise, the `card_revision` is the current one | `conflict`, `word.card_stale`: the card changed what it shows and cannot submit |
| The option, if any, is an option of the question revision answered | `invalid`, `word.option_unknown` |
| The question has an applied answer and `supersedes` does not name it, or the answer is `later` while it has one | `conflict`, `pool.answer_exists`: the Pool decides this inside its answer transaction, so two answers racing past this read cannot both apply; a genuine duplicate is returned before it |
| `supersedes` names an applied answer, and its source is an `operator-request` or the envelope's authority is the Hive Mind's word, which the Pool verifies | `refused`, `word.not_authorized` |

**What stales a card.** A card is stale only when what it showed has changed: the question's revision, or the card's presentation revision. The revisions of the decision round and of the held item are carried in the request and recorded with the answer as what was shown. They change when another card of the round is answered or another hold of the item is released, so they are never grounds to refuse. An answer to one card never makes another card of the same round stale: one review submission answering two cards of one round applies both, each by its own `hive-word answer`, in any order. No multi-answer operation exists.

Then it calls `hive-pool answer-question` once. The Pool records the answer exactly as given, with its source, and applies it in the same transaction:

| Answer | Disposition | What changes | Receipts |
| --- | --- | --- | --- |
| Against the current question revision, no `later` | `released` when the question holds work: the hold is released; `completed` when the question holds a decision item made for it | The card is answered, the round closes when every card is, the question becomes `answered` | `pool.answer.accepted`, and `pool.hold.released` for a release |
| `later` | `deferred` | The question becomes `deferred` with its revisit date; the hold stays; the card stays open | `pool.question.deferred` |
| Against an earlier question revision | `unreconciled` | Recorded, applied to nothing; the hold stays | `pool.answer.unreconciled`; `hive-word` returns `held` with the hold in `error.hold` and code `word.question_changed` |
| With `reconciles` naming an unreconciled answer, against the current revision | as the first row | The earlier answer is marked reconciled by this one | as the first row |
| With `supersedes` naming an applied answer | as the first row | The earlier answer is marked `superseded`; a released hold is not re-placed, and the owner re-holds if the new word requires it | `pool.answer.superseded` beside `pool.answer.accepted` |

- **Idempotent by source within a question.** Two answers to the same question with the same `source` are one answer: the same review submit event, the same session input, or the same operator request, which is the same `request_id`. The Pool returns the first's receipts with `duplicate` true and writes nothing. The key is the question and the source together: one submit event or one session input that answers two questions is two answers, each applied once, and answering the second never returns the first's receipt.
- **One applied answer.** A question has at most one applied answer. A further answer, whether it releases, completes or defers, is refused with `pool.answer_exists` unless it names the applied answer in `supersedes`, which marks that answer superseded as the table above says; and `later` never supersedes. An answer given after a deferral meets no applied answer and is not affected; an answer against an earlier revision is recorded unreconciled and applies nothing, so it never conflicts. A replay of the same `hive-word` request is the Pool's replay. Two separate operator requests have two `request_id`s and so two sources: an operator who chose Later and answers again under a new request is recorded and applied, never returned as a duplicate of the deferral.
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

## What this does not prevent

A process running as the same user that ignores its instructions can call `hive-word answer`, or the Pool's operations through a forged caller and causation, and claim an operator request as its source. The record then shows an answer whose source is an operator request, and nothing in the store prevents it: the home is private to one user, the caller authenticates nobody, and an operator request has no evidence beyond itself. A review submission can be checked against its round file and a session input against its receipt, which is why the source is kept and shown; an operator request cannot. Whether an answer is authenticated as the Hive Mind's against other processes of the same user, and by what, is the Hive Mind's ruling, left undecided below.

## Error codes

| Code | Status | Meaning |
| --- | --- | --- |
| `word.not_authorized` | `refused` | The caller cannot answer or supersede. |
| `authority_insufficient` | `refused` | A review submission's source is absent from its round file or carries other words, or the request to persist the word was not made by `hive-word` on a `hive-word` request. The common code of [commands.md](commands.md#common-error-codes). |
| `word.held_item_ambiguous` | `invalid` | Both a held item and a new item were given, or neither. |
| `pool.answer_exists` | `conflict` | The question already has an applied answer and the answer does not supersede it. Pool code of [pool.md](pool.md#error-codes). |
| `pool.question_answered` | `refused` | A question with an applied answer cannot be revised. Pool code. |
| `word.card_stale` | `conflict` | The card's presentation revision is no longer current. |
| `word.option_unknown` | `invalid` | The option is not one of the question revision answered. |
| `word.question_changed` | `held` | The answer was given against an earlier revision and awaits reconciliation; `error.hold` names the hold. |
| `word.hold_unresolved` | `refused` | The question has no applied answer. |
| `word.round_incomplete` | `refused` | The card belongs to a round that was not collected whole. |

## What `test-word` proves

With a fake `hive-pool` built from the declared schemas: a round of several cards answered to completion and closed; an option qualified by later free text kept as one answer; free text alone as a complete answer; `later` recorded as a deferral with the hold intact; a stale card refused; one review submission answering two cards of one round with both applied, whichever is sent first; an answer holding no option, no `later` and no text `invalid` and applying nothing; an answer against a changed question held for reconciliation and settled by a reconciling answer; a worker's answer refused; a review submission whose event is absent or carries other words refused with `authority_insufficient`; two answers with one source deduplicated; an operator who chose Later answering again under a new request recorded and applied, not returned as a duplicate; a revised question keeping its identity, round and hold with the card's presentation revision raised, a late answer against the old revision recorded unreconciled and the hold intact, a revision refused for an answered question; a second answer to an answered question refused unless it supersedes, and a `later` never superseding; one submit event answering two questions with each applied once, not returned as a duplicate of the other; a retry of a `hold`, a `revise`, an `answer` and a `complete` request returning the recorded outcome with no second set of identities and no check against the changed records, even after the first answer changed the round; a reused `request_id` with other words refused with `replay_mismatch`; a request with a value where a reference belongs `invalid`, and free text without one passing unchanged; `complete` refused without an answer; and `verify` reading the record for each subject kind, including a superseded answer. `test-integration` proves the real `hive-word` to `hive-pool` call, each exit state the Pool can return through `word`, replay through both, and a record of 2,097,152 bytes crossing whole.

## Decisions taken here

Each decision is followed by the alternative it rejected.

1. The decision records are Pool records with `pool.` kinds; `word` mints their identities and writes nothing; the alternative, `word.` kinds written by `pool`, breaks the rule that a package never writes a name that begins with another package's, and `word` has no part of the home to write a receipt in.
2. `word`'s replay is the Pool's, by the shared `request_id`, verified against the digest of the original `hive-word` request, which the Pool records as the request's causation, before any check of the present records and before any identity is minted; a replay store in `word` would be the store the design says it does not keep, and a replay that skipped the digest would return the old outcome for other words.
3. A card is an entry of the decision round, with its own revision, not a record of its own; a `pool.card` record would be one more file per question with no reader that needs it apart from the round.
4. Answers are deduplicated by their question and their source, the evidence that carries the Hive Mind's words, and not only by request identity; deduplication by request identity alone would record a resent review submission twice.
5. What an answer rests on is its source, recorded with it, and the Pool refuses to persist the word from anything but `hive-word`; the contracts give no authentication of the Hive Mind against another process of the same user, and none is invented here: the alternative, treating the `caller` as proof, would present attribution as authority.
6. An answer to a changed question is recorded and returned as `held`, so that no word of the Hive Mind is lost and none is applied to a question it was not given to; refusing it would lose the words, and applying it would answer a question it was not given to.

## Left undecided

| Open point | What settles it |
| --- | --- |
| Which `source` kinds the UI's Waiting on you submits under, beyond a review submission | Stage 6, when the card component is built; `operator-request` serves the CLI now |
| Verification of a `session-input` source against the input receipt of the supervisor session | The stage that builds session input, stage 4's session adapter, which defines the receipt's reader; until then the source is recorded and shown unverified |
| Whether an answer is authenticated as the Hive Mind's against other processes of the same user, and by what | The Hive Mind's ruling |
| How a value is recognized in free text, so that the request is refused whole before anything is persisted | Hive's secret-handling contract, which is being written into the design now and changes the record and command contracts by their own rule |
| Whether a supervisor may supersede an answer under a recorded posture | Stage 4, which defines postures; the operator and the Hive Mind's word suffice now |
| How a question that has become moot is closed, and on whose word | Settled with the decision intake's build, which waits on the operator's ruling on proof of presence; no withdrawal exists until then |
| How an imported `word` hold with no question is shown | The stage that builds the UI, in its review; the default it starts from is that it is shown as a hold on its item and becomes a card only when a question is raised for it |
