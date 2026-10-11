# Decisions taken in this definition

The design leaves a number of choices to contract work. Each one made here is listed with its reason, what it costs, and what was rejected. They are proposed together and are accepted or sent back as a whole. A decision that a later stage could change cheaply says so; the others are the ones to read closely.

Nothing here widens the design. Where a decision reads the design one way of two, it says which.

## 1. Go for everything delivered

**Decision.** Hive is written in Go, pinned to release line 1.27. The standard library is the only library at stage 1.

**Reason.** The design forbids a sixth runtime prerequisite. Go builds each executable as one self-contained file that needs nothing installed, on the Mac and on Linux alike, and its standard library covers what stage 1 needs: JSON, files and locks, processes, an HTTP server and embedded files. Treehouse, the larger of the two adaptations, is written in Go and can be adapted directly.

**Cost.** tasks-axi is written in TypeScript. The Pool's adaptation therefore becomes a translation, and its tests are carried over by re-expression rather than kept as files. The import record says so file by file; see [imports.md](imports.md#the-two-planned-imports).

**Rejected.** *TypeScript*, which would keep tasks-axi's source and tests as they are, needs Node at run time or a bundled runtime of tens of megabytes inside each executable, and would turn the Treehouse adaptation into the translation instead. *Shell*, as the inherited implementation uses, has no structured data or schema validation without tools outside the five, and its concurrency and atomicity are the source of defects this design exists to remove. *Rust* meets the delivery rule and makes both adaptations translations. *Swift* does not run the package tests in a Linux session.

**To undo.** Expensive after stage 2. This is the first decision to settle.

## 2. One module, each package under its own `internal/`

**Decision.** One Go module at the root. Each of the seven packages keeps all its code under `packages/<name>/internal/`, with one main package under `packages/<name>/cmd/`.

**Reason.** Go refuses an import of another directory's `internal/`. The design's rule that no package opens another's store then holds by construction: a package cannot link another's code, so its command is the only way in. It also keeps each package extractable.

**Rejected.** *A module per package* gives the same boundary at the cost of seven version sets to keep in step inside one repository. *Convention alone* is what the design calls bypassing a writer waiting to happen.

**To undo.** Moderate: a mechanical move, but every import path changes.

## 3. One executable per package, the other command names as links

**Decision.** Seven executables. Each package's other command names are links to its executable, which acts as the name it was started under.

**Reason.** The design names twenty-eight commands and says not to create a package for every command. A separate executable for each would be twenty-eight files of several megabytes each that must always be built and delivered in step. One per package keeps the ownership boundary visible on disk and the artefact small.

**Rejected.** *One executable for all of Hive* would put seven packages in one process image and end extraction. *Twenty-eight executables*, as above.

**To undo.** Cheap. The names and the contract do not change.

## 4. JSON Schema, in a restricted profile, with Hive's own validator

