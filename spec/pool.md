# The Pool

The Pool is the durable work queue of a private home and the one authoritative store that admission, specifications, rules, the Mind's word and task generations share, with one writer per transaction. This file is the contract of the `pool` package for stage 2 of the [ordered implementation plan](../docs/design.md#ordered-implementation-plan): where its records live, what each holds, what every operation does and refuses, how the execution place enters the contracts, how a legacy backlog comes in and goes out, and what `test-pool` proves. The decision intake that persists through it is in [word.md](word.md); the import of its upstream is planned in [import-plan-tasks-axi.md](import-plan-tasks-axi.md).

Owner: `pool`. Schemas: the ten records under [`schemas/records/`](schemas/records/) whose names begin `pool.`, and the operation bodies under [`schemas/ops/`](schemas/ops/) whose names begin `hive-pool.`, `hive-admit.`, `hive-imperative.`, `hive-spec.`, `hive-rule.` and `hive-memory.`. Proved by `test-contract` for the shapes and by `test-pool` for the behavior.

Nothing in this stage dispatches. The Pool records the place requested for an item's work and never assumes that the work has a worktree, a session or a clone.

## The store

Everything the Pool owns is under `pool/` in the home, in the five places [records.md](records.md#the-home) gives every package, and nowhere else:

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
| `pool/records/question/<question_id>.json`, `pool/records/decision-round/<decision_round_id>.json` | What the Mind's word persists: see [word.md](word.md). |
| `pool/receipts/`, `pool/requests/`, `pool/tx/` | As [records.md](records.md#the-home) fixes them. |
| `pool/tmp/<request_id>.tmp` | The temporary file of a write in progress, as records.md fixes it. The next writer that takes the store's lock empties it. Nothing of the Pool's is read from it: a file here is never read as a record, a receipt, an intent or an outcome. |

- **One writer.** `hive-pool` and its five sibling command names are one executable, and every write to `pool/` is one transaction of that executable under `locks/pool.lock`, as [records.md](records.md#writing-several-records) fixes. No other package writes under `pool/`, and `word` least of all: it calls.
- **The Mind's word.** `hold-question`, `answer-question` and `complete-question`, the three operations that persist the Mind's word, refuse at the Pool, with `authority_insufficient`, any request that is not made by the `hive-word` command on a `hive-word` request: the caller names the package `word` and its command `hive-word`, and the `causation` is a `hive-word` request of the request's own `request_id`, with the digest of its canonical form, which the Pool records with the request. An answer is recorded with its required `source`, which every read returns. This is attribution, as [word.md](word.md#what-this-does-not-prevent) says, not authentication.
- **One reader.** Only `pool` reads under `pool/`. Every other package reads the Pool through `hive-pool read`, which returns whole records as a [projection](#reading-the-pool).
- **Never inside a repository.** Every `hive-pool` command refuses a home inside a Git working tree with `home_inside_repository` before it reads anything, as every command does. The Pool therefore cannot write into a project's code root, and `test-pool` proves it for the import, which is the one operation given a path that may lie inside a repository: that path is read and never written.
- **No secret value.** No record, receipt, event, intent or recorded outcome under `pool/` holds a secret value. Where a record must name a secret, it holds a `secret_ref` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json): the secret's store, its host, the account identity and its purpose, all four and nothing else. The reference is neither the value nor a transferable authorization: holding one grants nothing, and the value is retrieved from the store at run time by whoever is entitled to it. The Imperative's `access.credentials` is the one place a credential is named, and the schema admits only references there. No field is a place for a value: a request whose text carries a value that Hive's secret-handling contract recognizes is refused whole, before anything is persisted, and nothing of it is applied. An answer is never redacted or rewritten; it is refused and given again. A value later found in a record is a found copy, handled as that contract says, by a sanitized incident that waits for the Hive Mind's word. How a value is recognized is that contract's, left undecided below. A request that puts a value where a reference belongs is `invalid`, and free text without one passes unchanged.

## Identifiers

Stage 2 registers six prefixes, each minted by `pool`:

| Prefix | Identifies |
| --- | --- |
| `adm_` | an admission record |
| `spec_` | a specification |
| `rule_` | a rule |
| `hold_` | a hold on an item |
| `mem_` | a knowledge projection |
| `migr_` | a legacy migration |

Their patterns are the `$defs` of [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json). The table in [identities.md](identities.md#identifier-form) and the registry in `contract/ids` are kept in step by a test of `test-contract`, so the rows are added there, with the constants, by the first work item of the build and not by this definition. The Imperative has no prefix of its own: it is keyed by its item. A generation, a criterion, an option and an answer are numbered or slugged inside their records and have no prefix.

The `q_`, `dr_` and `card_` identifiers of a question, a decision round and a card are minted by `word`, as identities.md says, and written by `pool`.

## Items

An item is one unit of work: a Swarm's change, a Lictor's report, or a decision held for the Hive Mind. It describes work, never a persistent agent, and a Fleet is never an item. Schema: [`records/pool.item.v1`](schemas/records/pool.item.v1.schema.json).

### State

`state` is one of `queued`, `in-flight`, `done` and `archived`. The first three are the three sections of the legacy backlog and keep its transitions; `archived` is a retired item, never a deleted one.

`definition` is kept beside `state`: `not-required`, `needed`, `defining` or `accepted`. The design's five-step passage is the two read together: *definition-needed* and *defining* are queued items with those definitions, *accepted* is a queued item whose definition is accepted, *building* is in-flight and *delivered* is done. Holds are kept apart from both, as separate records.

| From | To | Through | Refused when |
| --- | --- | --- | --- |
| `queued` | `in-flight` | `open-generation`, or `transition` | an active hold stands: `held`, naming it; a `blocked-by` edge names an item of this Pool that exists and is not done: `pool.blocked`; `specification_required` and `definition` is not `accepted`: `pool.specification_unaccepted`; `deliverable` is `decision`: `pool.not_dispatchable` |
| `queued`, `in-flight` | `done` | `transition` | a generation is open: `pool.generation_open` |
| `done` | `queued`, `in-flight` | `transition` (reopen) | as for the state entered |
| any but `archived` | `archived` | `transition` | an item not done has a dependent whose `blocked-by` edge names it and is itself not done or archived: `pool.dependency_active`; a generation is open: `pool.generation_open` |
| `archived` | anything | | always: `pool.state_transition` |

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
| `codex-void` | Codex in the Void: a named value that admission refuses with `pool.placement_parked`, unconditionally at stage 2. It is never substituted and never silently changed. |

**How parked is known.** At stage 2 the refusal of `codex-void` is written into `hive-admit check` itself: no rule record, policy request, configuration value, environment value, authority reference or `approve` can admit it, and `test-pool` proves that a check with each of those present still refuses. The design fixes what un-parks it, in [Codex: three backends and a migration boundary](../docs/design.md#codex-three-backends-and-a-migration-boundary) and [Enabling and retiring a Codex backend](../docs/design.md#enabling-and-retiring-a-codex-backend): a supported command-line route that honors model selection, a controlled replay of the full readiness contract, and the Hive Mind's recorded, authorized configuration change, with "an accepted default-model task" unable to release the parked state. In this specification that is one change, made by the stage that builds the Codex Void backend behind the adapter contract, which is stage 4's or later: a minor change of this file and of the check that replaces the unconditional refusal with a read of that backend's capability record, which stage 4 defines, in state ready, and the Hive Mind's ruling by reference as that record names it. Until that change is accepted and built, every `codex-void` request is refused, whatever else a home holds.

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

- **`define`** writes revision 1 of a new specification or the next revision of an existing one, whole, with the criteria whose identity or text changed listed in `changed_criteria`. Items named in `task_ids` move to `definition` `defining`. Invalidation is decided per binding, not from the last acceptance: `define` finds every item whose `specification` names this specification, whichever revision it is bound to, and compares the new revision's criteria, by identity, text and evidence, with the criteria of the revision that item is bound to. Where they differ, that item's acceptance is marked `invalidated`, the item loses its `specification`, returns to `defining` and receives a `definition` hold; an item bound to a revision whose criteria the new one leaves unchanged keeps its binding. Nothing else of those items changes, so a worker's target is never silently changed. `changed_criteria` lists the difference from the revision before and is information: invalidation does not read it, so a revision that changes nothing against the revision before it still invalidates an item bound to an earlier revision it differs from. Receipts: `pool.specification.defined`, and `pool.specification.invalidated` for each acceptance invalidated.
- **`accept`** appends the acceptance of the current revision for one item by the supervisor that called, one acceptance per item: the Fleet's for its workspace, the Hive's for its own. It binds the revision by identity, revision and digest into the item's `specification`, sets the item's `definition` to `accepted`, releases its `definition` hold, and lists the open product questions, each a question of this Pool that is still open; an item with such a question stays held by its `word` hold, because technical acceptance settles no product choice. Receipt: `pool.specification.accepted`.
- **`close`** records, after a verified landing, that the accepted requirements were met: the landed head, the landing receipt, the proof receipts and the resolved findings the caller names, bound to the accepted revision, item and generation. It is one transaction; if it does not commit, the landing stands and the closure stays pending for replay under the same request, and nothing reports the requirements landed because the code merged. Receipt: `pool.specification.closed`. Folding the accepted delta into a standing specification is a `define` of that specification that cites the closure.

`hive-admit check` and `hive-imperative render` read the specification through the Pool's own store; no other package reads it but through `hive-pool read`.

## Rules

The rule store separates original words from their operational interpretation and keeps both. Schema: [`records/pool.rule.v1`](schemas/records/pool.rule.v1.schema.json).

A rule carries its `subject` (a fixed key for what it governs), the `words` verbatim, its `origin`, its scope from the envelope (Hive, workspace, project, and a task generation where it rides an Imperative revision), the roles it `applies` to, one `rule_line` supplied by the recorder, its `effective_at`, its applying `authority` with the intake receipt, and for a temporary rule an `expires_at`, a `lift` condition naming a receipt kind and subject, or both. A temporary rule is this same record and no second mechanism.

- **`record`** appends a rule and writes `pool.rule.recorded`. The words and dates given are never changed afterwards. A rule that `changes` a standing rule names it by identity and revision with the relation `supersedes` or `qualifies`; the standing rule is never edited, nothing is recorded on it and its status does not change. The relation is read at each resolution and is never a retirement: see `resolve`.
- **`amend`** appends an attributed amendment of the rule's operation: `extend` or `end` with a new expiry time, `lift` with the receipt that met the lift condition, which must be of the kind and about the subject the condition named, or `note`. A supersession is not an amendment; to retire a standing rule for good, `end` it or record a standing rule that supersedes it. The original `expires_at` stays and the latest amendment's `new_expires_at` is what the resolver uses. Receipt: `pool.rule.amended`, or `pool.rule.lifted` for a lift.
- **`expire`** records that the expiry was noticed and honoured, with the time it was noticed; before the expiry time it is refused with `pool.rule_not_expired`, and a second call returns the first receipt. Receipt: `pool.rule.expired`. The receipt records the noticing; the exclusion happened by the clock.
- **`resolve`** computes, read-only, the rules effective for the envelope's scope, a role and an instant: it collects rules in force at `at` whose scope is the Hive, the workspace, the project where the scope names one, and the task generation where the request names one; drops those that do not apply to the role, are not yet effective, are lifted or ended, or are past their expiry time by the clock, whether or not an expiry receipt exists, and so leaves the **live set**; then drops a rule of the live set that a rule of the live set names with `supersedes`, so that a superseding rule retires the one it supersedes only while it is itself in force for this scope, role and instant: a temporary rule that has expired, has been lifted or is not yet effective leaves the standing rule in force; applies inheritance by letting the narrower scope govern a subject the wider also governs; and reports a **conflict** for a subject two rules govern at one scope when neither names the other in `changes`: a rule that `qualifies` another is in force beside it and is no conflict. It returns the rules in order, each as its rule line with the source and revision appended, the conflicts, what was excluded and why, and the **resolved-rule digest**, the digest of the canonical form of the rules array, present only when there is no conflict. A caller whose action depends on a conflicted subject holds that action; the resolver decides nothing.
- Expired and lifted rules, archived words and abandoned trials stay readable and are not loaded as current policy. A missing rendered field is not proof that a rule was retired.

The Shadow reads rules with an expiry through `hive-pool read` with `due_before`, and the UI's Scheduled view reads them the same way, with their receipts.

## Generated knowledge

`hive-memory` derives instructions from the effective rules and each fact's canonical owner, and records what each consumer received. Schema: [`records/pool.projection.v1`](schemas/records/pool.projection.v1.schema.json). One projection exists per scope and audience (`shared`, `fleet`, `project`); a re-render is a new rendering of the same record.

- **`render`** resolves the rules for the scope at the given instant, renders the text, records the sources at the revisions read, the renderer version, the resolved-rule digest and the text's digest, and writes `pool.projection.rendered`. A rendering equal to the current one by digest writes nothing. At stage 2 the sources are rules; a later stage that owns another kind of fact adds it as a source kind by a minor change.
- **`check`** reports, against the budgets given, each projection's state, size and budget standing, its stale sources, rule lines duplicated between projections of one scope, and consumers whose received or acknowledged digest is not the current one. It repairs nothing. **Freshness is computed, never stored**, here and at `hive-pool read` of a projection, from the rules in force at that moment: the check resolves the rules for the projection's scope as `render` did, at the clock, and the projection is `fresh` only when they resolve to the rendering's `rules_digest` with no conflict. A rule added after the rendering, a temporary rule that has expired by the clock or become effective by it, a lift, an end and a changed revision each make it `stale` with no receipt and no writer having touched it; its stale sources are the rules whose revision differs, the rules now in force that it did not read, and the rules it read that are no longer in force.
- **`propagate`** records one consumer's receipt, or acknowledgment, of a rendering at a digest, each with its own receipt: `pool.projection.received`, `pool.projection.acknowledged`. A step already recorded is returned unchanged.
- A projection is `stale` whenever the rules in force no longer resolve to the digest it was rendered from; no writer marks it, so a rule written, expired or lifted cannot leave one fresh. A stale projection cannot authorize work; the consumer that refuses one is stage 4's, and reads freshness through `hive-pool read` or `check`.

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

**`export-legacy`** renders items in the legacy format and returns the text, its digest and size; it writes no file. With a `migration_id`, it emits the source's preamble, sections, free lines and entries in the source's order: an item unmodified since its import is emitted from its exact original lines, where unmodified means that the item is at the `record_revision` the import wrote and the holds on it are exactly the holds the import placed, each at the revision the import wrote: a hold placed, released, bound to a question or released by a word answer since the import makes the item modified, because the legacy form carries the hold in the entry's own tags; a modified item is re-rendered canonically; an excluded entry is emitted verbatim. `exact` is true when the text equals the source. Without a migration, or with `normalize`, every item the selector matches is rendered canonically, as the upstream's `render` verb does. Legacy words are rendered from the `legacy` member and Hive-only fields are not written into the legacy form. A canonical rendering writes hold tags for the item's active holds only, so a released hold is never written back.

`test-pool` proves the round trip on the two upstream fixtures re-expressed as Hive fixtures, on a file with every tag the table names, and on a file with carriage returns, a header suffix and an unrecognized section; it proves a persistent-supervisor entry excluded; and it proves that an import into a home inside a repository is refused and that an import never writes the source path.

## Reading the Pool

`hive-pool read` is the one way another package sees the Pool. It reads one kind by identity or by query, takes no lock for one record and the lock shared for a list, and returns every record whole with a **projection header**: the home, the time of the read, the journal cursor at that moment and whether the read was complete. A record that cannot be read is omitted and named, never guessed; an incomplete read says so and is never an authoritative empty result.

| Reader | Reads | How |
| --- | --- | --- |
| `dispatch`, stage 4 | The item it dispatches, its admission in state `admitted` and the accepted specification revision; the Imperative by `render` | `read` with kinds `item`, `admission`, `specification`; `hive-imperative render` |
| `spawning-pool`, stage 3 | Nothing by call: the dispatcher passes `task_id`, `generation` and the requested place in each request | |
| `shadow` | Active holds with an `until`, questions with a revisit date, rules with an expiry time, each due at or before an instant | `read` with `due_before` |
| `ui` | Items with their derived facts, holds, questions and rounds, rules in force and recently expired, projections | `read` by query |
| `word` | Questions, rounds, holds and items, and the recorded outcome of its own earlier request | `read`, including kind `request-outcome` |

Kind `request-outcome` returns the recorded outcome of one earlier mutating request of this store by `request_id`: the recorded response with the digest of the request and the request's causation as recorded. A caller without a store of its own compares the causation's digest with the digest of the request it holds, and only then returns the outcome as a retry's, before it checks anything against the present records or mints identities; `word` is that caller. It is a read of the store's own replay record and returns nothing for a request not recorded.

## Receipts and events

The thirty-one receipt kinds are registered in [`packages/pool/hive-package.json`](../packages/pool/hive-package.json), each with the one fact it asserts. Every one has an empty body: the fact is carried by the receipt's `subject`, the record at the revision the fact holds for, and its `evidence`. Each is written inside the transaction that establishes its fact, at most once per request, kind and subject.

The Pool registers these event kinds, each with an empty body and a `basis` naming the receipt that establishes it:

`pool.item.added`, `pool.item.transitioned`, `pool.hold.placed`, `pool.hold.released`, `pool.generation.opened`, `pool.generation.closed`, `pool.admission.checked`, `pool.imperative.revised`, `pool.specification.accepted`, `pool.specification.invalidated`, `pool.specification.closed`, `pool.rule.recorded`, `pool.rule.amended`, `pool.rule.expired`, `pool.rule.lifted`, `pool.projection.rendered`, `pool.question.held`, `pool.question.answered`, `pool.question.deferred`, `pool.question.completed`, `pool.legacy.imported`.

`pool.admission.checked` is one event for the three check receipts; `pool.question.answered` is one event for an accepted, unreconciled or superseding answer. An event is appended in the transaction of its receipt, after the records and receipts, and its scope and `task_id` are the subject's.

## Error codes

| Code | Status | Meaning |
| --- | --- | --- |
| `pool.state_transition` | `refused` | The transition is not in the table. |
| `pool.blocked` | `refused` | A `blocked-by` edge names an item that is not done. |
| `pool.dependency_missing` | `refused` | A new `blocked-by` edge names an item that does not exist. |
| `pool.dependency_active` | `refused` | An item cannot be archived while a dependent's edge names it. |
| `pool.generation_open` | `conflict` | A generation is already open, or must be closed first. |
| `pool.not_dispatchable` | `refused` | A decision item cannot open a generation. |
| `pool.specification_unaccepted` | `refused` | The item requires an accepted specification and has none. |
| `pool.criteria_mismatch` | `refused` | The Imperative's Standard names a revision other than the item's. |
| `pool.admission_stale` | `conflict` | The admission's last check predates a change of the item or the Imperative. |
| `pool.admission_guess` | finding only | A typed field is a guess; the check is `held`. |
| `pool.authority_required` | finding only | A sensitive boundary lacks authority; the check is `held`. |
| `pool.imperative_stale` | `refused` | The Imperative's placement differs from the item's. |
| `pool.setup_contradiction` | `refused` | Harness and place, or Setup and Boundary, contradict. |
| `pool.placement_parked` | `refused` | The requested place is `codex-void`, which stage 2 refuses unconditionally; see [the execution place](#the-execution-place). |
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

Beyond the design's row in [test-targets.md](test-targets.md): the byte-exact legacy round trip and the identity migration; every transition of the table, allowed and refused; a hold surviving completion; an admission run with each requested place, `codex-void` refused, a Void place refused for a local-only project and held for an unpushed branch, then admitted on pushed evidence; criteria carried from Pool to Imperative by revision and an invalidation clearing every item bound to a revision the new one differs from, including an item bound to an earlier revision than the last one accepted; a render deterministic across runs; `codex-void` refused with a rule, a policy request, an approval and an authority reference all present; a home inside a repository refused; a persistent Fleet entry excluded; no operation that assumes a worktree, a session or a clone; an answer idempotent by source, unreconciled against a changed question, refused when it holds no option, no deferral and no text, and refused by the three word operations from any caller but `hive-word` on a `hive-word` request; a request with a value where a reference belongs `invalid` and free text without one passing unchanged; a rule excluded by the clock before its receipt and a lift honoured by its named receipt; a temporary rule that supersedes a standing rule, restoring the standing rule when it expires, is lifted or has not yet begun, and a qualifying rule beside the rule it qualifies with no conflict; a projection stale by the rules now in force with no writer having marked it, after a rule is added, after a temporary rule expires by the clock and before a future rule begins; an export of an imported item whose hold was released or answered re-rendered without the hold, never emitted from the original lines; every multi-record transaction rolled forward from its intent; and two writers under the lock with no lost update. Every test creates its home under a temporary directory and takes the clock as an input.

## Decisions taken here

Where the design was silent, this file reads it as follows. Each decision is followed by the alternative it rejected.

1. The design's five-step passage is `state` and `definition` read together, so the legacy three states keep their transitions unchanged; the alternative, one five-valued state, would make the legacy import map a queued item to a guessed definition state.
2. Holds are records of their own, not fields of the item; holds as fields could not be named by `error.hold` or a wake and would vanish with the item's archive.
3. A generation is an entry of the item, not a record of its own: it has no identity but the item's and its number; a `pool.generation` record with a composite identifier would be one more file per launch with no second reader.
4. The Imperative is keyed by its item, one record with all revisions; an `imp_` identity would be one more prefix with no second reader.
5. The admission check is repeatable and every check is kept; `approve` records a ruling and the caller checks again; one check per admission would need a new admission for every re-check at a launch path.
6. The place and the branch's state are evidence the caller supplies; the Pool runs no Git; the Pool reading the clone would give it a core dependency and a worktree assumption.
7. The questions and rounds of the Mind's word are Pool records with `pool.` kinds, written by `hive-pool`; see [word.md](word.md); `word.` kinds written by `pool` would break the rule that a package never writes a name that begins with another's.
8. Moving items between homes is deferred to the stage that builds routing, which defines it on the upstream's multi-item move as its base; see Left undecided. The `handoff` field of an item stays as the stage-4 route's way to hand one item to a child Fleet.
9. The rule line is supplied by the recorder; the resolver appends the source. Conflicts are reported, never resolved by the store; a line derived from the words would be an interpretation the store makes on its own.
10. A legacy kind word is kept verbatim and never mapped to a deliverable or class; a mapping table would invent intake decisions.
11. One `read` operation on `hive-pool` serves every Pool record kind, with a projection header; a read operation per command would be ten more schema pairs of the same shape.
12. Every receipt has an empty body, the subject at its revision and the evidence carrying the fact; body schemas would be thirty-one more files repeating the records.
13. The six new prefixes are declared in this file and in the `$defs` of `records/pool.item.v1`, and registered in `identities.md` and `contract/ids` by the first work item of the build in one commit, because `test-contract` holds the table and the registry in step and the definition may not edit `contract/`; adding the rows in the definition would have failed `test-contract`.
14. A `check` that refuses or holds returns no body, with its findings in the record and the receipts, so that no non-`ok` response carries a partial result; a body on refusal would be the partial result the command contract forbids.
15. The plan's order inside the stage, read-only import first, round trip and crash recovery proved, then one writer selected, is read as an order of proofs, not a calendar: the grammar and the read-only import start at once and touch no store, the legacy writer waits on the round trip and on the store, the one writer is the store under one lock with roll-forward, proved before the stage is accepted, and the contracts of stages 3 and 4 land first because nothing in them writes from a legacy file; a strictly serial stage would delay those contracts by the length of the translation and gain nothing, since the grammar and the store share no code.
16. `codex-void` is refused unconditionally at stage 2, in the check itself, and un-parked only by the later minor change [the execution place](#the-execution-place) names; a parked flag in a rule or configuration record could be flipped by a value, which the design's rule that a parked backend is refused and never substituted rules out.

## Left undecided

| Open point | What settles it |
| --- | --- |
| The maintenance queue of generated knowledge and what stow enqueues | Stage 4, which builds stow |
| How admission accepts a `posture`, `charter` or `instruction` authority for a sensitive boundary | Stage 4, which owns those records; a minor change adding the route |
| Which fact kinds beyond rules a projection renders, and the budgets `check` applies | The stage that owns each fact; budgets are configuration |
| Retention of recorded request outcomes and closed migrations | Configuration of the Pool, as decisions.md leaves it |
| The exact text layout of a rendered Imperative beyond the order and headings fixed above | The build, under the determinism rule; it is not a contract between packages |
| How a value is recognized, in free text or in a preserved `unknown` member, so that the request is refused whole before any byte of it reaches disk | Hive's secret-handling contract, which is being written into the design now and changes the record and command contracts by their own rule |
| Whether an answer is authenticated as the Hive Mind's against other processes of the same user, and by what | The Hive Mind's ruling; see [word.md](word.md#what-this-does-not-prevent) |
| Verification of a session-input source of an answer against its input receipt | The stage that builds session input; see [word.md](word.md#left-undecided) |
| Moving items between homes | The stage that builds routing, on the upstream's multi-item move as its base. That stage settles three things: an abort that both homes agree on, and when the destination's copy becomes the active one; which records travel with an item so that it works in its new home, namely its questions and rounds, admissions, accepted specification, Imperative and generation history, migration identity, dependency edges and links; and how a receipt, which is private to its home, is evidenced across two homes |
| Whether a command offers a short form at a terminal | The stage that builds the UI, in its review; the default it starts from is no short form: the request document is the one form |
| How an imported hold with no question is shown | The UI stage's review; the default is that it is shown as a hold on its item and becomes a card only when a question is raised for it |
| Where a decision item is shown | The UI stage's review; the default is that it appears in the Pool as a held item and its card among the waiting decisions |
| The labels of the placement values | The UI stage's review; the defaults are Mac, Claude cloud session, By policy and Codex Void (parked) |
