# The Mind's word

`word` is the single decision intake: every question the Hive Mind owes is raised through it, every answer enters through it, and nothing else writes a decision. It keeps no store. It mints the identities of a question, a decision round and a card, checks who brings each answer and the evidence it rests on, and persists through the Pool: each of its mutating operations is exactly one `hive-pool` request. This file is the contract of the `word` package for stage 2 of the [ordered implementation plan](../docs/design.md#ordered-implementation-plan), and of the Pool records and operations it persists through.

Owner: `word`, through `hive-word`, for the intake; `pool`, through `hive-pool`, for the records and the operations that write them. Schemas: the operation bodies under [`schemas/ops/`](schemas/ops/) whose names begin `hive-word.`; the records [`records/pool.question.v1`](schemas/records/pool.question.v1.schema.json) and [`records/pool.decision-round.v1`](schemas/records/pool.decision-round.v1.schema.json); and the bodies of the six `hive-pool` operations below whose names end `-question`. Proved by `test-word` for the intake's behavior, with a fake of `hive-pool` built from the declared schemas; by `test-pool` for the records and the six operations; and by `test-integration` for the real call.

An answer keeps its recorded source and nothing more is asked of it: Hive asks no proof of presence for an answer, and no record here carries one, as the design's [Complete review rounds and answers](../docs/design.md#complete-review-rounds-and-answers) states.

## How word persists through the Pool

- **Records.** A question and a decision round are Pool records with `pool.` kinds, at `pool/records/question/<question_id>.json` and `pool/records/decision-round/<decision_round_id>.json`. `word` never opens them: it reads them through `hive-pool read` and writes them through the six operations of [Operations](#operations). The names carry the owner of the file, `pool`; the identities carry the minter, `word`, as [identities.md](identities.md#identifier-form) registers `q_`, `dr_` and `card_`. A card is an entry of its round, and a question has exactly one card.
- **Receipts.** `word` has no part of the home and writes no receipt. The receipts of a `hive-word` operation are the receipts of the one `hive-pool` request it made, returned in its own response's `receipts` in full. A reader that sees "`word`'s receipt" in the design reads the receipt `hive-word` returned.
- **Events.** `word` appends no event: it has no transaction. The Pool's `pool.question.*` events report what `word` persisted.
- **Replay is a handoff.** `word` has no `requests/` store, so its replay is the Pool's, by the rule [commands.md](commands.md#a-command-with-no-store) gives a command with no store. After its request is validated and its home, scope and caller are checked, and before any check of the records and before any identity is minted, `word` reads the Pool's recorded outcome of its own `request_id` through `hive-pool read` of kind `request-outcome`, which recovers an interrupted transaction first, and compares the digest recorded there with the digest of the request in hand. Equal: the request is a retry, and `word` returns the recorded outcome, adapted as below, with `replayed` true, without one check against the present records, so a retry of an answer is never refused because the first answer changed the round. Different: `conflict`, `replay_mismatch`, and nothing is sent. None recorded: an answer's source is checked, identities are minted and `word` sends its one `hive-pool` request as a handoff, under its own `request_id`, `correlation_id` and scope, with `caller` the package `word` and its command `hive-word`, and as `causation` its own request: schema `hive.request`, its `request_id` and the digest of its canonical form. The Pool keys that request's replay on the causation's digest, so two identical `hive-word` requests that race past the read both receive the outcome of whichever reached the Pool first, with that one's identities.
- **Adapting the outcome.** The recorded response is the Pool's. For `hold`, the body is the Pool's `question`, `round`, `hold` and `item`, with `question_id`, `decision_round_id`, `card_id` and `hold_id` read from them; for `answer`, the Pool's body, except that an outcome whose disposition is `unreconciled` is returned as `held`, with no body, the hold in `error.hold` and code `word.question_changed`; for every other operation, the Pool's body unchanged. The receipts are the recorded ones, and a refusal, conflict or hold the Pool returned is returned unchanged, with the Pool's code.
- **Who may persist the word.** The six operations refuse at the Pool, with `authority_insufficient`, any request that is not a handoff from `hive-word`: the caller names the package `word` and its command `hive-word`, and the `causation` is a `hive.request` of the request's own `request_id` with its digest. The restriction is the Pool's and not the caller's to apply. It is attribution, as [What this does not prevent](#what-this-does-not-prevent) says, and not authentication.
- **Whose checks.** Every check that reads a question, a round, a hold or an item as they stand is the Pool's, made inside the transaction that writes, under its lock, so that no two requests racing past a read can both apply. `word`'s own checks are the two that need nothing of the Pool's: who brings the request, and for a review submission that the round file carries the words. A refusal of `word`'s own comes before any call, with its own code.

## No secret value

No question, option, consequence, amount, note, reason, answer, source, receipt or recorded outcome the decision intake defines holds a secret value. Where one must name a secret, it holds a `secret_ref` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json), the identifier of the secret's Spoor and nothing else, which is neither the value nor a transferable authorization and grants nothing to whoever holds it.

- **No field is a place for a value.** A `hive-word` request whose text holds a string of a credential shape on the fixed list of [records.md](records.md#encoding) is refused whole, `invalid` with `secret_shape_recognized`, by the owner's no-value check of the contract library before anything is sent to the Pool, and nothing of it is applied. The Pool's writer runs the same check on its own request.
- **An answer is never redacted or rewritten.** It is refused and given again, with the reference in place of the value.
- **A value found later is a found copy,** handled as the design's [What happens when a copy is found where none may be](../docs/design.md#what-happens-when-a-copy-is-found-where-none-may-be) says, by a sanitized incident raised through `hive-word hold`.

## A question's life

A question holds one item through one `word` hold: the work it gates, which a question prefers, or a decision item the Pool adds for a question with its own scope. Its states:

| State | Means | The hold | Its card |
| --- | --- | --- | --- |
| `open` | Awaiting the Hive Mind. | Active | `open` |
| `deferred` | The Hive Mind chose Later; still awaiting, with a revisit date. | Active | `open` |
| `answered` | An answer is accepted and applied; its disposition is the owner's, and pending. | Active | `answered` |
| `released` | The owner released the hold on the applied answer: the work may proceed as the answer directs. | Released | `answered` |
| `completed` | The owner completed the question on the applied answer, with no work released by it. | Released | `answered` |
| `moot` | Closed without an answer, on evidence that nothing is left for it to gate. | Released | `moot` |

| From | To | Through |
| --- | --- | --- |
| (none) | `open` | `hold` |
| `open`, `deferred` | `deferred` | `answer` with Later, against the current revision |
| `open`, `deferred` | `answered` | `answer` without Later, against the current revision |
| `answered` | `answered` | `answer` that supersedes the applied answer |
| `answered` | `released` | `release` |
| `answered` | `completed` | `complete` |
| `open`, `deferred` | `moot` | `close` |

- **Acceptance is not release.** An answer is recorded and applied in one transaction, and it never releases the hold: the owning supervisor reads the exact words and records their disposition, `release` or `complete`, with a disposition note of its own, which the card shows beside the Hive Mind's words. Until then the hold stays and the held work waits on its supervisor, as the design's [Progress in real time](../docs/design.md#progress-in-real-time) reads it. A reader shows the acceptance and the disposition as two facts, each with its receipt.
- **A deferral is not approval.** Later leaves the work held and records a revisit date, which the Shadow reads and Scheduled lists; reaching it re-presents the card and runs nothing.
- **Released, completed and moot are final.** Such a question takes no further answer at its current revision, no revision, no disposition and no close. A later change of the Hive Mind's word on that matter is a new question, raised on the work it concerns.
- **Nothing is deleted.** No operation removes a question or its round: archiving the held item, the round's review or the report that raised it keeps them whole, with every answer and receipt, and how long they are kept beyond that is the Pool's retention configuration, as [pool.md](pool.md#left-undecided) leaves it.

A round is `open` while any of its cards is `open`, and `closed` from the moment none is; a closed round takes no new card and never reopens. An unreconciled answer changes no state of the question, the card or the round.

A review's [final record](review.md#finaljson) reads each card's decision in these states: `pending` for a question open or deferred, `accepted` for one answered, `released`, `completed`, and `superseded` for one closed as moot, each with the receipt that establishes it.

## Operations

Each mutating operation is one handoff to one `hive-pool` operation, which does all of what follows in one transaction and writes the receipts named, with the Pool's events that [pool.md](pool.md#receipts-and-events) lists for them. Who may send each, by the envelope's `caller`, which is attribution and never authentication:

| `hive-word` | `hive-pool` | Caller accepted | Receipts |
| --- | --- | --- | --- |
| `hold` | `hold-question` | a supervisor, the operator, or a package other than `ui` | `pool.question.held`; `pool.hold.placed` unless a questionless hold is bound; `pool.item.added` for a decision item |
| `revise` | `revise-question` | a supervisor, the operator, or a package other than `ui` | `pool.question.revised` |
| `answer` | `answer-question` | a supervisor or the operator | `pool.answer.accepted`, with `pool.answer.superseded` when it supersedes; `pool.question.deferred`; or `pool.answer.unreconciled` |
| `release` | `release-question` | a supervisor or the operator | `pool.question.released`, `pool.hold.released` |
| `complete` | `complete-question` | a supervisor or the operator | `pool.question.completed`, `pool.hold.released` |
| `close` | `close-question` | a supervisor or the operator | `pool.question.mooted`, `pool.hold.released` |

Any other caller, a worker and the package `ui` always among them, is refused with `word.not_authorized` before anything is sent. A review card's answer reaches `word` only from the owning supervisor, which relays a submission after it has collected the complete submitted round, as [review.md](review.md) says. The check stops an honest mistake and proves nothing about who typed. `revise`, `release`, `complete` and `close` pass on the envelope's `expected_revisions`, which name the question's record revision the caller read; a different revision at the Pool is `revision_conflict`.

### `hold`

Raises a question and holds the item it gates. The request names the held item, or a `new_item` for a question with its own scope, never both; carries the question's text, the consequence of leaving it unanswered, the options with the recommendation, the `amount` of a spend ask and the origin; and names the round to join or asks for a new one. `word` mints `question_id`, `card_id` and, for a new round, `decision_round_id`, and sends `hive-pool hold-question` with the request's `issued_at` as the question's `asked_at`. The Pool:

- refuses a held item that does not exist (`not_found`), lies outside the request's scope (`scope_mismatch`) or is archived (`pool.held_item_archived`); options whose identities repeat or a recommendation that is not one of them (`pool.option_unknown`); and an identity that already exists (`already_exists`);
- for a `new_item`, adds a decision item in the request's scope: its title and body, `deliverable` `decision`, no level, `definition` `not-required`, queued;
- binds the question to the held item's active `word` hold that no question names, the earliest placed where there are several, as a legacy import leaves them; otherwise places a new `word` hold with the hold reason given, or the question's text;
- writes the question at revision 1, `open`, with the item it holds marked `work` or `decision-item`, which records where the item came from and is never read by a disposition;
- opens the round with its origin, the question's when none is given, its attachment manifest and whether it was collected whole; or adds the card to an existing round of the request's scope that is `open` (`pool.round_closed` otherwise) and complete (`pool.round_incomplete` otherwise). The card enters at presentation revision 1, showing question revision 1 and the held item's revision at that moment.

A question prefers to hold the work it gates; a separate decision item is for a question with its own scope. A spend ask is an ordinary question with `amount` stated; there is no separate spend path. A round that was not collected whole is recorded with its `incomplete_reason`: nothing in it is answered, no card joins it, and its owner asks its questions again in a new, complete round and closes the old ones as moot, as [`close`](#close) says. An imported `word` hold stays shown as a hold with no question until a question is bound to it, as the design's [Decision cards](../docs/design.md#decision-cards) states.

### `revise`

Writes the next revision of an `open` or `deferred` question, whole: its text, consequence, options and recommendation, with the `amount` of a spend ask, so that an owner who corrects a question changes it and does not raise a second one. In `hive-pool revise-question` the Pool appends the revision and makes it current, raises the card's presentation revision by one and sets the card's question revision to the new one. The question keeps its identity, its round, its hold and its state, a deferral with its revisit date included; nothing is released and no answer changes.

- A question with an applied answer is not revised (`pool.question_answered`): its answer is changed by a superseding answer while its disposition is pending. A question released, completed or moot is final (`pool.question_closed`).
- An answer given against the earlier revision is recorded unreconciled, as [`answer`](#answer) defines; with this operation that path is reachable.

### `answer`

The one intake for an answer. The request carries the card binding the Hive Mind answered against, with the card, question, held item and decision round each at the revision shown; the option, if one was chosen, with its label as displayed; the free text exactly as typed, which may qualify the option and is always kept whole; or Later with a revisit date; the `source`; the time the answer was given; and, where it applies, the applied answer it `supersedes` or the unreconciled answer it `reconciles`. An answer holds an option, or Later, or text that is not empty; one with none of the three is `invalid` and applies nothing, and the schemas of the request, of `hive-pool answer-question` and of the record each say so.

**What an answer rests on is its source.** The `caller` is attribution, as [commands.md](commands.md#the-request) says: it authenticates nobody and grants nothing, and an answer is not the Hive Mind's because a caller says so. The source is required, names evidence, is recorded with the answer and is returned by every read, so a reader always sees where the words came from:

| Source | Names | Checked by `word` |
| --- | --- | --- |
| `review-submission` | The review's task, generation and `review_id`, the round and the `feedback_event_id` of the submit event | The round file, read through the contract library's validated reader, holds that submit event, and its submission carries this card's answer with the same binding, option, label, text and Later; otherwise `authority_insufficient` |
| `session-input` | The `input_id` and the input receipt of the supervisor session the words were typed into | Nothing yet: it is recorded and shown unverified until the stage that builds session input, as [Left undecided](#left-undecided) says |
| `operator-request` | The `request_id` of this `hive-word answer` request, typed at a terminal | That it is the request's own `request_id`, else `invalid`, and that the caller is the operator, else `word.not_authorized`. An operator request has no further evidence, and the record says exactly that |

Then it sends `hive-pool answer-question` once. The Pool reads the question, its card and round, and checks in order; the first that applies decides:

| Check | Result |
| --- | --- |
| The binding names a question of this home, its one card, its round, the item it holds and a revision the question has | `not_found` otherwise |
| An answer to this question with the same source is recorded | The first answer's receipts, with `duplicate` true; nothing is written |
| The round was collected whole | `refused`, `pool.round_incomplete` otherwise |
| The option, if any, is an option of the revision answered | `invalid`, `pool.option_unknown` otherwise |
| The revision answered is not the current one | Recorded unreconciled, below, in any state |
| The question is released, completed or moot | `refused`, `pool.question_closed` |
| The binding's card revision is the card's | `conflict`, `pool.card_stale` otherwise: the card changed what it shows and cannot submit |
| The question has an applied answer: the answer is not Later and `supersedes` names that answer | `conflict`, `pool.answer_exists` otherwise |
| `supersedes` is given while no answer is applied, or `reconciles` names an answer that is not unreconciled | `invalid`, `pool.answer_reference` |

**What stales a card.** A card is stale only when what it showed has changed: the question's revision, or the card's presentation revision. The revisions of the decision round and of the held item are carried in the binding and recorded with the answer as what was shown. They change when another card of the round is answered or another hold of the item is released, so they are never grounds to refuse. An answer to one card never makes another card of the same round stale: one review submission answering two cards of one round applies both, each by its own `hive-word answer`, in either order. No multi-answer operation exists.

The Pool records the answer exactly as given, with its source, the caller that brought it and the question's record revision that records it, and applies it in the same transaction:

| Answer | Disposition | What changes | Receipt |
| --- | --- | --- | --- |
| Against the current revision, no Later | `accepted` | The question becomes `answered` and stands on it; its card becomes `answered` with the answer's sequence; the round closes when no card of it is open; a deferral ends. The hold stays. | `pool.answer.accepted` |
| Against the current revision, with `supersedes` naming the applied answer | `accepted` | The question stands on this answer instead, its disposition still pending; the card names it. The superseded answer stays as written. | `pool.answer.accepted` and `pool.answer.superseded` |
| Later against the current revision, no answer applied | `deferred` | The question becomes `deferred`, with this answer's revisit date; the hold stays and the card stays open | `pool.question.deferred` |
| Against an earlier revision, Later or not | `unreconciled` | Recorded exactly as submitted, Later and its revisit date included, and applied to nothing: no state, deferral, card or hold changes. `hive-word` returns `held` with the hold in `error.hold` and code `word.question_changed` | `pool.answer.unreconciled` |

- **Every word is kept.** An answer entry is never changed once written. Whether an answer was superseded is read from the later answer that names it in `supersedes`; whether it was reconciled, from the later answer that names it in `reconciles`. An unreconciled answer awaits reconciliation until an answer with a greater sequence is accepted or deferred, which the Hive Mind gave against the revision now asked; `reconciles` records that the Hive Mind settled it with that earlier answer in view.
- **Idempotent by source within a question.** Two answers to the same question with the same source are one answer: the same review submit event, the same session input, or the same operator request, which is the same `request_id`. The key is the question and the source together: one submit event or one session input that answers two questions is two answers, each applied once, and answering the second never returns the first's receipts. Two separate operator requests have two `request_id`s and so two sources: an operator who chose Later and answers again under a new request is recorded and applied, never returned as a duplicate of the deferral.
- **One applied answer.** A question stands on at most one answer. While its disposition is pending, a further answer replaces it only by naming it in `supersedes`, and Later never supersedes: a deferral is not offered for a question already answered. An answer after a deferral meets no applied answer and needs none. A replay of the same `hive-word` request is the Pool's replay.
- **One answer, exactly.** An option and the text typed with it are one answer; an option is never applied ahead of its text; text with no option is a complete answer. `word` never rewrites, trims or summarizes a word of it.

### `release` and `complete`

The owner's disposition of an `answered` question: the owning supervisor, or the operator at a terminal, reads the Hive Mind's exact words and records what it did on them, with a `note` in its own words and, where it names a record, its `evidence`. Neither supplies an answer: each is refused for a question with no applied answer (`pool.question_unanswered`), and for one already released, completed or moot (`pool.question_closed`).

- **`release`** records the disposition `released` and releases the `word` hold, with the release's receipt as the hold's `released_by`: the work it gated may proceed as the answer directs. A question whose held item, as it stands in the Pool's transaction, is a decision item (deliverable `decision`) gates no work and is completed instead (`pool.decision_item`); an item made as a decision item and since given work is work. Where the answer asks for a change before the work proceeds, the owner revises the item or its Imperative first and names the revision in the evidence.
- **`complete`** records the disposition `completed`: the question is closed and releases no work. Its hold is released in the same transaction only where nothing is left for it to gate: the held item, as it stands in the Pool's transaction, is a decision item (deliverable `decision`), or the held item and every item beneath it are archived. A question whose held work is live is refused (`pool.question_holds_work`): the owner releases it, or archives the work the answer stopped and completes it then.

Completing a Lictor report, a review round or a card round never completes a question: each has its own receipt, and none closes the others by inference. A report's decision inventory, which the design's [The Pool and the Mind's word](../docs/design.md#the-pool-and-the-minds-word) asks for before the report is completed, lists the questions that are not yet released, completed or moot.

### `close`

Closes a question that has become moot, by evidence and never as an answer. Only an `open` or `deferred` question is closed: one with an applied answer has the Hive Mind's word, and its disposition is the owner's (`pool.question_answered`); one released, completed or moot is final (`pool.question_closed`). The request carries the owner's reason and at least one piece of evidence, each of which the Pool checks as it stands at the close:

| Evidence | Holds when |
| --- | --- |
| `item-archived` | The ref names the held item, and it and every item beneath it are archived: nothing is left for the hold to gate |
| `question-replaced` | The ref names another question of this home that is not moot and holds the same item with its own `word` hold active: the item stays held on that question |

Evidence that does not hold refuses the close (`pool.question_not_moot`), and nothing is written. Otherwise the question becomes `moot`, with its reason and evidence and no deferral; its card becomes `moot`; the round closes when no card of it is open; and its `word` hold is released, with the close's receipt as its `released_by`. No answer is recorded or implied: a moot question never stands as the Mind's word, a hold released by a close never verifies as released by the word, and the card is shown among the settled cards as the design's explicit supersession of an unanswered call.

A question that has become moot while the work it gates is still live is not closed: the hold is still the Hive Mind's to lift. Its owner revises the question to say what changed, with its recommendation, and the Hive Mind answers it.

### `verify`

Read-only. It answers whether the Mind's word stands, from the record and never from the reference, by [the rule below](#the-word-stands), for one subject:

| Subject | `stands` when |
| --- | --- |
| An `authority` of kind `word` | The rule below holds |
| A `task` | No `word` hold on the item or on an ancestor is active |
| A `hold` | It is a `word` hold released by the disposition of an applied answer, `release` or `complete` |
| A `question` | It has an applied answer: it is `answered`, `released` or `completed` |

The response lists, when the word does not stand, each reason once: `unanswered` (open, or for an authority answered only after the revision it names), `deferred`, `unreconciled` (an answer awaits reconciliation), `disposition-pending` (answered, its hold still active), `superseded`, `moot`, `no-question` (an imported hold no question names), `not-word`, `out-of-scope`, `not-found` and `malformed`. It returns the holds and questions it read, whole, so the reader sees the Hive Mind's exact words and judges what they authorize: `verify` establishes that the Hive Mind answered and in which words, never that the words say yes. It reads the Pool through `hive-pool read` and nothing else, records nothing and may be repeated freely.

## The word stands

An authority reference of kind `word` names a question, as a record reference with `schema` `pool.question`, its `question_id` as `id`, and the question's `record_revision` that the caller read when it relied on the answer. It stands when, and only when, all of these hold:

1. The reference has that schema and a `record_revision` no greater than the question's own, and names a question of this home; else `malformed` or `not-found`.
2. The question is `answered`, `released` or `completed`, so it stands on an applied answer; a deferred, open or moot question never stands.
3. The applied answer was recorded at a record revision no greater than the reference's: the answer the caller relied on is the one still applied. An answer that superseded it since is `superseded`; an answer first applied after the revision named is `unanswered`.
4. The question's scope contains the envelope's: the same home, and a Hive question for any scope of it, a workspace question for that workspace and its projects, a project question for that project.

The check is `word`'s, defined here once. The Pool applies the same rule to its own records where its operations accept a `word` authority, in `hive-admit approve`, a change of the budget record and `hive-rule record` of a project's Assimilation posture, as [pool.md](pool.md) states, and `hive-word verify` applies it through `hive-pool read`. Assimilation, at stage 5, calls `verify` or applies the rule to what it reads. A reference is never itself authority.

## What this does not prevent

A process running as the same user that ignores its instructions can call `hive-word answer`, or the Pool's six operations with a forged caller and causation, and name an operator request as its source. The record then shows an answer whose source is an operator request, and nothing in the store prevents it: the home is private to one user, the caller authenticates nobody, and an operator request has no evidence beyond itself. By the operator's decision Hive asks no proof of presence for an answer, an answer keeps its recorded source and nothing more, and a worker keeps its reach: no session is cut off from `hive-word` or from the home to make a forgery impossible. A review submission can be checked against its round file and a session input against its receipt, which is why the source is kept and shown with every answer; an operator request cannot. The refusal of a worker caller and the Pool's handoff check stop honest mistakes and say who brought the words; they are not authentication and are never presented as such.

## Error codes

`word`'s own codes, returned before anything is sent to the Pool:

| Code | Status | Meaning |
| --- | --- | --- |
| `word.not_authorized` | `refused` | The caller is not one the operation accepts, or an operator-request source is brought by a caller that is not the operator. |
| `word.question_changed` | `held` | The answer was given against an earlier revision and is recorded unreconciled; `error.hold` names the hold. |

`word` also returns, as [commands.md](commands.md#common-error-codes) defines them, `authority_insufficient` for a review submission whose submit event is absent from its round file or carries other words, and `replay_mismatch` for a `request_id` used before with another request.

The Pool's codes for the six operations, which `word` returns unchanged:

| Code | Status | Meaning |
| --- | --- | --- |
| `pool.answer_exists` | `conflict` | The question stands on an applied answer and the answer does not supersede it, or is Later. |
| `pool.answer_reference` | `invalid` | `supersedes` is given while no answer is applied, or `reconciles` names an answer that is not unreconciled. |
| `pool.card_stale` | `conflict` | The card's presentation revision is no longer the one answered. |
| `pool.option_unknown` | `invalid` | The option is not one of the question revision answered, or a question's options repeat an identity or omit its recommendation. |
| `pool.round_incomplete` | `refused` | The round was not collected whole: its cards take no answer and it takes no new card. |
| `pool.round_closed` | `refused` | A card cannot join a closed round. |
| `pool.held_item_archived` | `refused` | A question cannot hold an archived item. |
| `pool.question_answered` | `refused` | A question with an applied answer cannot be revised or closed as moot. |
| `pool.question_unanswered` | `refused` | A question with no applied answer has no disposition to record. |
| `pool.question_closed` | `refused` | A question released, completed or moot takes no answer at its current revision, no revision, no disposition and no close. |
| `pool.question_holds_work` | `refused` | A question whose held work is live is released, or completed only once that work is archived. |
| `pool.decision_item` | `refused` | A question whose held item is, as it stands, a decision item releases no work and is completed instead. |
| `pool.question_not_moot` | `refused` | Evidence offered for a close does not hold. |

The common codes apply unchanged: `authority_insufficient` for a request to the six operations that is not a handoff from `hive-word`, `not_found`, `already_exists`, `scope_mismatch` and `revision_conflict`.

## What `test-word` proves

With a fake `hive-pool` built from the declared schemas: each operation sending exactly one handoff, with the request's own `request_id`, correlation, scope, caller and causation digest; a retry of a `hold`, a `revise`, an `answer`, a `release`, a `complete` and a `close` returning the recorded outcome with no second set of identities and no check against the changed records, even after the first answer changed the round; a reused `request_id` with other words refused with `replay_mismatch` and nothing sent; an unreconciled outcome returned as `held` with `word.question_changed` and the hold in `error.hold`, on the first call and on its replay; a worker's request refused for every operation, a request from the package `ui` refused for every operation, and an operator-request source from a caller that is not the operator refused, each with `word.not_authorized` and nothing sent; a review submission whose submit event is absent, or carries another option, label, text or Later, refused with `authority_insufficient`, and one that matches sent through; a session-input source sent through and recorded unverified; a request with a string of a listed credential shape refused whole with nothing sent, and free text without one passing unchanged; a refusal, conflict or hold of the Pool returned with the Pool's code; and `verify` reading the record for each subject kind, a superseded answer, a deferral, a pending disposition, a moot question and an imported hold with no question among them, and an authority standing only on the answer applied at the revision it names, in its scope.

`test-pool` proves the records and the six operations, as [pool.md](pool.md#what-test-pool-proves) lists them. `test-integration` proves the real `hive-word` to `hive-pool` call, each exit state the Pool can return through `word`, replay through both, two identical requests racing to one outcome, and a record of 2,097,152 bytes crossing whole.

## Decisions taken here

Each decision is followed by the alternative it rejected.

1. The decision records are Pool records with `pool.` kinds; `word` mints their identities and writes nothing. `word.` kinds written by `pool` would break the rule that a package never writes a name that begins with another package's, and `word` has no part of the home to write a receipt in.
2. `word`'s replay is the Pool's, as a handoff that the Pool keys on the digest of the original `hive-word` request, read back through `hive-pool read` before any check of the present records and before any identity is minted. A replay store in `word` would be the store the design says it does not keep; a replay that skipped the digest would return the old outcome for other words; and keying the Pool's replay on its own request, whose identities `word` mints anew on a retry, would answer a racing retry with `replay_mismatch` instead of its outcome.
3. Every check of a record as it stands is the Pool's, inside the transaction that writes; checks in `word` against a read would let two requests racing past the read both apply.
4. A card is an entry of the decision round, with its own revision, and a question has one card; a `pool.card` record would be one more file per question with no reader that needs it apart from the round.
5. Answers are deduplicated by their question and their source, the evidence that carries the Hive Mind's words, and not only by request identity; deduplication by request identity alone would record a resent review submission twice.
6. An answer is accepted and never releases a hold; the owner's `release` or `complete` does, with its note. Releasing on acceptance would free work on any answer, a refusal included, and would leave no moment in which the work waits on its supervisor, which the design's progress, map and cards each show.
7. An answer to a changed question is recorded exactly as submitted and returned as `held`, a Later included, which sets no deferral; refusing it would lose the words, and applying it would answer a question it was not given to.
8. Answer entries are never changed: supersession and reconciliation are read from the later answer, as a superseding rule leaves the rule it supersedes untouched; the question names the answer it stands on. Marking the earlier entry would rewrite a record of the Hive Mind's words.
9. A question and a card have no superseded state, since no operation produced one. An unanswered question that has become moot is closed by evidence the Pool checks, the held work archived or the question asked again, which releases its hold and never counts as the word. A close on the owner's say alone, or one that released live work, would lift a hold that only the Hive Mind can lift.
10. Supersession is open only while the disposition is pending; once released, completed or moot, a question is final and a new word on the matter is a new question. Reopening a settled question would re-hold work already released under a record that says it was released.
11. The word stands on an answer bound by the record revision the caller read, so an approval never outlives a superseding answer. A reference by question alone would let an approval stand on words the Hive Mind replaced.

## Left undecided

| Open point | What settles it |
| --- | --- |
| How the UI's Waiting on you page sends the operator's answer to `word`, and under which `source` kind | Stage 6, when the card component is built, which amends the caller table above then; `operator-request` serves a terminal now |
| Verification of a `session-input` source against the input receipt of the supervisor session | Stage 4's session adapter, which defines the receipt's reader; until then the source is recorded and shown unverified |
| Whether a supervisor may supersede an answer, or record a disposition, under a recorded posture | Stage 4, which defines postures; the Hive Mind's own later words and the owner's disposition suffice now |
| Where a supervisor records the decision inventory of a Lictor report, and how it finds the questions the report raised | Stage 4, which builds collection; a selector of `hive-pool read` by origin, if it needs one, enters then as a minor change |
