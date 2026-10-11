# Stage 2 definition: the Pool

For the reader who accepts this definition or sends it back. Everything it defines is specification under `spec/` on this branch; nothing is implemented, imported or installed.

## 1. The objective

Stage 2 gives Hive its durable work queue and its one decision intake, built on the accepted stage 1 contracts. When the stage is built, a private home holds one Pool in which items, holds, generations, admission records, Imperatives, specifications, rules, knowledge projections and the Mind's word share one store with one writer per transaction; a legacy Markdown backlog imports with a byte-exact round trip and an explicit identity migration and exports again, as a format and never as a second queue; admission checks the Imperative's single schema and records receipts bound to its revision; accepted criteria are carried from the Pool item into the Imperative's Standard by revision and never copied; temporary rules expire by the clock and lift by receipt; and the execution place enters the contracts: the Pool item records the place requested for it, on the Mac, in a Claude cloud session, or left to policy, with Codex Void a named value admission refuses while it is parked, the Imperative's Setup renders that Placement axis from the item, and admission refuses a Void placement for a local-only project and holds one whose source branch is not pushed. Nothing dispatches, and the Pool never assumes that an item's work has a worktree. Every lane of this stage is judged by whether its part of that sentence is built, proved by its test target, and readable by stages 3 and 4, the Shadow and the UI without a further design decision.

## 2. What is defined

