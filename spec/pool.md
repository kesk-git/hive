# The Pool

The Pool is the durable work queue of a private home and the one authoritative store that admission, specifications, rules, the Mind's word and task generations share, with one writer per transaction. This file is the contract of the `pool` package for stage 2 of the [ordered implementation plan](../docs/design.md#ordered-implementation-plan): where its records live, what each holds, what every operation does and refuses, how the execution place enters the contracts, how a legacy backlog comes in and goes out, and what `test-pool` proves. The decision intake that persists through it is in [word.md](word.md); the import of its upstream is planned in [import-plan-tasks-axi.md](import-plan-tasks-axi.md).

Owner: `pool`. Schemas: the eleven records under [`schemas/records/`](schemas/records/) whose names begin `pool.`, and the operation bodies under [`schemas/ops/`](schemas/ops/) whose names begin `hive-pool.`, `hive-admit.`, `hive-imperative.`, `hive-spec.`, `hive-rule.` and `hive-memory.`. Proved by `test-contract` for the shapes and by `test-pool` for the behavior.

Nothing in this stage dispatches. The Pool records the place requested for an item's work and never assumes that the work has a worktree, a session or a clone.

## The store

Everything the Pool owns is under `pool/` in the home, in the four places [records.md](records.md#the-home) gives every package, and nowhere else:

| Path | Holds |
| --- | --- |
| `pool/records/item/<task_id>.json` | [Items](#items). |
| `pool/records/hold/<hold_id>.json` | [Holds](#holds). |
| `pool/records/admission/<admission_id>.json` | [Admission records](#admission). |
| `pool/records/imperative/<task_id>.json` | [Canonical Imperatives](#the-imperative), one per item, keyed by the item. |
| `pool/records/specification/<specification_id>.json` | [Specifications](#specifications). |
| `pool/records/rule/<rule_id>.json` | [Rules](#rules). |
| `pool/records/projection/<projection_id>.json` | [Knowledge projections](#generated-knowledge). |
| `pool/records/migration/<migration_id>.json` | [Legacy migrations](#legacy-import-and-export). |
| `pool/records/move/<move_id>.json` | [Moves between homes](#moves-between-homes). |
| `pool/records/question/<question_id>.json`, `pool/records/decision-round/<decision_round_id>.json` | What the Mind's word persists: see [word.md](word.md). |
| `pool/receipts/`, `pool/requests/`, `pool/tx/` | As [records.md](records.md#the-home) fixes them. |

- **One writer.** `hive-pool` and its five sibling command names are one executable, and every write to `pool/` is one transaction of that executable under `locks/pool.lock`, as [records.md](records.md#writing-several-records) fixes. No other package writes under `pool/`, and `word` least of all: it calls.
- **One reader.** Only `pool` reads under `pool/`. Every other package reads the Pool through `hive-pool read`, which returns whole records as a [projection](#reading-the-pool).
- **Never inside a repository.** Every `hive-pool` command refuses a home inside a Git working tree with `home_inside_repository` before it reads anything, as every command does. The Pool therefore cannot write into a project's code root, and `test-pool` proves it for the import, which is the one operation given a path that may lie inside a repository: that path is read and never written.
- **No secret value.** No record, receipt, event, intent or recorded outcome under `pool/` holds a secret value. Where a record must name a secret, it holds a `secret_ref` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json): the name of the platform's secret store and the entry's name in it, and nothing else. The Imperative's `access.credentials` is the one place a credential is named, and the schema admits only references there. Free text is the author's responsibility; a test of this rule is a schema test, and a request that puts a value where a reference belongs is `invalid`.

## Identifiers

Stage 2 registers seven prefixes, each minted by `pool`:

| Prefix | Identifies |
| --- | --- |
| `adm_` | an admission record |
| `spec_` | a specification |
| `rule_` | a rule |
| `hold_` | a hold on an item |
| `mem_` | a knowledge projection |
| `migr_` | a legacy migration |
| `move_` | a move between homes |

Their patterns are the `$defs` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json). The table in [identities.md](identities.md#identifier-form) and the registry in `contract/ids` are kept in step by a test of `test-contract`, so the rows are added there, with the constants, by the first work item of the build and not by this definition. The Imperative has no prefix of its own: it is keyed by its item. A generation, a criterion, an option and an answer are numbered or slugged inside their records and have no prefix.

The `q_`, `dr_` and `card_` identifiers of a question, a decision round and a card are minted by `word`, as identities.md says, and written by `pool`.

## Items

An item is one unit of work: a Swarm's change, a Lictor's report, or a decision held for the Hive Mind. It describes work, never a persistent agent, and a Fleet is never an item. Schema: [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json).

### State

`state` is one of `queued`, `in-flight`, `done`, `archived` and `moved`. The first three are the three sections of the legacy backlog and keep its transitions; `archived` is a retired item, never a deleted one; `moved` is an item that left by a [move](#moves-between-homes) and stays so that its identity and history resolve.

`definition` is kept beside `state`: `not-required`, `needed`, `defining` or `accepted`. The design's five-step passage is the two read together: *definition-needed* and *defining* are queued items with those definitions, *accepted* is a queued item whose definition is accepted, *building* is in-flight and *delivered* is done. Holds are kept apart from both, as separate records.

| From | To | Through | Refused when |
| --- | --- | --- | --- |
| `queued` | `in-flight` | `open-generation`, or `transition` | an active hold stands: `held`, naming it; a `blocked-by` edge names an item of this Pool that exists and is not done: `pool.blocked`; `specification_required` and `definition` is not `accepted`: `pool.specification_unaccepted`; `deliverable` is `decision`: `pool.not_dispatchable` |
| `queued`, `in-flight` | `done` | `transition` | a generation is open: `pool.generation_open` |
| `done` | `queued`, `in-flight` | `transition` (reopen) | as for the state entered |
| any but `archived`, `moved` | `archived` | `transition` | an item not done has a dependent whose `blocked-by` edge names it and is itself not done or archived: `pool.dependency_active`; a generation is open: `pool.generation_open` |
| `archived`, `moved` | anything | | always: `pool.state_transition` |

- `done` sets `closed_at`, from the request's `at` or the clock, and adds the request's `evidence` to `links`. A reopen clears `closed_at`. A completion without evidence is allowed and is what it says: nothing in the Pool claims a landing, a report or a Harvest that no link names.
- An unanswered hold survives `done` and `archived`: the hold record stays active, the question stays owed, and only the Mind's word releases it. Cleanup, at a later stage, reads the hold and retains the question.
- `transition` to `in-flight` without a generation exists for the legacy import and for tests; dispatch at stage 4 goes through `open-generation`.

### What an item carries

| Field | Meaning and rule |
| --- | --- |
| scope fields | The [scope](identities.md#scope). The Hive's own work is `hive`; a Fleet's is `project`, or `workspace` for work of no single project. `clone_id` is given only where the work is bound to one registered clone; the Pool never requires it. |
| `title`, `body` | Kept exactly as written. |
| `deliverable` | `swarm`, `lictor` or `decision`. Absent until intake assigns it; a decision item never dispatches. |
| `class` | The dispatch class, 0 to 7. Absent until intake assigns it. |
| `priority` | 0 to 4, as the legacy format carries it. |
| `placement_requested` | The [execution place](#the-execution-place). `policy` when the request leaves it out. |
| `delivery` | The three delivery axes, declared separately: `validation` (`synapse` or `direct`), `publication` (`harvest` or `local-only`), `assimilation` (`word` or `posture`). A change of publication cannot remove validation: the two are independent fields, and `local-only` with `synapse` is an ordinary combination. |
| `specification_required`, `specification` | Whether build dispatch needs an accepted specification, and the accepted revision bound by identity, revision and digest. Only `hive-spec accept` sets `specification`, and an invalidation clears it. |
| `deps` | `blocked-by`, `parent` and `discovered-from` edges to items of the same Pool, each with free-text `reason` that is carried and never read for state. A new `blocked-by` edge must name an item that exists; a dangling edge found in a legacy file is kept and treated as resolved, as the upstream does. |
| `links` | A Harvest by full URL, a report or document by URL or path, or a record by reference. |
| `schedule` | A `gate` with its not-before time or a `recurrence` with its interval and anchor, as declared on the item, each a wall-clock time with its zone. The item owns the work; the dispatcher's scheduled-job record, at stage 4, carries the runs. A date here dispatches nothing. |
| `generations` | Every [generation](#generations) opened on the item. |
| `handoff` | The one handoff to a child Fleet, with its evidence, instead of a second competing task. Written by the parent on the receipt stage 4's `hive-fleet route` returns. |
| `moved_from`, `moved_to` | Set by a [move](#moves-between-homes). |
| `legacy` | The legacy identity, kind, repo and free-form fields, verbatim, with the migration that created the item. Never read as authority. |

`add` mints the `task_id`, writes the item and the `pool.item.added` receipt, and refuses a `blocked-by` edge to an item that does not exist (`pool.dependency_missing`). `update` applies a patch and reports the fields that changed; a patch that changes nothing writes nothing, raises no revision and returns no receipt. A change of `placement_requested` after a generation opened does not touch that generation; the next one reads it.

### Derived facts

`blocked`, `held` and `ready` are never stored. `hive-pool read` computes them at the moment of the read: an item is blocked when it is not done and a `blocked-by` edge names an item of this Pool that exists and is not done; held when a hold on it is active and, for a dated hold, its `until` has not passed; ready when it is queued, not blocked, not held and not a decision item.

### Generations

A generation is one execution of an item. It is opened in the Pool before anything is launched and closed when the caller establishes how it ended, and the Pool records only what it is told: the number, the place requested at the moment it opened, the Imperative revision and admission it runs under, and the outcome and evidence the closer named. Execution state, the backend, the resolved place and the lease belong to the dispatcher's and the Spawning Pool's records at later stages, bound to the item by `task_id` and `generation`.

- `open-generation` requires an admission in state `admitted` whose last check read the item's current revision and the Imperative's current revision; a changed item or Imperative since then is `pool.admission_stale`, and the caller checks again. It numbers the generation one above the last, snapshots `placement_requested`, moves a queued item to in-flight in the same transaction, and writes `pool.generation.opened`. At most one generation is open at a time: a second is `pool.generation_open`.
- `close-generation` records the outcome the caller reports from its own receipts: `delivered`, `failed`, `stopped`, `superseded` or `unknown`, with the receipt or record as `closed_by`. `unknown` is an outcome and never a success. Closing changes the item's state not at all; delivery is a `transition` with its own evidence.
- A change of place is a new generation, never an edit: the snapshot in a closed generation is never changed.

The Spawning Pool at stage 3 reads the generation as the Pool records it: `task_id`, `generation` and `placement_requested`. A generation whose requested place is `claude-cloud-session` or, once it is ever admitted, `codex-void`, holds no lease of its own; the Spawning Pool leaves room for it and for the collection lease taken on its behalf.

## Holds

A hold keeps an item out of ready and out of dispatch. Each is its own record, so that a response can name it in `error.hold`, the Shadow can key a wake on it, and it can outlive the item's completion. Schema: [`records/pool.hold.v1`](schemas/records/pool.hold.v1.schema.json).

| Kind | Placed by | Released by |
| --- | --- | --- |
| `word` | `hive-pool hold-question`, for `hive-word hold` | An accepted answer through `hive-word answer`; see [word.md](word.md) |
| `admission` | `hive-admit check`, with a `reason_code` | A later `hive-admit check` that admits |
| `definition` | The Pool, when an item that requires a specification is added or its acceptance is invalidated | `hive-spec accept` |
| `parked`, `external`, `resource`, `future` | `hive-pool hold`, with a reason; `future` with its `until` | `hive-pool release` |

- A dated hold is inactive on and after its `until` and stays `active` as a record until released; the Shadow reads active holds with `due_before` and wakes the owner, who releases or re-presents. A hold's `until` is a wall-clock time with its zone; a legacy date is read at the start of that day in the import's zone and kept beside it as written.
- Releasing a hold writes `pool.hold.released` with the release's evidence and never deletes the record.
- A `word` hold imported from a legacy backlog has no question; it is answered by `hive-word hold` raising one for it, which binds the question to the existing hold, and then as any other.

## The execution place

Where an item's work runs is a choice made for that item. The Pool records the request, the Imperative renders it, admission checks it. Resolution, launch and the binding of the resolved place into the execution record are stage 4's.

`placement_requested` is one of:

| Value | Meaning |
| --- | --- |
| `mac` | On the Mac. |
| `claude-cloud-session` | In a Claude cloud session. |
| `policy` | Left to the policy resolver. |
| `codex-void` | Codex in the Void: a named value that admission refuses with `pool.placement_parked` for as long as the backend is parked. It is never substituted and never silently changed. |

- **Setup renders the Placement axis from the item.** `hive-imperative create` and `revise` take no placement: the command copies `placement_requested` at the item revision it read into `setup.placement`, with that revision. The author cannot set it; the inspector and the rendered text show it as the item's.
- **Admission reads the item and the project.** `hive-admit check` checks the place named in its request, the item's requested place or a place the caller's policy resolved, against the project's repository binding read from the identity records: a Void placement (`claude-cloud-session` or `codex-void`) for a `local-only` project is refused with `pool.placement_local_only`; a Void placement whose source branch the caller's evidence does not show pushed is held with reason code `source_branch_not_pushed`; `codex-void` is refused as above. `mac` and `policy` pass this check for any project. The Pool runs no Git: the branch's state is evidence the caller supplies, and absent evidence is unknown, which holds.
- **The Imperative and the item agree.** A check compares the Imperative's `setup.placement` with the item's current `placement_requested`; a difference is `pool.imperative_stale` and the caller revises. A harness that cannot run in the place is a contradiction: `codex` with `claude-cloud-session` is refused with `pool.setup_contradiction`.

## Admission

Admission preserves the original request, checks the Imperative's single schema and records a receipt bound to the Imperative revision and digest and the authority used. Schema: [`records/pool.admission.v1`](schemas/records/pool.admission.v1.schema.json).

- `create` records the request immutably: the words as given, who gave them, the authority they carried and the Imperative revision they concern, which must be current. It checks nothing. Receipt: `pool.admission.created`.
- `check` runs the checks below against the Imperative revision, the item as it stands and the evidence in the request, appends the check to the record and writes one receipt: `pool.admission.admitted`, `pool.admission.refused` or `pool.admission.held`. The response status follows: `ok`, `refused` with the first finding's code, or `held` with the hold in `error.hold`. A check may be run as often as a launch path needs; every launch path at stage 4 runs it, and a later check with new evidence may admit what an earlier one held. An earlier check is never changed.
- `approve` records a required ruling: the envelope's authority must be of kind `word` and name a question of this Pool with an applied answer in the request's scope, which the Pool verifies against its own record; any other kind is `authority_insufficient`. It records the approval and nothing else; the caller runs `check` again, which reads it. Receipt: `pool.admission.approved`.

The checks, in order; the first that does not pass fixes the result, and every finding is recorded:

| Check | Fails as |
| --- | --- |
| Object, Standard, Output and Boundary are present and non-empty | `invalid` at `create`: the schema requires them. A record that lost one is `record_malformed`. |
| No field is a guess | `held`, reason code `field_guess`, finding `pool.admission_guess` naming the field, its question and its consequence. The supervisor raises the question through `hive-word hold`. |
| The Imperative is current and agrees with the item | `refused`, `pool.imperative_stale` |
| Harness and place do not contradict | `refused`, `pool.setup_contradiction` |
| The item requires a specification and carries an accepted one at the Imperative's `standard.criteria` | `refused`, `pool.specification_unaccepted`; a Standard naming a revision other than the item's is `pool.criteria_mismatch` |
| The place is admissible for the project and the backend is not parked | `refused`, `pool.placement_parked` or `pool.placement_local_only` |
| A Void place has a pushed source branch | `held`, reason code `source_branch_not_pushed` |
| Each sensitive boundary (`spend`, `deletion`, `security`, `publication`) has explicit, applicable authority: an approval on the record, or the request's own `word` authority verified against the Pool | `held`, reason code `authority_required`, finding `pool.authority_required` |
| Validation and publication of the Output agree with the item's `delivery`; `harvest` is not asked of a `local-only` project | `refused`, `pool.delivery_mismatch` or `pool.publication_needs_forge` |

Authority kinds admission can verify at stage 2 are `word` and `rule`, because their records are the Pool's. A `posture`, `charter` or `instruction` reference names a record of `dispatch`, which stage 4 defines; until that stage adds the route by which admission accepts one, such a reference is `authority_insufficient` for a sensitive boundary. High-confidence dispatch within existing authority needs no authority reference at all: `none` passes every check but the sensitive ones.

## The Imperative

The canonical Imperative is one record per item, with every revision whole, and it never enters a repository, a Harvest, a comment or a log. Schema: [`records/pool.imperative.v1`](schemas/records/pool.imperative.v1.schema.json).

A revision holds the dispatch class and the four sections of the design: **Task** (the Hive Mind's intent kept apart, then Object and Standard), **Setup**, **Rules** (the Boundary, the validation commands and the channels) and **Definition of done** (Output and the stopping point). Object, Standard, Output and Boundary are typed fields inside their sections and each carries a `confidence`: `high`, or `guess` with the question and consequence admission holds on. There is no four-part block beside the Imperative and no copy of it anywhere.

- **The Standard carries criteria by reference.** `standard.criteria` names the accepted specification revision; `render` reads that revision from the Pool and writes its criteria into the Standard's text. There is no editable copy of the criteria in the Imperative, and a revision whose `standard.criteria` differs from the item's `specification` does not pass admission.
- **Setup holds the Placement axis** copied from the item, the repository binding copied from the project record at authoring (or given, for the Hive's own projectless work), the branches with their revisions once known, the role, the harness, dependencies, tools, access and isolation assertions. Setup implements the Boundary and cannot widen it; a validation command is an argument vector and never a line of shell.
- **`create`** writes revision 1; **`revise`** writes the next, whole, with the reason, under the record's expected revision. Both copy the placement and the repository, compute the revision's digest over the canonical form of its class and four sections, and write `pool.imperative.revised`. Steering that changes scope revises first; the caller then repeats admission.
- **`render`** is read-only and deterministic: the same revision, the same specification revision and the same renderer version give the same text and digest, and `test-pool` proves it. The text opens with the worker role boundary, then the four sections in order with their headings, the Standard's criteria rendered from the specification, and the Placement axis in Setup. It returns the text, its digest and size, and the launch parameters the Imperative itself fixes: role, harness, requested place, class, validation and publication. Model, effort and the resolved place are the resolver's and are not here.
- **`deliver`** records exactly what one open generation was given: the revision, the rendered digest and size, the renderer version, the transport and the dispatcher's receipt. It is stage 4's to call and stage 2's to build and prove with a test caller. Receipt: `pool.imperative.delivered`.

## Specifications

A specification is a native record of what a change must satisfy: prose and acceptance criteria, each criterion with a stable identity and the evidence that proves it. Schema: [`records/pool.specification.v1`](schemas/records/pool.specification.v1.schema.json). A sufficiently complete existing specification is referenced by identity and need not be rewritten for every item.

- **`define`** writes revision 1 of a new specification or the next revision of an existing one, whole, with the criteria whose identity or text changed listed in `changed_criteria`. Items named in `task_ids` move to `definition` `defining`. A revision whose `changed_criteria` is not empty invalidates the current acceptance: the acceptance's state becomes `invalidated`, every item carrying that revision loses its `specification`, returns to `defining` and receives a `definition` hold; nothing else of those items changes, so a worker's target is never silently changed. Receipts: `pool.specification.defined`, and `pool.specification.invalidated` for each acceptance invalidated.
- **`accept`** records the acceptance of the current revision for one item by the supervisor that called: the Fleet's for its workspace, the Hive's for its own. It binds the revision by identity, revision and digest into the item's `specification`, sets the item's `definition` to `accepted`, releases its `definition` hold, and lists the open product questions, each a question of this Pool that is still open; an item with such a question stays held by its `word` hold, because technical acceptance settles no product choice. Receipt: `pool.specification.accepted`.
- **`close`** records, after a verified landing, that the accepted requirements were met: the landed head, the landing receipt, the proof receipts and the resolved findings the caller names, bound to the accepted revision, item and generation. It is one transaction; if it does not commit, the landing stands and the closure stays pending for replay under the same request, and nothing reports the requirements landed because the code merged. Receipt: `pool.specification.closed`. Folding the accepted delta into a standing specification is a `define` of that specification that cites the closure.

`hive-admit check` and `hive-imperative render` read the specification through the Pool's own store; no other package reads it but through `hive-pool read`.

## Rules

The rule store separates original words from their operational interpretation and keeps both. Schema: [`records/pool.rule.v1`](schemas/records/pool.rule.v1.schema.json).

A rule carries its `subject` (a fixed key for what it governs), the `words` verbatim, its `origin`, its scope from the envelope (Hive, workspace, project, and a task generation where it rides an Imperative revision), the roles it `applies` to, one `rule_line` supplied by the recorder, its `effective_at`, its applying `authority` with the intake receipt, and for a temporary rule an `expires_at`, a `lift` condition naming a receipt kind and subject, or both. A temporary rule is this same record and no second mechanism.

- **`record`** appends a rule and writes `pool.rule.recorded`. The words and dates given are never changed afterwards. A rule that `changes` a standing rule names it by identity and revision with the relation `supersedes` or `qualifies`; the standing rule is never edited and a supersession is recorded on it as an amendment of kind `supersede` in the same transaction.
- **`amend`** appends an attributed amendment of the rule's operation: `extend` or `end` with a new expiry time, `lift` with the receipt that met the lift condition, which must be of the kind and about the subject the condition named, `supersede` with the superseding rule, or `note`. The original `expires_at` stays and the latest amendment's `new_expires_at` is what the resolver uses. Receipt: `pool.rule.amended`, or `pool.rule.lifted` for a lift.
- **`expire`** records that the expiry was noticed and honoured, with the time it was noticed; before the expiry time it is refused with `pool.rule_not_expired`, and a second call returns the first receipt. Receipt: `pool.rule.expired`. The receipt records the noticing; the exclusion happened by the clock.
- **`resolve`** computes, read-only, the rules effective for the envelope's scope, a role and an instant: it collects rules in force at `at` whose scope is the Hive, the workspace, the project where the scope names one, and the task generation where the request names one; drops those that do not apply to the role, are not yet effective, are lifted, ended or superseded, or are past their expiry time by the clock, whether or not an expiry receipt exists; applies inheritance by letting the narrower scope govern a subject the wider also governs; and reports a **conflict** for a subject two rules govern at one scope with no supersession between them. It returns the rules in order, each as its rule line with the source and revision appended, the conflicts, what was excluded and why, and the **resolved-rule digest**, the digest of the canonical form of the rules array, present only when there is no conflict. A caller whose action depends on a conflicted subject holds that action; the resolver decides nothing.
- Expired and lifted rules, archived words and abandoned trials stay readable and are not loaded as current policy. A missing rendered field is not proof that a rule was retired.

The Shadow reads rules with an expiry through `hive-pool read` with `due_before`, and the UI's Scheduled view reads them the same way, with their receipts.

## Generated knowledge

`hive-memory` derives instructions from the effective rules and each fact's canonical owner, and records what each consumer received. Schema: [`records/pool.projection.v1`](schemas/records/pool.projection.v1.schema.json). One projection exists per scope and audience (`shared`, `fleet`, `project`); a re-render is a new rendering of the same record.

- **`render`** resolves the rules for the scope at the given instant, renders the text, records the sources at the revisions read, the renderer version, the resolved-rule digest and the text's digest, and writes `pool.projection.rendered`. A rendering equal to the current one by digest writes nothing. At stage 2 the sources are rules; a later stage that owns another kind of fact adds it as a source kind by a minor change.
- **`check`** reports, against the budgets given, each projection's state, size and budget standing, its stale sources, rule lines duplicated between projections of one scope, and consumers whose received or acknowledged digest is not the current one. It repairs nothing.
- **`propagate`** records one consumer's receipt, or acknowledgment, of a rendering at a digest, each with its own receipt: `pool.projection.received`, `pool.projection.acknowledged`. A step already recorded is returned unchanged.
- A projection becomes `stale` when a source it names is written at a later revision; `record`, `amend`, `expire` and `define` mark the projections that name the changed record stale in their own transaction. A stale projection cannot authorize work; the consumer that refuses one is stage 4's.

The maintenance queue the design names, into which a stow enqueues uncovered knowledge, is left to the stage that builds stow.

## Legacy import and export

The legacy Markdown backlog is an import and export format, not a second queue. Its grammar is the upstream's, translated; see [import-plan-tasks-axi.md](import-plan-tasks-axi.md). Schema of the migration: [`records/pool.migration.v1`](schemas/records/pool.migration.v1.schema.json).

**`import-legacy`** reads one file at the physical path the request names, in the zone it names, and parses it with the translated grammar. With `dry_run` true it returns the report and writes nothing. With `dry_run` false it requires that the parsed file renders back byte for byte (`round_trip_exact`), refuses otherwise with `pool.legacy_not_round_trip`, and in one transaction writes one `pool.migration` holding the source text and every entry's exact lines, one item per entry with an identifier, a hold for each entry with hold tags, and the receipts `pool.legacy.imported` and, per record, `pool.item.added` and `pool.hold.placed`. `expected_digest` makes the caller import what it inspected. The file is never written, and a second import of the same source digest into the same home is `already_exists`.

| Legacy form | Pool |
| --- | --- |
| `## Queued`, `## In flight`, `## Done` sections, a header suffix allowed | `state` `queued`, `in-flight`, `done`; other sections pass through as free lines |
| The bullet's identifier | `legacy.legacy_id`; the item gets a minted `task_id` |
| The prose before the tags | `title`; indented continuation lines are `body` |
| `(repo: x)`, `(kind: x)`, a leading kind word such as SHIP or SCOUT | `legacy.repo`, `legacy.kind`, verbatim; nothing is inferred into `deliverable` or `class` |
| `(priority: n)` | `priority` |
| `(since DATE)`, `(merged\|reported\|done\|closed DATE)` | `created_at`, `closed_at`, at the start of that day in the import's zone |
| `(hold: reason)`, `(hold-kind: captain\|external\|load\|parked\|future)`, `(hold-until: DATE)` | A hold of kind `word`, `external`, `resource`, `parked` or `future`, with `until` kept as instant, zone and the date as written |
| `blocked-by\|parent\|discovered-from: <id> - <reason>` | `deps`, resolved to the `task_id` the same import minted; an edge to an identifier the file does not hold is kept in `legacy.meta` and treated as resolved |
| Pull request URLs, `data/<id>/report.md` paths, other URLs in the prose | `links` of kind `harvest`, `report`, `doc` |
| A `PERSISTENT SECONDMATE` entry, or any entry the design's rule names as a persistent supervisor | **Excluded**: no item is created, the entry is recorded in the migration with its reason and its lines, and the export emits it verbatim. A persistent Fleet never becomes an ordinary task. |
| A public-followup marker and its body | Kept in `body` and `legacy.meta` verbatim; the upstream's public-followup state machine is not translated |
| Free-form lines without an identifier, the preamble, carriage returns, the final newline | Kept in the migration exactly, never addressed by identity |

**`export-legacy`** renders items in the legacy format and returns the text, its digest and size; it writes no file. With a `migration_id`, it emits the source's preamble, sections, free lines and entries in the source's order: an item unmodified since its import, by `record_revision`, is emitted from its exact original lines; a modified item is re-rendered canonically; an excluded entry is emitted verbatim. `exact` is true when the text equals the source. Without a migration, or with `normalize`, every item the selector matches is rendered canonically, as the upstream's `render` verb does. Legacy words are rendered from the `legacy` member and Hive-only fields are not written into the legacy form.

`test-pool` proves the round trip on the two upstream fixtures re-expressed as Hive fixtures, on a file with every tag the table names, and on a file with carriage returns, a header suffix and an unrecognized section; it proves a persistent-supervisor entry excluded; and it proves that an import into a home inside a repository is refused and that an import never writes the source path.

## Moves between homes

A move carries items from one Pool to another with their identities, so that work routed between the Hive and a Fleet, or between Fleets, is one item in one place. Schema: [`records/pool.move.v1`](schemas/records/pool.move.v1.schema.json). It is three transactions joined by receipts, each replayable, with the destination written before the source commits to the move, as the upstream does:

1. **`prepare`**, in the source: mints the `move_id`, verifies each item exists, is `queued` or `done` with no open generation, and that no `blocked-by` edge would be stranded across the two homes, meaning an edge between a moving item and one that stays (`pool.split_dependency`); records the items with their revisions and digests and the active holds that travel; sets `moved_to` on each item, which keeps its state and is not dispatchable while the move is unfinished; and writes `pool.move.prepared`. It returns the move record, the item records and the holds for the caller to carry.
2. **`accept`**, in the destination: verifies the prepared receipt and each item against its digest, refuses an identifier that already exists there (`already_exists`), writes the items with the destination scope and `moved_from`, the holds, a copy of the move record in role `destination`, and `pool.move.accepted`.
3. **`complete`**, in the source: verifies the accepted receipt and sets each item's state to `moved`. Receipt: `pool.move.completed`.

A move that stops after `prepare` leaves the source items with `moved_to` set and not dispatchable (`pool.moving`) until the caller completes or aborts; **`abort`** clears `moved_to` in the source, or in the destination removes nothing and marks the move `aborted` so that `complete` is refused and the accepted items are archived with the reason, since a record is never deleted. Recovery of either home's own transaction is the ordinary roll-forward; recovery of the move is the caller replaying the next step with the receipts it holds, which the move record also carries.

## Reading the Pool

`hive-pool read` is the one way another package sees the Pool. It reads one kind by identity or by query, takes no lock for one record and the lock shared for a list, and returns every record whole with a **projection header**: the home, the time of the read, the journal cursor at that moment and whether the read was complete. A record that cannot be read is omitted and named, never guessed; an incomplete read says so and is never an authoritative empty result.

| Reader | Reads | How |
| --- | --- | --- |
| `dispatch`, stage 4 | The item it dispatches, its admission in state `admitted` and the accepted specification revision; the Imperative by `render` | `read` with kinds `item`, `admission`, `specification`; `hive-imperative render` |
| `spawning-pool`, stage 3 | Nothing by call: the dispatcher passes `task_id`, `generation` and the requested place in each request | |
| `shadow` | Active holds with an `until`, questions with a revisit date, rules with an expiry time, each due at or before an instant | `read` with `due_before` |
| `ui` | Items with their derived facts, holds, questions and rounds, rules in force and recently expired, projections | `read` by query |
| `word` | Questions, rounds, holds and items, and the recorded outcome of its own earlier request | `read`, including kind `request-outcome` |

Kind `request-outcome` returns the recorded response of one earlier mutating request of this store by `request_id`, so that a caller without a store of its own can learn whether its request already completed before it mints identities for a retry. It is a read of the store's own replay record and returns nothing for a request not recorded.

## Receipts and events

The thirty-five receipt kinds are registered in [`packages/pool/hive-package.json`](../packages/pool/hive-package.json), each with the one fact it asserts. Every one has an empty body: the fact is carried by the receipt's `subject`, the record at the revision the fact holds for, and its `evidence`. Each is written inside the transaction that establishes its fact, at most once per request, kind and subject.

The Pool registers these event kinds, each with an empty body and a `basis` naming the receipt that establishes it:

`pool.item.added`, `pool.item.transitioned`, `pool.hold.placed`, `pool.hold.released`, `pool.generation.opened`, `pool.generation.closed`, `pool.admission.checked`, `pool.imperative.revised`, `pool.specification.accepted`, `pool.specification.invalidated`, `pool.specification.closed`, `pool.rule.recorded`, `pool.rule.amended`, `pool.rule.expired`, `pool.rule.lifted`, `pool.projection.rendered`, `pool.question.held`, `pool.question.answered`, `pool.question.deferred`, `pool.question.completed`, `pool.legacy.imported`, `pool.move.completed`.

`pool.admission.checked` is one event for the three check receipts; `pool.question.answered` is one event for an accepted, unreconciled or superseding answer. An event is appended in the transaction of its receipt, after the records and receipts, and its scope and `task_id` are the subject's.

## Error codes

| Code | Status | Meaning |
| --- | --- | --- |
| `pool.state_transition` | `refused` | The transition is not in the table. |
| `pool.blocked` | `refused` | A `blocked-by` edge names an item that is not done. |
| `pool.dependency_missing` | `refused` | A new `blocked-by` edge names an item that does not exist. |
| `pool.dependency_active` | `refused` | An item cannot be archived or moved while a dependent's edge names it. |
| `pool.split_dependency` | `refused` | A move would strand a `blocked-by` edge across two homes. |
| `pool.moving` | `refused` | The item is in a prepared move. |
| `pool.generation_open` | `conflict` | A generation is already open, or must be closed first. |
| `pool.not_dispatchable` | `refused` | A decision item cannot open a generation. |
| `pool.specification_unaccepted` | `refused` | The item requires an accepted specification and has none. |
| `pool.criteria_mismatch` | `refused` | The Imperative's Standard names a revision other than the item's. |
| `pool.admission_stale` | `conflict` | The admission's last check predates a change of the item or the Imperative. |
| `pool.admission_guess` | finding only | A typed field is a guess; the check is `held`. |
| `pool.authority_required` | finding only | A sensitive boundary lacks authority; the check is `held`. |
| `pool.imperative_stale` | `refused` | The Imperative's placement differs from the item's. |
| `pool.setup_contradiction` | `refused` | Harness and place, or Setup and Boundary, contradict. |
| `pool.placement_parked` | `refused` | The requested place is `codex-void` while the backend is parked. |
| `pool.placement_local_only` | `refused` | A Void place for a local-only project. |
| `pool.delivery_mismatch` | `refused` | The Output's validation or publication differs from the item's delivery. |
| `pool.publication_needs_forge` | `refused` | A Harvest is asked of a local-only project. |
| `pool.rule_not_expired` | `refused` | `expire` before the expiry time. |
| `pool.rule_conflict` | reported, not a status | Two rules govern one subject at one scope; `resolve` lists it and returns no digest. |
| `pool.legacy_malformed` | `invalid` | The file is not a legacy backlog the grammar accepts. |
| `pool.legacy_not_round_trip` | `refused` | The parsed file does not render back byte for byte, so it is not imported. |
| `pool.fleet_not_a_task` | `refused` | A request tries to add a persistent supervisor as an item. |

The common codes of [commands.md](commands.md#common-error-codes) apply unchanged: a held item is `held` with code `held` and the hold in `error.hold`; a revision mismatch is `revision_conflict`; a missing record is `not_found`.

## What `test-pool` proves

Beyond the design's row in [test-targets.md](test-targets.md): the byte-exact legacy round trip and the identity migration; every transition of the table, allowed and refused; a hold surviving completion; an admission run with each requested place, `codex-void` refused, a Void place refused for a local-only project and held for an unpushed branch, then admitted on pushed evidence; criteria carried from Pool to Imperative by revision and an invalidation clearing them; a render deterministic across runs; a home inside a repository refused; a persistent Fleet entry excluded; no operation that assumes a worktree, a session or a clone; an answer idempotent by source and unreconciled against a changed question; a move recovered after a crash at each of its three steps; a rule excluded by the clock before its receipt and a lift honoured by its named receipt; a projection marked stale by a rule change; every multi-record transaction rolled forward from its intent; and two writers under the lock with no lost update. Every test creates its home under a temporary directory and takes the clock as an input.

## Decisions taken here

Where the design was silent, this file reads it as follows. Each is listed with its alternative in the stage's definition summary.

1. The design's five-step passage is `state` and `definition` read together, so the legacy three states keep their transitions unchanged.
2. Holds are records of their own, not fields of the item.
3. A generation is an entry of the item, not a record of its own: it has no identity but the item's and its number.
4. The Imperative is keyed by its item, one record with all revisions.
5. The admission check is repeatable and every check is kept; `approve` records a ruling and the caller checks again.
6. The place and the branch's state are evidence the caller supplies; the Pool runs no Git.
7. The questions and rounds of the Mind's word are Pool records with `pool.` kinds, written by `hive-pool`; see [word.md](word.md).
8. The move is three transactions joined by receipts, destination first, as the upstream orders its two files.
9. The rule line is supplied by the recorder; the resolver appends the source. Conflicts are reported, never resolved by the store.
10. A legacy kind word is kept verbatim and never mapped to a deliverable or class.

## Left undecided

| Open point | What settles it |
| --- | --- |
| The maintenance queue of generated knowledge and what stow enqueues | Stage 4, which builds stow |
| How admission accepts a `posture`, `charter` or `instruction` authority for a sensitive boundary | Stage 4, which owns those records; a minor change adding the route |
| Which fact kinds beyond rules a projection renders, and the budgets `check` applies | The stage that owns each fact; budgets are configuration |
| Retention of recorded request outcomes, closed migrations and completed moves | Configuration of the Pool, as decisions.md leaves it |
| The exact text layout of a rendered Imperative beyond the order and headings fixed above | The build, under the determinism rule; it is not a contract between packages |
