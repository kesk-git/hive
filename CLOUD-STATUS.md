# Cloud status: review schema fixtures

## What landed

Example files for the four review schemas, under `contract/testdata/schemas/`:

- `review/` proves `review.v1.schema.json` (`review.json`).
- `review-feedback/` proves `review-feedback.v1.schema.json` (`round-NNNN.feedback.json`).
- `review-reply/` proves `review-reply.v1.schema.json` (`round-NNNN.reply.json`).
- `review-final/` proves `review-final.v1.schema.json` (`final.json`).

Each directory holds `valid/` and `invalid/` only, every file is `.json`, and nothing else was touched. The accepted examples cover each scope level, both owner roles, both parent-route kinds, an unpublished and a completed review, every event kind of a feedback file, a text-only card answer, a deferred card, a reply with pictures and a raised card, a reply with nothing to answer, every decision state and every draft kind. Each refused example breaks exactly one rule the schema states and is named for that rule: a missing required member, a malformed identifier, digest, path or time, a value below its minimum, a wrong type, an unlisted member, a value outside an enumeration, a scope field the scope level forbids or needs, an owner or route member its kind forbids or needs, an event carrying the member of another kind or lacking its own, a `submit` or `done` event without its control label, an annotation without a target or an instruction with one, an option without its label or a label without its option, a decision with a receipt while pending or without one in any other state, and a record-revision or open `unknown` member on the two write-once files that declare neither.

A fractional number is refused by the decoder before any schema is applied, so the "not an integer" examples use strings. The same tree was run through the schema package once more outside the test to confirm that every refused file is refused at the member its name points to, and for the reason its name gives.

## What the commands printed

```
$ make check
go vet ./...

$ make test-contract
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	(cached)
ok  	github.com/kesk-git/hive/contract/clock	(cached)
ok  	github.com/kesk-git/hive/contract/ids	(cached)
ok  	github.com/kesk-git/hive/contract/schema	1.560s
ok  	github.com/kesk-git/hive/contract/store	(cached)
?   	github.com/kesk-git/hive/spec	[no test files]
```

Both exited 0. The module's Go toolchain was fetched without trouble.

## Unproved by examples

Each rule below is stated by `spec/review.md` but cannot be expressed by the committed schema: it holds across two files, across rounds or writes, between fields the schema binds separately, or describes a writer's or server's behaviour. No example was invented for any of them. Each is listed by the sentence that states it.

The directory:

- "A revision number and a round number are written with at least four digits. They rise independently, each from 1, and are never reused." (File naming, and uniqueness across writes.)
- "A revision's assets live under `assets/<revision number>/` and nowhere else, so a later revision cannot overwrite an earlier one's." (Ties an asset path to the number of the entry that lists it.)
- "A round's attachments live under `attachments/<round number>/`." (Ties an attachment path to the round of the file.)

Who writes what:

- "Every file is written by `ui`, through `hive-ui`." and the table beneath it: who supplies each file and whether it is written once or replaced. (Writer behaviour; the schema fixes only the `writer` member's shape where the file carries one.)
- "Any package of the same home may read these files through the contract library's validated reader, without a lock, as the Shadow does to read the registered review files; none but `ui` writes them."
- "The feedback writer is the one narrowly scoped writer the page reaches, and it writes only the registered round's file."

`review.json`:

- "`parent_route`: the registered route to the owner's parent, or `none` for the Hive's own review." (The schema binds the route's members to its kind and the owner's members to its role, but does not tie the route's kind to the owner's role.)
- "Identifiers and this route decide ownership; a matching short name or a selected filter never does."
- "An entry is never changed or removed." (Across writes.)
- "`current_revision` is 0 until the first revision is published." (Ties `current_revision` to the manifest.)
- "`completion`: null until Done, then a reference to `final.json`." (The schema fixes the shape, not the moment it changes or the file it names.)
- "A new current revision is published only after its HTML, its assets and any reply to the previous round are durable. Publishing is one replacement of `review.json`."
- "The server serves a revision's HTML and its declared assets and nothing else, after checking each path and digest against this manifest."

`round-NNNN.feedback.json`:

- "Exactly one per round."
- "The file's `record_revision` is the round revision a submission names as expected." (Ties a submission's `expected_round_revision` to the file's `record_revision`.)
- "`event_id`: Chosen by the page and reused on retry, so a retried save is one event."
- "`sequence`: Assigned by the writer: consecutive from 1 in the file." (Across the items of the array.)
- "`operator`: The authenticated operator as the serving login established it: the local home-bound UI session, or the verified private-network identity. Never a value the browser supplied."
- "`target`, `note` and `card-draft` are **drafts**. They send nothing to `word` and give no worker anything to act on."
- "An `annotation` has a target, and keeps the original target even when a later revision moves or removes the element." (The first half is proved; keeping the target across revisions is not.)
- "It is the same binding in the card draft, the submission's card answer, the reply and the final record. It keeps the review round distinct from the decision round."
- "It holds a stable `request_id`, the round revision it expects, the last draft sequence it includes, and an immutable snapshot of:" (That `last_draft_sequence` names an earlier event of the file, and that the snapshot matches the drafts.)
- "An option is never applied ahead of its text, and text with no option is a complete answer." (The second half is proved by the text-only answer; the first is behaviour.)
- "A card left unanswered has no entry and stays open;"
- "The writer acknowledges a submission only after the snapshot is durable, and returns its `event_id` and `sequence`. A different round revision, or a second submission that conflicts, is returned as a `conflict` and overwrites nothing. A repeated `request_id` commits once."
- "A submission is a receipt, not a ruling. Nothing in the file is an accepted answer until `word` says so through its own receipt, and a stale or incomplete card gains nothing by being written here."
- "`done`: The displayed revision and digest, the last feedback sequence the final record includes, and which sequences were submitted and which were drafts left unsubmitted." (That the listed sequences name events of this file, of the stated kinds.)

`round-NNNN.reply.json`:

- "The Lictor's reply to one round, written once."
- "Bindings: the review, the round, the scope, task and generation, the Lictor, and the feedback file it answers with its path, the last sequence the Lictor read and the digest of the events through that sequence (the events digest)." (That the path, sequence and digest match the feedback file.)
- "`responses`: each with its own `response_id`, the `event_id` of the submission it answers, and the `note_id`, `question_id` or `card_id` it answers where it answers one of them; its text; a picture of the element it concerns where one helps, as a declared asset; and the binding of a decision card where the response puts a decision to the Hive Mind." (That the event is a submit event of the feedback file, that the identities appear in it, and that the picture is an asset `review.json` declares.)
- "`next_revision`: the revision written in answer to this round, with its path and digest, where one was asked for." (That it matches a manifest entry whose `answers_round` is this round.)
- "The Hive Mind's words stay in the feedback file. A reply quotes none of them as its own record."

`final.json`:

- "Written once, by Done, after the Done event is durable in the current feedback file."
- "Identity: the review, scope, task and generation; the Done event by round, `event_id` and sequence; the operator; the time." (That the Done event exists in that round's file with that operator.)
- "`final_revision`: the revision displayed at Done, with its digest." (That it matches the Done event's displayed revision.)
- "`rounds`: every round, with its feedback file, the last sequence Done included and the events digest through that sequence, and its reply where one was written."
- "`decisions`: every card the review presented, with what was known of its decision at that moment: `pending`, `accepted`, `released`, `completed` or `superseded`, and the `word` receipt that establishes any state but `pending`." (The states and the receipt rule are proved; that every presented card is listed, and that the receipt establishes the state, are not.)
- "`unsubmitted_drafts`: every draft saved and never submitted, by round and sequence."
- "A crash between the Done event and this file is recovered from the Done event. `final.json` records that the review ended and what Done included. It supplies no missing answer, closes no hold, accepts no implementation, completes no task and authorizes no Assimilation."

The events digest:

- "A reply and the final record bind the file to what they read by its **events digest**: the digest of the canonical form of the JSON array of the file's events with `sequence` 1 through the stated `last_sequence`, in order." (The schema fixes the digest's form, not its value.)
- "Those events are never changed or dropped, so the digest keeps verifying after later events are appended, and a verifier recomputes it from the file as it stands."
- "The reply file and each revision file are written once, so their digests are the digests of their bytes."

Rules that hold across the files:

- "**Words are kept exactly.** Text is stored as typed, whitespace included. An amendment is a new event; nothing is replaced by a summary."
- "**Nothing is dropped under pressure.** A writer that cannot keep an event whole refuses with a precise failure. It never truncates and never reports a save that did not happen."
- "**History only grows.** Events, manifest entries, replies and the final record are never changed once written. A late submission to an earlier round is appended to that round's file."
- "**Files, not a listener.** Everything a review needs survives the end of the Lictor, the supervisor and the UI process. A restart replays pending submitted feedback through the same event identities."
- "**One decision intake.** A committed card answer reaches the decision only through `hive-word answer`, sent by the owning supervisor with the exact words and the original question and decision-round revisions. `ui` never writes a decision."

done claude/review-schema-fixtures-m0asry
