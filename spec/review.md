# Review files

A mockup or design review is a directory of files in the owning supervisor's private home. The design fixes the files and what each must hold; this file fixes their schemas, names and writers. The surface that shows them, the server that serves them and the routing of a submitted round are stage 6's.

Owner: `ui`, which is the only writer of anything under `reviews/`. Schemas: [`review.v1`](schemas/review.v1.schema.json), [`review-feedback.v1`](schemas/review-feedback.v1.schema.json), [`review-reply.v1`](schemas/review-reply.v1.schema.json), [`review-final.v1`](schemas/review-final.v1.schema.json). Proved by `test-contract` for the shapes and by `test-ui` for the behavior.

## The directory

```text
reviews/<task_id>/<generation>/<review_id>/
  review.json
  revision-0001.html
  revision-0002.html
  assets/0001/...
  assets/0002/...
  round-0001.feedback.json
  round-0001.reply.json
  round-0002.feedback.json
  attachments/0001/...
  final.json
```

- The path is relative to the home. `<generation>` is the number written in decimal.
- A revision number and a round number are written with at least four digits. They rise independently, each from 1, and are never reused.
- A revision's assets live under `assets/<revision number>/` and nowhere else, so a later revision cannot overwrite an earlier one's.
- A round's attachments live under `attachments/<round number>/`.
- Every path inside these files is relative to the review directory.

## Who writes what

Every file is written by `ui`, through `hive-ui`. The party that supplies the content differs:

| File | Supplied by | Written |
| --- | --- | --- |
| `review.json` | The owning supervisor registers the review; the Lictor publishes each revision | Replaced atomically at each change, under the review's lock |
| `revision-NNNN.html` and its assets | The assigned Lictor | Once. Never changed |
| `round-NNNN.feedback.json` | The Hive Mind, through the page | Replaced atomically as events are added; earlier events are never changed or dropped |
| `round-NNNN.reply.json` | The assigned Lictor | Once |
| `final.json` | The Hive Mind pressing Done | Once |

A Lictor and a supervisor never write these files directly. They call `hive-ui`, whose operations for registering, publishing and replying stage 6 defines. The feedback writer is the one narrowly scoped writer the page reaches, and it writes only the registered round's file.

## `review.json`

The review's identity and manifest.