**Decision.** Schemas are JSON Schema draft 2020-12 files under `spec/schemas/`, limited to the keywords [records.md](records.md#schema-notation) lists. The contract library validates them with its own implementation of that profile. The schema files are embedded into every executable.

**Reason.** The schemas are the contract between packages that may later be extracted, and between Go and the browser, so they must be in a notation that is not Go. JSON Schema is the common one. The full draft is large and its hardest parts are unneeded here; the profile is small enough to implement and test completely with the standard library, which keeps the library list empty.

**Rejected.** *Go types as the source of truth* would make the contract unreadable outside Go. *A third-party validator* is a dependency, an import record and a transitive audit for something a few hundred lines cover. *A typed interface language with code generation* adds a toolchain and committed generated files.

**To undo.** Moderate. The schema files survive a change of validator; widening the profile is a minor change.

## 5. JSON files on disk, no database

**Decision.** A record is one JSON file, replaced atomically. A journal is a file of JSON lines. Several records change together through a recorded intent that recovery rolls forward. There is no database.

**Reason.** The design asks for atomic record replacement, append-only journals and locks, and for records an operator can read. Files give all of that with the standard library, are inspected with any tool, and need no engine to be installed or linked.

**Rejected.** *SQLite* would give transactions for free, and needs either the C toolchain in the build, which ends the self-contained executable, or a large third-party pure-Go engine. *Markdown with front matter*, as the Pool's upstream uses, is kept as an import and export format only; as the store it would make every record's shape a matter of parsing prose.

**Cost.** Hive implements its own multi-record transaction. It is small, it is in one place, and `test-contract` proves its recovery.

**To undo.** Expensive once homes hold records.

## 6. Every object closed, newer versions refused, unknown fields kept apart

**Decision.** Every object schema rejects a field it does not list. A reader refuses a record of a newer version than it knows. A migration carries what it does not recognize into the record's `unknown` member, which every later writer preserves.

**Reason.** The design says malformed input fails closed and that unknown fields survive migrations unchanged. Closed schemas give the first. A designated `unknown` member gives the second without leaving every record open to a mistyped field passing as valid.

**Rejected.** *Open records that ignore what they do not know* cannot tell a newer field from a mistake, and a writer that does not know a field cannot be trusted to keep its meaning.

**To undo.** Cheap in one direction: a schema can be opened later; it cannot easily be closed.

## 7. Typed, lowercase identifiers; whole-number generations

**Decision.** An identifier is a registered prefix, an underscore and a lowercase ULID. A generation, a round and a review revision are whole numbers from 1.

**Reason.** A prefix says what an identifier names wherever it appears. A ULID needs no coordination between homes and is unique across them. Lowercase only, because identifiers become file names on a file system that ignores case. Generations are ordered and the design numbers them in paths, so they are numbers.

**Rejected.** *Names or slugs as identity*: the design says a label re-homes nothing. *Short identifiers*: the design says the same short identifier in two homes must never select an owner. *UUIDs* work equally and are longer in every path.

**To undo.** Expensive once records exist.

## 8. Requests never travel as arguments, and the home is always named

**Decision.** A request is read from a file or standard input, never from an argument. `--home` is required; there is no default, no environment variable and no search.

**Reason.** The design requires that a large request never be one argument and that private records never be ambient inputs. Making the file the only route removes the size boundary instead of guarding it, and removes the question of which requests are small enough. Naming the home every time is the plain form of "never ambient".

**Rejected.** *Arguments for small requests and a file for large ones* is two paths to test and one threshold to get wrong. *A default home from the environment* is the ambient input the design rules out.

**Cost.** A person at a terminal types more. A command may offer a short form that builds the same request.

**To undo.** Cheap to add a convenience; the canonical form would stay.

## 9. Eleven exit states

**Decision.** The exit codes 0 to 10 of [commands.md](commands.md#exit-and-error-states), each with one meaning, and a finer `code` inside the response.

**Reason.** A caller decides what to do next from the exit state alone: proceed, fix the request, wait, reconcile or stop. `unknown` and `held` are separate from failure because the design treats them separately: an unknown outcome is reconciled and never retried, and a held one waits on a named decision.

**Rejected.** *Zero and one with everything else in the response body* loses the distinction exactly when the response could not be written. *A code per error* turns the exit code into a second error registry.

**To undo.** Moderate. Adding a state in the reserved range is a minor change; changing a meaning is not.

## 10. One event journal per home, shared through the contract library

**Decision.** A home has one journal. Every package appends to it through the contract library, under one lock, and reads it the same way.

**Reason.** The design asks for a home sequence. One journal makes that the line order: consecutive, with no gap, and safe across a crash. It also lets a package built at stage 2 record events before the Shadow exists at stage 4.

**Rejected.** *A journal per package with a shared counter* leaves a gap or a duplicate when a process dies between the counter and the append, unless each writer reads the others' journals to recover. *A journal owned by the Shadow* would make every package call a later stage's command to record anything.

**Reading the design.** The design says no package opens another's private store. The journal is the home's, not a package's, and holds only notices of what owners have already recorded. This is the one place two packages touch the same files, and it is always through one implementation.

**To undo.** Moderate before stage 4, expensive after.

## 11. Every command has one owner

**Decision.** The design's package table names some commands and covers others with a phrase. All twenty-eight are assigned in [packages.md](packages.md#commands-by-owner): the session, policy, context, park, resume, steering, control and continuity commands to `dispatch`, and the two Assimilation commands to `synapse`.

**Reason.** The phrases in the table point there. Two assignments are less certain than the rest and are named so they can be moved: `hive-retire`, whose release of a worktree lease touches the Spawning Pool, and `hive-sync`, which refreshes a project copy after a landing that `synapse` verified. Both stay in `dispatch` as continuity, calling the owner of what they touch.

**To undo.** Cheap before the command is built.

## 12. A shared contract library that is not an eighth package

**Decision.** `contract/` implements the rules common to all seven packages, with its own target, `test-contract`. It owns no command, no path of a home and no record.

**Reason.** The design says a responsibility has one owner and is not duplicated. Envelope handling, validation, atomic files and the execution path are needed by all seven; seven copies would be seven chances to differ.

**Rejected.** *Placing it in `pool`* would make every package depend on the Pool's code. *Copies per package*, as above.

**Reading the design.** The design counts seven packages, each with a store or a command. The library has neither and is not counted among them.

**To undo.** Cheap.

## 13. The identity part of provisioning is built at stage 1

**Decision.** Stage 1's build delivers `hive-fleet provision` for identity records only: home, workspace, project and clone, in one home. Everything else in `dispatch` waits for stage 4.

**Reason.** The plan builds the Pool at stage 2 and the Spawning Pool at stage 3, each in a real home and the second against real clones, while the package that owns the registry is built at stage 4. Something must create a home's identity before stage 2. Building the owner's own operation early keeps one writer; the alternative is a second writer that stage 4 would have to replace.

**Rejected.** *Fixture identities until stage 4* would let stages 2 and 3 pass their tests and leave no way to use them. *Home creation inside `pool`* would give the home's identity two owners.

**To undo.** Cheap: the operation's later versions extend it.

## 14. Two narrow readings of the review files

**Decision.** A free-standing round instruction is recorded as a `note` event whose `note_kind` is `instruction`. `round_id` is the round's number.

**Reason.** The design lists five event kinds and also says an instruction is its own entry; it names a `round_id` and also numbers rounds in file names. Each reading keeps both statements true without adding to the design's lists.

**Rejected.** *A sixth event kind* and *a separate round identifier beside the number*: each adds something the design does not name.

**To undo.** Cheap before stage 6.

## 15. Hand-written browser files and no second toolchain

**Decision.** The UI's HTML, CSS and JavaScript are standard files a browser loads as they are, embedded into `hive-ui`. The repository has no bundler, transpiler or package manifest of another ecosystem.

**Reason.** A second toolchain is a second set of dependencies to audit and a set of generated files whose provenance must be tracked. The UI the design describes is a map, lists and cards over live records, which current browsers support directly.

**Cost.** No typed browser code and no component library. Stage 6 measures the UI against budgets chosen from evidence, and may bring this decision back if they cannot be met.

**To undo.** Cheap until stage 6, and then a question for that stage.

## 16. Platforms

**Decision.** Delivered for macOS on Apple silicon and on Intel. Built and tested on Linux. Not supported on Windows.

**Reason.** The design places every supervisor on the Mac. Linux is where a Void session and a hosted check run Hive's own tests. tmux, one of the five, does not exist on Windows.

**To undo.** Cheap to add a platform.

## 17. Target names beyond the seven

**Decision.** `test-contract`, `test-integration`, `test-distribution`, `check-imports`, `check`, `build` and `test`, driven by `make`.

**Reason.** The design names the seven package targets and says integration targets exist separately. The rest follow from the library, the distribution contract and the import record each needing a proof.

**To undo.** Cheap.

## 18. Flat scope fields with a declared level

**Decision.** Every request, receipt and event carries `scope_level` and the flat fields `home_id`, `workspace_id`, `project_id`, `clone_id`, `task_id` and `generation`. The level is `hive`, `workspace` or `project`.

**Reason.** The design names these fields flat wherever it lists a record's contents, and requires the projectless scope to be declared rather than implied by absence. A workspace-wide operation, such as a Fleet's stow, is neither projectless nor a project's, so there are three levels.

**To undo.** Expensive once records exist.

## 19. Calls between packages start from what the design states

**Decision.** Each package's declared list of the packages it may call holds only the flows the design states. A stage extends its own package's list when it needs to, and the graph stays free of cycles.

**Reason.** Guessing the full graph now would be design by anticipation. Starting small makes every added call a visible, reviewed change.

**To undo.** Cheap; it is a list in a declaration.

## 20. Translating from source counts as an import

**Decision.** Behavior re-expressed in another language by working from an upstream's source is recorded as an import of kind `translation`, with the upstream's notices kept.

**Reason.** The result is derived from the upstream. Recording it is what the design's attribution rule asks for and costs one file.

**To undo.** Not applicable; removing a record of a real derivation would be the error.

## Left to the stage that needs it

These are not decided here, deliberately. Each is cheap to add later and would be a guess now.

| Open point | First needed |
| --- | --- |
| The Pool's own records and the layout inside `pool/` | Stage 2 |
| Where the Spawning Pool keeps its worktrees on disk, and its lease record | Stage 3 |
| A streamed form of response | Stage 4 |
| Pending approvals in the session, and how a reader is told of a new message | Stage 4 |
| The minimum version of each of the five, by probe | The stage that first runs each |
| Where instructions and skills sit in the artefact | The stage that first delivers one |
| Retention periods and segment sizes | The stage that owns each store, as configuration |
| Which package declares the optional browser stack | The stage that first selects a browser task |