| File | Holds |
| --- | --- |
| `spec/pool.md` | The Pool's contract: store, records, operations, the execution place, admission, the Imperative, specifications, rules, generated knowledge, legacy import and export, moves, projections, receipts, events, error codes, the secrets rule, proofs, decisions, open points. |
| `spec/word.md` | The Mind's word: the intake, how it persists through the Pool, replay, the answer's checks and dispositions, verify, error codes, proofs, decisions, open points. |
| `spec/import-plan-tasks-axi.md` | The import plan for tasks-axi, field by field of the record the build writes. |
| `spec/README.md` | Edited: the index gains the three files; contract version 1.2. |
| `spec/test-targets.md` | Edited: the `test-pool` and `test-word` rows gain the stage 2 proofs. |
| `spec/receipts.md`, `spec/events.md` | Edited: a sentence each pointing at the stage 2 registrations. |
| `spec/decisions.md` | Edited: the open point "the Pool's own records" is marked settled. |
| `packages/pool/hive-package.json` | Amended: 31 operations across the six commands, 35 receipt kinds, the stage 2 test scope. |
| `packages/word/hive-package.json` | Amended: the ownership statement and the stage 2 test scope. |
| `spec/schemas/records/pool.item.v1.schema.json` | The item, and the definitions every Pool record shares: prefixes, placement, delivery axes, holds, secrets by reference, generations. |
| `spec/schemas/records/pool.hold.v1.schema.json` | A hold on an item. |
| `spec/schemas/records/pool.admission.v1.schema.json` | An admission request with every check made against it. |
| `spec/schemas/records/pool.imperative.v1.schema.json` | The canonical Imperative: the four sections, the typed fields, the Placement axis, deliveries. |
| `spec/schemas/records/pool.specification.v1.schema.json` | A specification with revisions, acceptance and closures. |
| `spec/schemas/records/pool.rule.v1.schema.json` | A rule, verbatim, with amendments, expiry and lift; the resolved rule and the conflict. |
| `spec/schemas/records/pool.projection.v1.schema.json` | A knowledge projection with its sources, digest and per-consumer propagation. |
| `spec/schemas/records/pool.migration.v1.schema.json` | A legacy migration with the source text and every entry's exact lines. |
| `spec/schemas/records/pool.move.v1.schema.json` | A move between homes and its steps. |
| `spec/schemas/records/pool.question.v1.schema.json` | A question for the Hive Mind with its answers, exactly as given. |
| `spec/schemas/records/pool.decision-round.v1.schema.json` | A decision round with its cards. |
| `spec/schemas/ops/hive-pool.add.{request,response}.v1.schema.json` | Add an item. |
| `spec/schemas/ops/hive-pool.read.{request,response}.v1.schema.json` | Read any Pool record kind as a projection, with derived item facts and the recorded outcome of a request. |
| `spec/schemas/ops/hive-pool.update.{request,response}.v1.schema.json` | Patch an item and report what changed. |
| `spec/schemas/ops/hive-pool.transition.{request,response}.v1.schema.json` | Move an item between states. |
| `spec/schemas/ops/hive-pool.open-generation.{request,response}.v1.schema.json` | Open a generation under an admitted admission. |
| `spec/schemas/ops/hive-pool.close-generation.{request,response}.v1.schema.json` | Close a generation with its outcome. |
| `spec/schemas/ops/hive-pool.hold.{request,response}.v1.schema.json` | Place a parked, external, resource or future hold. |
| `spec/schemas/ops/hive-pool.release.{request,response}.v1.schema.json` | Release such a hold. |
| `spec/schemas/ops/hive-pool.move.{request,response}.v1.schema.json` | One step of a move between homes. |
| `spec/schemas/ops/hive-pool.import-legacy.{request,response}.v1.schema.json` | Import a legacy backlog, dry run or with the migration. |
| `spec/schemas/ops/hive-pool.export-legacy.{request,response}.v1.schema.json` | Export in the legacy form. |
| `spec/schemas/ops/hive-pool.hold-question.{request,response}.v1.schema.json` | Persist a question, its card and the word hold, for `hive-word hold`. |
| `spec/schemas/ops/hive-pool.answer-question.{request,response}.v1.schema.json` | Persist and apply an answer, for `hive-word answer`. |
| `spec/schemas/ops/hive-pool.complete-question.{request,response}.v1.schema.json` | Complete a question, for `hive-word complete`. |
| `spec/schemas/ops/hive-admit.create.{request,response}.v1.schema.json` | Record an admission request immutably. |
| `spec/schemas/ops/hive-admit.check.{request,response}.v1.schema.json` | Run the admission checks, including the place and the branch. |
| `spec/schemas/ops/hive-admit.approve.{request,response}.v1.schema.json` | Record a required ruling. |
| `spec/schemas/ops/hive-imperative.create.{request,response}.v1.schema.json` | Write revision 1; Setup without placement, which the command copies from the item. |
| `spec/schemas/ops/hive-imperative.revise.{request,response}.v1.schema.json` | Write the next revision. |
| `spec/schemas/ops/hive-imperative.render.{request,response}.v1.schema.json` | Render the text and launch parameters, deterministically. |
| `spec/schemas/ops/hive-imperative.deliver.{request,response}.v1.schema.json` | Record what a generation was given. |
| `spec/schemas/ops/hive-spec.define.{request,response}.v1.schema.json` | Write a specification revision. |
| `spec/schemas/ops/hive-spec.accept.{request,response}.v1.schema.json` | Accept a revision for an item. |
| `spec/schemas/ops/hive-spec.close.{request,response}.v1.schema.json` | Close requirements against a landed head. |
| `spec/schemas/ops/hive-rule.record.{request,response}.v1.schema.json` | Record a ruling verbatim. |
| `spec/schemas/ops/hive-rule.amend.{request,response}.v1.schema.json` | Append an attributed amendment, lift or supersession. |
| `spec/schemas/ops/hive-rule.expire.{request,response}.v1.schema.json` | Record an expiry noticed. |
| `spec/schemas/ops/hive-rule.resolve.{request,response}.v1.schema.json` | Resolve the effective rules and digest. |
| `spec/schemas/ops/hive-memory.render.{request,response}.v1.schema.json` | Render a projection. |
| `spec/schemas/ops/hive-memory.check.{request,response}.v1.schema.json` | Report duplication, budgets, staleness and drift. |
| `spec/schemas/ops/hive-memory.propagate.{request,response}.v1.schema.json` | Record a consumer's receipt or acknowledgment. |
| `spec/schemas/ops/hive-word.hold.{request,response}.v1.schema.json` | Raise a question and hold the work. |
| `spec/schemas/ops/hive-word.answer.{request,response}.v1.schema.json` | The one intake for an answer. |
| `spec/schemas/ops/hive-word.complete.{request,response}.v1.schema.json` | Complete a question. |
| `spec/schemas/ops/hive-word.verify.{request,response}.v1.schema.json` | Verify that the word stands. |

Each `{request,response}` row is two files. No stage 1 schema changed.