- **Identity:** `review_id`, the [scope](identities.md#scope) (`hive` for the Hive's own projectless work, otherwise `project`, or `workspace` for work that belongs to no single project), `task_id` and `generation`.
- **`owner`:** the supervisor that owns the review: the Hive, or the Fleet of one workspace, by home.
- **`lictor`:** the assigned Lictor, as the task and generation of the execution that writes revisions and replies.
- **`parent_route`:** the registered route to the owner's parent, or `none` for the Hive's own review. Identifiers and this route decide ownership; a matching short name or a selected filter never does.
- **`revisions`:** the manifest. One entry per published revision: its number, its HTML file and every asset it may load, each with path, digest and size, the round it answers where there is one, and when it was published. An entry is never changed or removed.
- **`current_revision`** and **`current_round`:** what the page shows and where new feedback goes. `current_revision` is 0 until the first revision is published.
- **`completion`:** null until Done, then a reference to `final.json`.

A new current revision is published only after its HTML, its assets and any reply to the previous round are durable. Publishing is one replacement of `review.json`.

The server serves a revision's HTML and its declared assets and nothing else, after checking each path and digest against this manifest.

## `round-NNNN.feedback.json`

Exactly one per round. It carries the round's bindings and the ordered events of everything the Hive Mind pressed or typed.

**Bindings:** `review_id`, `round_id`, the scope, `task_id`, `generation`, `owner`, `lictor` and the `reviewed_revision` with its path and digest. The file's `record_revision` is the round revision a submission names as expected.

**Events**, each with:

| Field | Meaning |
| --- | --- |
| `event_id` | Chosen by the page and reused on retry, so a retried save is one event. |
| `sequence` | Assigned by the writer: consecutive from 1 in the file. |
| `captured_at`, `received_at` | When the page captured the input, and when the writer made it durable. |
| `operator` | The authenticated operator as the serving login established it: the local home-bound UI session, or the verified private-network identity. Never a value the browser supplied. |
| `kind` | `target`, `note`, `card-draft`, `submit` or `done`. |
| `control_label` | The exact label of the control pressed. Required for `submit` and `done`. |

and exactly one of the following, by kind. A field that does not apply is absent.

| Kind | Member | Holds |
| --- | --- | --- |
| `target` | `target` | A click or a selection: the reviewed revision and digest, the element's locator and captured text, and for a selection the exact quoted text with its start and end paths and offsets. |
| `note` | `note` | A note as it stands after this edit: its stable `note_id`, its `note_kind`, its exact text, and for an annotation its target. |
| `card-draft` | `card_draft` | A card as it stands after this edit: the card binding, the selected option and its displayed label if one is selected, the typed text verbatim, or Later with its revisit date. |
| `submit` | `submission` | The commitment: see below. |
| `done` | `completion` | The displayed revision and digest, the last feedback sequence the final record includes, and which sequences were submitted and which were drafts left unsubmitted. |

`target`, `note` and `card-draft` are **drafts**. They send nothing to `word` and give no worker anything to act on.

A note has a `note_kind`. An `annotation` has a target, and keeps the original target even when a later revision moves or removes the element. An `instruction` is a free-standing instruction for the round and has none. It is its own entry and is never recast as an answer to a card.

**The card binding** ties a card to the decision it presents: card, question, held item and decision round, each with its revision, and the home and task that own the decision. It keeps the review round distinct from the decision round.

**A submission** is the commitment boundary. It holds a stable `request_id`, the round revision it expects, the last draft sequence it includes, and an immutable snapshot of:

- each answered card, as one answer: the option, if any, together with the text typed with it. An option is never applied ahead of its text, and text with no option is a complete answer. A card left unanswered has no entry and stays open;
- the round's notes and instructions;
- the attachment manifest.

The writer acknowledges a submission only after the snapshot is durable, and returns its `event_id` and `sequence`. A different round revision, or a second submission that conflicts, is returned as a `conflict` and overwrites nothing. A repeated `request_id` commits once.

A submission is a receipt, not a ruling. Nothing in the file is an accepted answer until `word` says so through its own receipt, and a stale or incomplete card gains nothing by being written here.

## `round-NNNN.reply.json`

The Lictor's reply to one round, written once.

- **Bindings:** the review, the round, the scope, task and generation, the Lictor, and the feedback file it answers with that file's digest and the last sequence the Lictor read.
- **`responses`:** each with its own `response_id`, the `event_id` of the submission it answers, and the `note_id`, `question_id` or `card_id` it answers where it answers one of them; its text; a picture of the element it concerns where one helps, as a declared asset; and the binding of a decision card where the response puts a decision to the Hive Mind.
- **`next_revision`:** the revision written in answer to this round, with its path and digest, where one was asked for.

The Hive Mind's words stay in the feedback file. A reply quotes none of them as its own record.

## `final.json`

Written once, by Done, after the Done event is durable in the current feedback file.

- **Identity:** the review, scope, task and generation; the Done event by round, `event_id` and sequence; the operator; the time.
- **`final_revision`:** the revision displayed at Done, with its digest.
- **`rounds`:** every round, with its feedback file, that file's digest and the last sequence Done included, and its reply where one was written.
- **`decisions`:** every card the review presented, with what was known of its decision at that moment: `pending`, `accepted`, `released`, `completed` or `superseded`, and the `word` receipt that establishes any state but `pending`.
- **`unsubmitted_drafts`:** every draft saved and never submitted, by round and sequence.

A crash between the Done event and this file is recovered from the Done event. `final.json` records that the review ended and what Done included. It supplies no missing answer, closes no hold, accepts no implementation, completes no task and authorizes no Assimilation.

## Rules that hold across the files

1. **Words are kept exactly.** Text is stored as typed, whitespace included. An amendment is a new event; nothing is replaced by a summary.
2. **Nothing is dropped under pressure.** A writer that cannot keep an event whole refuses with a precise failure. It never truncates and never reports a save that did not happen.
3. **History only grows.** Events, manifest entries, replies and the final record are never changed once written. A late submission to an earlier round is appended to that round's file.
4. **Files, not a listener.** Everything a review needs survives the end of the Lictor, the supervisor and the UI process. A restart replays pending submitted feedback through the same event identities.
5. **One decision intake.** A committed card answer reaches the decision only through `hive-word answer`, sent by the owning supervisor with the exact words and the original question and decision-round revisions. `ui` never writes a decision.

## Where this reads the design narrowly

The design names five event kinds and says a free-standing instruction is "its own entry". This specification keeps the five kinds and records an instruction as a `note` whose `note_kind` is `instruction`, with its own `note_id`, rather than adding a sixth kind. The design calls the round's identity `round_id` and also numbers rounds in file names; here `round_id` is that number. Both readings are listed in [decisions.md](decisions.md#14-two-narrow-readings-of-the-review-files).