## 3. The contracts the dependent jobs read or write

All of them land in work item 1.

| Job | Reads or writes | Where |
| --- | --- | --- |
| Stage 3, the Spawning Pool | A task generation as the Pool records it: `task_id`, `generation`, `placement_requested`, open or closed. A Void generation holds no lease. It calls nothing; the dispatcher passes the identities. | `pool.item.generations`, `open-generation`, `close-generation` |
| Stage 4, session and metering | Writes: `hive-admit create` and `check` at every launch path, `approve`; `open-generation`, `close-generation`, `hive-imperative deliver`, `transition`, `hive-rule expire` from the Shadow's wake, `hive-rule record` for a policy request with an expiry, `hive-spec close` after landing. Reads: the item it dispatches, the admission in state `admitted`, the accepted specification revision, `hive-imperative render` for the text and launch parameters, `hive-rule resolve` for the digest. | `pool.admission`, `pool.imperative`, `pool.item`, `pool.rule`, the operations named |
| `word` | Mints `q_`, `dr_`, `card_`; persists through `hold-question`, `answer-question`, `complete-question`; reads through `read`, including kind `request-outcome`. | `spec/word.md` |
| `ui` | Items with derived facts, holds, questions, rounds, rules in force and recently expired, projections; answers only through `hive-word answer`. | `hive-pool read`, `hive-word answer` |
| `shadow` | Active dated holds, questions with a revisit date, rules with an expiry, due at or before an instant. | `hive-pool read` with `due_before` |

## 4. Left undecided

| Point | What settles it |
| --- | --- |
| The maintenance queue of generated knowledge and what a stow enqueues | Stage 4, which builds stow |
| How admission accepts a `posture`, `charter` or `instruction` authority for a sensitive boundary | Stage 4, which owns those records, by a minor change adding the route |
| Which fact kinds beyond rules a projection renders, and the budgets `check` applies | The stage that owns each fact; budgets are configuration |
| Retention of recorded request outcomes, closed migrations and completed moves | Pool configuration, as decisions.md leaves retention |
| The exact text layout of a rendered Imperative beyond the fixed order and headings | The build, under the determinism rule; not a contract between packages |
| Which answer `source` kinds the UI submits under beyond a review submission | Stage 6; `operator` serves the CLI now |
| Whether a supervisor may supersede an answer under a recorded posture | Stage 4, which defines postures |
| Whether the move between homes is what `hive-fleet route` uses or a handoff creates a child item | Stage 4; both the move and the `handoff` field are defined so either reading builds |

## 5. Decisions taken where the design was silent

Recommendation first, with its consequence; then the alternatives.

1. **The design's five-step passage is `state` and `definition` read together.** The legacy three states keep their proven transitions; definition-needed and defining are queued items. Alternative: one five-valued state, which would make the legacy import map queued to a guessed definition state.
2. **Holds are records of their own.** A response, a wake and a receipt can name one, and a hold survives completion. Alternative: holds as fields of the item, which cannot be referenced by `error.hold` and vanish with the item's archive.
3. **A generation is an entry of its item, not a record.** Its identity is the item's and its number, as the design numbers them; the dispatcher's execution record is the generation's own record at stage 4. Alternative: a `pool.generation` record with a composite identifier, one more file per launch.
4. **The Imperative is keyed by its item**, one record with every revision. Alternative: an `imp_` identity, one more prefix with no second reader.
5. **The admission check is repeatable and every check is kept**; `approve` records a ruling and the caller checks again. Alternative: one check per admission, which would need a new admission for every re-check at a launch path.
6. **The place and the branch's state are evidence the caller supplies.** The Pool runs no Git and executes nothing, as its declaration says; an unpushed or unknown branch holds. Alternative: the Pool reading the clone, which would give it a core dependency and a worktree assumption.
7. **The decision records are Pool records with `pool.` kinds; `word` mints their identities and writes nothing.** This keeps the rule that a package never writes a name that begins with another's, and `word` has no part of the home for receipts. Alternative: `word.` kinds written by `pool`, which needs a ruling on that rule.
8. **`word`'s replay is the Pool's**, by the shared request identity, with a read of the recorded outcome before any identity is minted. Alternative: a replay store in `word`, which the design forbids.
9. **The move is three transactions joined by receipts, destination first**, as the upstream orders its two files. Alternative: a single-home re-route through a child item, which the `handoff` field also supports; both are defined.
10. **The rule line is supplied by the recorder, and conflicts are reported, never resolved by the store.** Alternative: a derived line from the words, which would be an interpretation the store makes on its own.
11. **A legacy kind word is kept verbatim** and never mapped to a deliverable or class. Alternative: a mapping table, which would invent intake decisions.
12. **One `read` operation serves every Pool record kind**, with a projection header. Alternative: a read operation per command, ten more schema pairs with the same shape.
13. **Every receipt has an empty body**; the subject at its revision and the evidence carry the fact. Alternative: body schemas, thirty-five more files repeating the records.
14. **The seven new prefixes are declared in the Pool's schema and prose and registered in `identities.md` and `contract/ids` by the build**, in one commit, because `test-contract` holds the table and the registry in step and this definition may not edit `contract/`.
15. **The admission's `check` returns `refused` or `held` with no body** and its findings in the record and receipts, so a non-`ok` response carries no partial result, as the command contract requires.

Product rulings that need the operator, each with a recommendation:

- **Short forms at a terminal.** No command of stage 2 defines a flag or positional form; the request document is the one form. Recommendation: keep it so until a person types these commands often enough to ask.
- **An imported questionless word hold.** A legacy `captain` hold has no question. Recommendation: the UI shows it as a hold on the item and not as a card in Waiting on you until a supervisor raises its question through `hive-word hold`.
- **Decision items in the Pool view.** A question with its own scope is a held item with `deliverable` `decision`. Recommendation: it appears in the Pool as a held item and its card in Waiting on you, not in the Pool alone.
- **The words of the placement values.** `mac`, `claude-cloud-session`, `policy` and `codex-void` are record values; the task inspector will show them. Recommendation: the UI labels them "Mac", "Claude cloud session", "By policy" and "Codex Void (parked)".

## 6. The build plan

Each work item can be delivered alone and proved by its own targets. Lanes within an item run in parallel where their "waits on" allows; items 1 and 2 start together.

**Work item 1: land the Pool store and every contract the dependent stages read: items, holds, generations, admission, the Imperative, specifications, rules and the decision intake.** It delivers every record, operation, receipt and event that stages 3 and 4, the UI and the Shadow read or write, over the contract library alone, so that each later job can build against a real `hive-pool` and `hive-word` before the rest of stage 2 exists.

| Lane | Owns | Proves | Waits on |
| --- | --- | --- | --- |
| a | The seven prefix constants in `contract/ids/ids.go` with their rows in `spec/identities.md`; fixtures under `contract/testdata/schemas/` for the eleven records and seventy operation bodies; `packages/pool/internal/store/` (records, transactions, request outcomes, the read with its projection header, over `contract/`); `packages/pool/cmd/hive-pool/` with the five links and `capabilities`; `packages/pool/internal/item/` for `add`, `read`, `update`, `transition`, `hold`, `release`, `open-generation`, `close-generation` | `test-contract` for the fixtures and the declarations; `test-pool` for the items, holds and generations | Nothing |
| b | `packages/pool/internal/admission/` and `packages/pool/internal/imperative/`: `hive-admit create`, `check`, `approve`; `hive-imperative create`, `revise`, `render`, `deliver`; the project record read through the contract reader | `test-pool`: every placement rule, the four typed fields, the guess hold, the authority hold, deterministic rendering, criteria by revision | Lane a's store interface; starts on the schemas and integrates when a lands |
| c | `packages/pool/internal/specification/` and `packages/pool/internal/rule/`: `hive-spec define`, `accept`, `close`; `hive-rule record`, `amend`, `expire`, `resolve` | `test-pool`: invalidation clearing criteria, closure pending on failure, expiry by the clock before its receipt, lift by its named receipt, inheritance and conflicts, the digest | Lane a's store interface |
| d | `packages/word/cmd/hive-word/`, `packages/word/internal/` and `packages/pool/internal/decision/` for `hold-question`, `answer-question`, `complete-question` | `test-word` with a fake `hive-pool` from the declared schemas; `test-pool` for the three operations | Lane a's store interface for the Pool side; the `word` side starts at once |

**Work item 2: translate tasks-axi: read-only legacy import first, the byte-exact round trip, then the identity migration.** It brings the proven grammar, transitions, archive lookup and link recognition into the Pool as a recorded translation, read-only first so that the round trip is proved before the Pool writes anything from a legacy file.

| Lane | Owns | Proves | Waits on |
| --- | --- | --- | --- |
| a | `imports/tasks-axi.json`, `packages/pool/upstream/tasks-axi/LICENSE`, the root `NOTICE`, the `imports` entry of the pool declaration, written with or before the first translated file; `packages/pool/internal/legacy/grammar.go`, `normalize.go`, `archive.go`, `packages/pool/internal/item/links.go`; the translated tests `grammar_test.go`, `maintain_test.go`, `links_test.go`, `id_test.go`; fixtures under `packages/pool/testdata/legacy/` written for the purpose; the audit | `check-imports`; `test-pool` for the byte-exact round trip on every fixture | Nothing: the grammar is pure and touches no store |
| b | `packages/pool/internal/legacy/importer.go` in dry-run mode and `exporter.go`: `import-legacy` with `dry_run`, `export-legacy` | `test-pool`: the report, `round_trip_exact`, export verbatim and canonical, a persistent-supervisor entry excluded, a home inside a repository refused, the source never written | Lane 2a |
| c | The write path of `importer.go` and the `pool.migration` record; the translated tests `store_test.go`, `crud_test.go`, `state_test.go`, `derive_test.go` | `test-pool`: the identity migration, holds from tags, dependencies resolved to minted identities, a second import of one digest refused, an unmodified item exported from its exact lines | Lanes 1a and 2b |

**Work item 3: prove crash recovery, concurrent writers and the move between homes.** Every multi-record transaction of the Pool rolls forward from its intent and two writers never lose an update, and the one cross-home operation is proved at each of its steps; it is separate because it can be proved only once the operations exist.

| Lane | Owns | Proves | Waits on |
| --- | --- | --- | --- |
| a | `packages/pool/internal/store/recovery_test.go` and `concurrency_test.go` with a fault-injecting store harness, covering every operation of items 1 and 2 | `test-pool`: roll-forward after a crash at each step of each transaction, two writers under the lock, a malformed record stopping the mutation that needs it | Every lane of items 1 and 2 |
| b | `packages/pool/internal/move/move.go` and `move_test.go`: `hive-pool move` in its four steps | `test-pool`: identities preserved across two homes, a stranded dependency refused, a crash after each step recovered by replaying the next, abort at each step | Lane 1a |

**Work item 4: generated knowledge: render, check and propagate projections from the rule store.** Projections are derived from the effective rules, marked stale by a rule change and acknowledged per consumer; it stands alone because nothing of stages 3 or 4 reads them yet and they depend only on the rule store.

| Lane | Owns | Proves | Waits on |
| --- | --- | --- | --- |
| a | `packages/pool/internal/memory/`: `hive-memory render`, `check`, `propagate`; the stale marking in the rule and specification writers | `test-pool`: a projection rendered with its sources and digest, marked stale by a rule change, duplication and budget reports, receipt and acknowledgment per consumer, drift reported | Lane 1c |

**Work item 5: integration and distribution of the two packages.** The real `hive-word` to `hive-pool` call, every exit state through `word`, replay through both, the large record, the capabilities agreeing with the declarations and the two executables with their links in the artefact; it is separate because it runs against built executables and proves the calls, not the packages.

| Lane | Owns | Proves | Waits on |
| --- | --- | --- | --- |
| a | `integration/calls/word_pool_test.go`; `integration/calls/pool_callers_test.go` with test callers standing in for `dispatch`, `shadow` and `ui`; the capabilities-against-declaration check for both packages; the distribution checks for `hive-pool`, its five links and `hive-word` | `test-integration`, `test-distribution` | Lanes 1a and 1d |

## 7. The import

- **Upstream:** tasks-axi, `https://github.com/kunchenguid/tasks-axi`, release 0.2.6, tag `tasks-axi-v0.2.6`, commit `9401ff899c0d1d8ae6b4fd8727b9025abda2032c`, which was also the head of its main branch on 2026-10-11. Kind `translation`, owner `pool`.
- **Translated:** the Markdown grammar and its byte-exact round trip; normalization; the queued, in-flight, done transitions and reopen; dependency edges and the derived blocked, held and ready facts; holds with a date; the patch that reports changes; pull request and report link recognition; archive lookup for import; the multi-item move, re-expressed as three transactions between two homes. Eight upstream test files are re-expressed as translated tests, each naming its upstream file and cases; the two fixtures are re-expressed as Hive fixtures, not taken, unless a lane finds it must take one and then records it as an asset.
- **Left behind:** the public-followup state machine; the CLI, argument, output, suggestion, home, setup, skill and version files; configuration from flags, environment and files; the error type and the lock, replaced by the contract library; the data model and backend seam, replaced by the record schemas; identifier minting; the generated skill, the build, workflow and release files; documentation. The two runtime libraries, `@toon-format/toon` and `axi-sdk-js`, are replaced by the command contract and nothing of them is taken, so their licence files need not be established.
- **Notices to keep:** the one notice `Copyright (c) 2026 Kun Chen` and the MIT licence text, 1,065 bytes, digest `sha256:945016bd37e1ba7211622ef60ee1d23ab727896ba7710edd21e8fbe983863969`, kept unchanged at `packages/pool/upstream/tasks-axi/LICENSE` and reproduced in the root `NOTICE`. No file upstream carries a per-file header. The record, the kept licence and the `NOTICE` are written by work item 2, lane a, with or before the first translated file; `check-imports` holds the record to paths that exist, which is why this definition does not write it.

## 8. Contradictions and corrections

Nothing in the accepted stage 1 contracts had to change beyond the minor additions the change rule allows, and no sentence of `spec/imports.md` became untrue, so none was edited.

**Needs a ruling: stage 1 contract changes.** None.

Found and resolved by a reading, not a change:

- `identities.md` says `q_`, `dr_` and `card_` are minted by `word`, and `packages.md` says `word` keeps no store; a retry of `hive-word hold` would mint a second set of identities. Resolved by the read of the recorded outcome before minting, in word.md. If the operator would rather `pool` mint them, that is a one-row change of identities.md at stage 2's build.
- `packages.md` rule 3 says a package never writes a name that begins with another's, and the design says `word` persists through `pool` and speaks of "word's receipt". Resolved by making the decision records and receipts `pool.` kinds and reading "word's receipt" as the receipt `hive-word` returns. The response envelope's "every receipt this invocation wrote" is read, for `hive-word`, as the receipts of the one request it made.
- `identities.md` says a later stage registers a prefix by adding a row, and `contract/ids` is held to that table by `test-contract`; a definition that may not touch `contract/` cannot add the row. Resolved by declaring the prefixes in the Pool's schema and prose and having work item 1 add the rows with the constants in one commit.
- The design's package table lists `hive-pool add/read/update`; this definition adds eleven operations to `hive-pool` and one to `hive-imperative`, as `packages.md` allows a stage to do. The design's `hive-admit create/check/approve`, `hive-spec define/accept/close`, `hive-rule record/amend/expire/resolve` and `hive-memory render/check/propagate` are kept as named.
- The design says `hive-pool` "ports the home-scoped tasks-axi interface" and also that the legacy format is import and export only. Resolved by keeping the legacy verbs as `import-legacy` and `export-legacy` and the Pool's own operations in Hive's vocabulary; nothing of the legacy command surface is a Pool operation.
- The design asks the Pool stage to "prove that private-home selection cannot write into a project's code root". The stage 1 contract already refuses a home inside a repository for every command; the Pool adds the one operation that is given a path inside a repository, the import, and proves that path is read and never written. No contract changed.

Three things in the design that a later stage should look at, listed and not corrected because they are outside this job: the Record model row for the registry says "dispatch provisioning with durable pool references" without saying what the Pool references are; the Scheduled view reads "`word` holds carrying a revisit date" while the revisit date lives on the question and the hold stays, which pool.md and word.md now say and the UI can read either way; and the design's `test-dispatch` row names "admission" as that package's proof while the admission records and checks are the Pool's, so stage 4 proves its launch paths call `hive-admit check` and stage 2 proves the checks.
