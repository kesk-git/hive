# Revision notes: the secret-handling contract

Documentation-only revision of `docs/design.md`, in two rounds. The new section is [Secrets: Hive keeps none](docs/design.md#secrets-hive-keeps-none), anchor `#secrets-hive-keeps-none`. Nothing under `spec/` was edited; `README.md` was checked in both rounds and no sentence there became untrue, so it is unchanged.

## (a) Sentences under `spec/` that the contract makes untrue or incomplete

Each entry: file, the exact sentence, the change it needs. The repository's owner decides when these land, under the rule `spec/README.md` gives. A file or sentence listed under "checked and agree" was read and agrees with the contract, for the reason given.

### Needs a change

| File | Exact sentence | Change it needs |
| --- | --- | --- |
| `spec/distribution.md` | "A delivered Hive needs six other programs and nothing else: **Git, gh, tmux, Claude Code, Codex CLI and no-mistakes**." | Incomplete rather than untrue: add that a delivered Hive also runs the platform utilities its packages declare under `platform`, the first being macOS's own `security`, which the platform supplies and nobody installs, so the count of six is of programs someone must install. |
| `spec/distribution.md` | "So the delivered core has exactly six runtime prerequisites, and a seventh cannot arrive by accident: there is nothing in the repository that could need one." | Add: a declared platform utility is not a seventh prerequisite because it ships with the platform; the `platform` list is how it is named and the distribution proof is how it is seen. |
| `spec/distribution.md` | "A program that is missing or refuses to run makes the operation that needs it `unavailable`, with `dependency_missing` or `dependency_unusable`. It never makes another operation fail, and never causes a fallback to another program." | Add the platform-utility codes the design names: `platform_utility_missing` and `platform_utility_unusable`, with the same no-fallback rule, and the store conditions `secret_store_locked`, `secret_missing` and `secret_store_unsupported`. |
| `spec/distribution.md` | "A package that needs a platform utility as a separate process lists it in its declaration under `platform` with its reason. Every list is empty at stage 1, and the expectation is that they stay empty: what the earlier shell implementation asked of `ps`, `stat`, `shasum` and the like, the standard library does in process." | Keep the first sentence. Replace the expectation with: the lists stay empty except for a platform utility the design names, the first being `security`, declared by each package that runs Tendril against the keychain (`dispatch` at stage 4, `synapse` at stage 5); a platform utility is part of the platform and is not a seventh dependency. Minor change. |
| `spec/distribution.md` | "A Hive executable starts another program only through `contract/exec`, and only one of:" (list of four kinds) | Add a fifth kind: a platform utility the package declares under `platform`. Minor change. |
| `spec/distribution.md` | "`contract/exec` takes the program as one of those named kinds and its arguments as a list. It never passes a command line through a shell, and never builds one from text." | Add: it builds the child's environment itself, from nothing; the only path by which a resolved secret value enters a child process is Tendril, one file of `contract/exec`, which sets exactly one declared variable and nothing in the argument list; a program that needs a value is started directly, never through tmux. |
| `spec/distribution.md` | "Hive makes no network connection of its own. Every connection is made by one of the six, under that program's own configuration and credentials." | Complete the second sentence: "...or, where the design names a Spoor for the identity the program runs under, under a value Tendril resolved from the platform's store into that one short-lived process." |
| `spec/distribution.md` | "The integration tests fail when run with a search path that holds only the artefact's `bin/`, recorded fakes of `gh`, `tmux`, `claude`, `codex` and `no-mistakes`, and the real `git`: that is, with no seventh program reachable." | Add a recorded fake of `security` to the fakes, standing for the declared platform utility and not for a seventh dependency; and add the two checks the design's Prove sentences name: no Go package but Tendril's references the value type's resolving constructor or the child-environment setter. |
| `spec/schemas/package.v1.schema.json` | `"description": "Platform utilities the package runs as separate processes, each with its reason. Expected to stay empty."` | Drop "Expected to stay empty." or qualify it: empty unless the design names the utility. Minor change; no record changes shape. |
| `spec/packages.md` | "\| `synapse` \| `word` \| Assimilation refuses an unresolved Mind's word hold. \|" | Add `dispatch` to what `synapse` may call, for the lookup of the publisher's Spoor at each use (a reference, never a value). The graph stays acyclic: `dispatch` calls `pool` and `spawning-pool` only. Minor change of the kind the file anticipates. |
| `spec/packages.md` | "`dispatch` \| Sessions, Fleet provisioning and the identity registry, policy, metering, reservations, scheduled jobs, steering, continuity" | Add "Spoor records, their presence check and the incident receipt" to what `dispatch` owns; the record kind `dispatch.spoor` and the receipt kind `dispatch.spoor.incident` enter its declaration at stage 4. Minor change. |
| `spec/packages.md` | "6. **Declared executables only.** A package runs the external executables its declaration lists under `executes`, and no others. See [distribution.md](distribution.md)." | Add "and the platform utilities it lists under `platform`", so the rule names both lists. |
| `spec/packages.md` | The list under "The contract library": "the one process-execution path ([distribution.md](distribution.md))." | Add two items: Tendril, the one small audited launcher and the ephemeral value type, in `contract/exec`; and the no-value check as part of the schema package's validation, which every owner runs on every body before it hands a transaction to the store. |
| `spec/records.md` | "3. Write the **intent** to `tx/<request_id>.json`, a record of schema [`hive.tx-intent`](schemas/tx-intent.v1.schema.json): every record to be written, with its expected revision and the digest of its new content, and every receipt and event to be written." | Incomplete against the schema, which carries the whole body of every file and the outcome: say so, and add that the owner has validated every body, the no-value check included, before step 1 takes the lock, so that the intent, the first place a record's bytes reach disk, never holds a value. |
| `spec/records.md` | "1. Write the new content to a temporary file in the package's `tmp/` directory, named for the request that writes it." (Writing one record) | Add before it: the owner validates the record with the schema package, the no-value check included, before the store opens any temporary file. The store itself is unchanged. |
| `spec/schemas/tx-intent.v1.schema.json` | `"description": "The file tx/<request_id>.json of a store: a transaction in progress, with every file it will write and every event it will append. See spec/records.md, Writing several records."` | Agrees with the schema's own shape; the prose of `records.md` step 3 is what is incomplete. Add one clause: every body in it has passed the owner's validation. |
| `spec/identities.md` | The prefix table, "A later stage registers a new prefix by adding a row here." | Add the row `spoor_`, a Spoor, minted by `dispatch`, at stage 2: the prefix is registered with the first record that names a Spoor, stage 2's Imperative, while the Spoor record itself enters at stage 4, so stage 2's schema is not reopened. The design makes a Spoor's name its identifier. |
| `spec/packages.md` | "\| `dispatch` \| `pool`, `spawning-pool` \| The dispatcher reads accepted admission and specification revisions and acquires an isolated worktree. \|" | Add `word` to what `dispatch` may call, at stage 4, for the one Mind's word that asks which store a host uses when its home records none. The graph stays acyclic: `word` calls only `pool`, which calls nothing. The declaration it touches is `packages/dispatch/hive-package.json`, whose `calls` list gains `word`. Minor change of the kind the file anticipates. |
| `spec/commands.md` | "\| 8 \| `unavailable` \| Something the operation needs cannot be used: a core executable is missing or refuses, a source cannot be read, an authoritative record is unreadable or malformed. \| Nothing \|" | Add: a secret store is locked or has no adapter, an item is missing from it, or a declared platform utility is missing or unusable. |
| `spec/commands.md` | "\| `unavailable` \| `dependency_missing`, `dependency_unusable`, `authentication_refused`, `source_unreadable`, `record_malformed` \|" | Add the common codes the design names: `secret_store_locked`, `secret_missing`, `secret_store_unsupported`, `platform_utility_missing`, `platform_utility_unusable`. Minor change. |
| `spec/commands.md` | "A command reads no environment variable as an input to the contract. It uses the environment only as any process does: to find the external executables it runs, and for those executables' own configuration." | Agrees for what a command reads. Add one clause for what a command writes: a child's environment is set only through `contract/exec`, and a resolved value enters it only through Tendril and appears in no argument, response, receipt or log. |
| `spec/test-targets.md` | "No target contacts a network, starts a model turn, reads a credential or touches an account. A test that needs `gh`, `tmux`, `claude` or `codex` runs a recorded fake that the test itself places first on the search path." | Add `security` to the programs run as a recorded fake; say that real keychain behavior is a Mac-only qualification outside the targets. |
| `spec/test-targets.md` | The `test-contract` row | Add, at stage 4: the value type's encodings fail and its print form is the fixed redaction; Tendril's bounds; a transaction whose owner-side validation fails leaves no intent, temporary file or record; Tendril runs a fake with exactly the declared variables. |
| `spec/test-targets.md` | The `test-dispatch` row | Add, at stage 4: Spoor round trip, enrolment and presence check against the fake store, each store condition's status and code, the one hold for an unrecorded store, no credential passed at launch, and the incident hold and receipt. |
| `spec/test-targets.md` | The `test-ui` row | Add, at stage 6: the enrolment status surface renders name, purpose and resolution and takes no value; the feedback writer's no-value check on notes, card answers and submitted rounds. |
| `spec/test-targets.md` | The `test-distribution` row | Add: a recorded fake of `security` among the fakes, and the check that no Go package but Tendril's references the resolving constructor or the child-environment setter. |
| `spec/session-messages.md` | "A `text` part holds text exactly as produced or typed." | Incomplete once a writer refuses a recognized value: add that the adapter runs the owner's no-value check before the message's first byte, refuses whole what it recognizes, and keeps exactly what it cannot. |
| `spec/review.md` | "1. **Words are kept exactly.** Text is stored as typed, whitespace included. An amendment is a new event; nothing is replaced by a summary." | Same addition: the feedback writer's check runs before the first byte, refuses whole what it recognizes, keeps exactly what it cannot. |
| `spec/events.md` | "4. **Status is evidence, not input.** An event carrying what a worker or a vendor reported is recorded as that source's statement, validated for shape, size and identity. Nothing executes its content." | Add "and for the no-value check" to the validation list, with the same refuse-whole-or-keep-exactly rule. |
| `spec/decisions.md` | "## 16. Platforms ... Not supported on Windows." | Agrees. Add to the open-points table: the store choice record and the adapter for a chosen store on Linux (and Windows, should it ever be delivered), first needed by the stage that first runs a secret-needing operation there. |

### Checked and agree

- `spec/README.md`: the conventions and the change rule; agrees. The changes above are minor under its rule, except where the owner judges the new shared call, the new record kind or the new codes otherwise.
- `spec/receipts.md`: "A receipt never leaves its home ... and never enters a repository, a Harvest, a comment or a log." Agrees; a receipt holds a Spoor at most, and the incident receipt names a Spoor and a place, never a value.
- `spec/events.md` rule 5: "An event's body holds references to records, never a canonical Imperative, a private report, a credential or the Hive Mind's words beyond what its kind's schema allows." Agrees.
- `spec/imports.md`: "Nothing private enters an import: no credential, no private home state, no decision text and no configuration note." Agrees.
- `spec/layout.md` rule 7 (one process-execution path) and rule 11 (nothing private committed): agree. Tendril is one file of `contract/exec`.
- `spec/identities.md` apart from the prefix row: no credential sentence; a Spoor is a record of `dispatch` under `dispatch/records/spoor/`, not an identity record, so the three shared reads are unchanged.
- `spec/naming.md`: the names Spoor and Tendril collide with nothing there or elsewhere.
- `spec/schemas/*` other than the two named: no schema has a field for a credential value; the closed-object rule is what refuses one. The Spoor schema is stage 4's to add.

### Package declarations outside `spec/` that will name `security` (for information)

- `packages/dispatch/hive-package.json`: `platform` gains `security` with its reason at stage 4, for the presence check and for the dispatch identity's `gh` trigger; `calls` gains `word` at stage 4, for the store-choice question; `kind_prefixes` already covers `dispatch.spoor`; `receipt_kinds` gains `dispatch.spoor.incident` and the presence receipt.
- `packages/synapse/hive-package.json`: `platform` gains `security` at stage 5, for the publisher's `gh` invocations; `calls` gains `dispatch`.
- `contract/hive-package.json`: unchanged; Tendril is code of the library and runs a utility only on behalf of a package that declares it.

## (b) Open product choices

Each key names a choice the ruled points leave open. The document is whole either way.

### `secrets-store-choice-scope`

Where the Linux or Windows store choice is recorded. Options: (1) in the home, as a record of `dispatch`, one per home; (2) once per host, outside every home. Recommendation: (1). A home is explicit, already selected for every command and tested hermetically; a per-host file is the ambient input the command contract rules out. A home moved to another host re-chooses. The design is written for (1) ("recorded in the home").

### `secrets-void-references`

Whether an Imperative for a Void session names the Spoors its work needs, or the Void host's store is provisioned by the operator with no mention in the Imperative. Options: (1) the Imperative names Spoor name and purpose under allowed access and the session resolves them in its own host; (2) the Imperative names nothing and the operator provisions the Void environment separately. Recommendation: (1), because a named gap is the hold the design wants. The design allows both ("at most a Spoor's purpose and name").

### `secrets-detection`

Two separate choices, which the design keeps apart.

*The recognizer at the writer*: what the owner's no-value check recognizes, and refuses whole before the first byte, beyond a value-typed field (which it always recognizes, by construction). Options: (1) nothing more; (2) known credential shapes, as a fixed list in the contract library's schema package; (3) the shapes of the Spoors enrolled in this home, which would require knowing something of each value. Recommendation: (1) now, since (2) risks refusing ordinary text and (3) is a read the contract forbids outside a named operation; revisit (2) with evidence from the pilot.

*The detector after the fact*: what counts as a detected unauthorized copy for the incident. Options: (1) only the Hive Mind's or a supervisor's instruction naming a record, and a package's own report in a response; (2) also a scheduled scan of records, journals and transcripts for known shapes; (3) also an external scanner's report (gitleaks is in the inventory as a development tool), read as an instruction. Recommendation: (1) with (3) as an ordinary instruction; (2) only if the recognizer takes option (2) above, so both use one list. The design describes the incident and who raises it without fixing the detector.

### `secrets-worker-same-user`

Workers run as the same user as Hive, so the platform grants a worker's shell the same keychain access it grants Hive. Options: (1) accept the platform boundary and state it, as the design does; (2) enrol Hive's secrets in a dedicated keychain file that is locked except during a resolve; (3) run workers as a separate user, a change to the Spawning Pool and tmux contracts. Recommendation: (1) for the pilot, (2) as the first hardening, (3) a new ruling.

### `secrets-publisher-env-variable`

How the publication and dispatch identities' tokens reach gh. Options: (1) Tendril's minimal environment with the one variable gh reads, as the ruled points allow for a program that takes a credential only through the environment; (2) gh's own multi-account store with a per-invocation account switch, which writes gh's global state, a side effect the launch rules forbid. Recommendation: (1), as written.

### `secrets-ui-session`

Whether the UI session's authentication material, minted in memory for the server's own use and never written or shown, must outlive one run of the server. Options: (1) no: a restart of the server ends every session and the operator logs in again by the loopback or tailnet rule; (2) yes: the material is re-derived from the home-bound login at start without being stored; (3) yes, by storing it, which the contract forbids. Recommendation: (1) for the pilot, (2) if reconnect measurements of stage 6 make a restart too costly; never (3). The design is written so that (1) and (2) both hold.

### `secrets-order`

Not applicable. The contract was written without moving any stage or any package ownership: the Spoor record, presence check, status projection and incident receipt enter with `dispatch` at stage 4, Tendril, the value type, the no-value check and the keychain adapter enter the contract library at stage 4, the publisher's Spoor enters `synapse` at stage 5 and the status surface enters `ui` at stage 6. What stage 1 must leave room for is stated in the design and is already there as built: the owner validates every body with the schema package before the store writes the intent, so the no-value check is an addition to that validation and the store changes nothing. The check itself is in force from stage 2's first writer as that validation against closed schemas; the value type joins it at stage 4 with Tendril; a recognizer for credential shapes, if `secrets-detection` adds one, enters the schema package after that decision and changes no schema.

## (c) Section-by-section log of `docs/design.md`

Every reference points at `#secrets-hive-keeps-none` unless another anchor is named. "R2" marks a change of round 2.

| Section | What changed |
| --- | --- |
| Round 3, in brief | The evidence row says the presence check finds the item by its attributes and retrieves no value (the only sentence outside the section that still described it as resolving). "What Hive holds" gives the true reason a model transcript holds no value: it is the harness's own file, and no Hive command prints, returns or exports a value and no launch passes one. "Where the parts enter the plan" says once when the no-value check enters (stage 2's first writer; the value type at stage 4; a recognizer, if chosen, after that decision), when the `spoor_` prefix is registered (stage 2's Imperative) against when the Spoor record enters (stage 4), and names the two new calls, `dispatch` to `word` at stage 4 and `synapse` to `dispatch` at stage 5, with the graph acyclic; stage 2's text agrees. The findings sentence of "Validated local delivery" and the Shadow sentence are whole either way: a recognized value is refused whole (the finding set held with the precise failure; the observation left unrecorded and its source read as unknown), an unrecognized one is kept exactly and is a found copy. |
| Opening paragraphs | One sentence at the end of the third paragraph naming the section as the one statement of the contract. |
| Vocabulary and placement | R2: two rows, Spoor and Tendril, one clause each with a link; nothing restated. |
| What Hive keeps from firstmate; Operator contract; Retained capabilities | Checked, unchanged. |
| Native workflow, configurable policy and executors | The adapter-registration sentence says the authentication reference is a Spoor, by reference. |
| Tooling (five points) | Checked, unchanged: six dependencies stay. |
| Repository and package contracts | `dispatch` row gains the Spoor records and their enrolment check and `test-dispatch` gains Spoor proofs; `ui` row gains the enrolment status surface; one sentence on the `platform` declaration, by reference. |
| Inventory and disposition | The macOS utilities row names `security` as the first declared platform utility, by reference. |
| Fork and adapt | Spawning Pool: a lease, a receipt and a checkout hook carry no credential. R2, Pool: an imported record passes the owner's no-value check before its first byte. Both by reference. |
| Native rebuild contracts | Session launch/restart: a launch passes no credential; quota reading: a reader holds a Spoor at most; metering: observation caches expose no value; each by reference. |
| `ui`: Home and navigation | Table row, Secrets: the one enrolment status surface, by reference. |
| `ui`: map, animation, masthead, dock, decision cards, Scheduled, record model | Checked, unchanged. |
| `ui`: Review, Serving and login | R2: the UI session's own authentication material is minted in memory, never written or shown, and is no Spoor; whether it outlives a run is a product choice that changes nothing (`secrets-ui-session`). Login holds or shows no value, by reference. |
| `ui`: Review, Overlay and isolation | R2: "read submission credentials" rewritten as the session's authentication material, kept in memory and never handed to a page; a Spoor or a value is never among what a page can read, by reference. |
| `ui`: Review, Capture | R2: the feedback writer runs the owner's no-value check on every event, note and card answer before the first byte; refuses whole what it recognizes, keeps exactly what it cannot, a found copy from when found; by reference. |
| `ui`: Review, Submit round | R2: a committed card answer and the submitted round pass the same check; a refused answer is not committed and the card stays open; by reference. |
| `ui`: Review, Sharing | The export holds no Spoor or value, by reference. Wake, Done, durability and qualification checked, unchanged. |
| Kept executors and migration boundaries | Row, Platform utility (`security`), by reference; the profile sentence; the import sentence names no Spoor and no value. |
| Ordered implementation plan | Stage 1: what it leaves room for; stage 2: the Imperative names Spoors and has no value field, and R2, imported records pass the no-value check; stage 3: leases and hooks carry no credential; stage 4: where the Spoor, Tendril and the keychain adapter enter; stage 5: the publisher's Spoor; stage 6: the status surface; stage 7: no value found after the lifecycle. Order and ownership unchanged. |
| Components and private state | Intro: no store holds a value; Observability UI, Fleet owner, Dispatcher and Synapse rows refer to the section; the execution record binds the Spoor a Hive identity used, never a value. |
| The Pool and the Mind's word; Definitions; Complete review rounds | Checked, unchanged. |
| Secrets: Hive keeps none | New section after Components and private state. R2: Spoor holds four fields beyond what every record carries, has prefix `spoor_`, and its name is its identifier; the value type's JSON and text encodings fail and its print form is one fixed redaction; Tendril is small and audited, as contract sentences with Prove cases; the no-value check runs on every body before the intent, the first place bytes reach disk, and is run by the owner with the schema package, the store unchanged; all six dependencies named in the passes-nothing paragraph, the plugin daemon and tmux server never receive a value, Tendril hands a value only to one short-lived invocation; the self-kept Spoor defined; macOS keychain as the default with no other macOS store designed; the Windows and Linux question as one Mind's word through `word`, with a table of each condition's status and code; the presence check asks for attributes and retrieves no value; the command-contract rows for intents, exact text and unknown fields rewritten to refuse-whole-or-keep-exactly; the incident named as a `hive-word hold` raised by the owning supervisor, recorded by `dispatch` through `hive-fleet`, with who may raise one; stage 1's room stated precisely. |
| Imperatives: Admission | Imperatives name access as Spoors, have no value field; one offered is refused before anything is written; by reference. |
| Imperatives: Render and deliver | Setup's allowed access named as Spoors; the prompt sentence rewritten as the Spoor rule; launchers pass no credential; by reference. |
| Eight dispatch classes | An unusable authentication is a Spoor that does not resolve, by reference. |
| Effective policy and resource admission | `hive-resume` resolves the same Spoor again, by reference. |
| Runtime adapters (intro) | R2: the capability record's authentication context reference is a self-kept Spoor, naming the harness's own store, checked by its own probe, never resolved by Hive; by reference. |
| Local Claude and Codex | The launch passes no credential; the harnesses resolve their own login; a tmux session receives no value; by reference. |
| Fleets with local-only custody | Checked, unchanged. |
| Claude in the Void | Environment preparation verifies the Void host's own store; Hive ships no value; by reference. |
| Claude through the official GitHub integration | The credential reference is a Spoor resolved into the one `gh` trigger invocation; the workflow's credentials live in the forge's store; by reference. |
| Codex: three backends | R2: the legacy reader's authentication-context reference is a self-kept Spoor naming the CLI's own login, never resolved by Hive; by reference. Enabling and retiring checked, unchanged. |
| The Shadow | An authentication refusal names the Spoor and never a value. R2: persisted observations, wakes, whole vendor or forge responses and worker status hold no value; the Shadow's writer runs the check before the first byte; an unrecognized one is a found copy. By reference. Context and capacity checked, unchanged. |
| Restart is a non-event | Session start loads Spoors and no value; by reference. |
| Synapse (intro, obligations, local delivery, custody) | Checked, unchanged. |
| Validated local delivery, findings and custody | R2: a finding or log line quoting a value found in a change is a found copy; it stays in the private ledger, raises the incident, and the public-safe findings never carry it; by reference. |
| Checks and review contracts | A project's credential never passes through Hive; the reviewers resolve their own login; by reference. |
| Native local gate | Checked, unchanged. |
| Fix rounds and completion | R2: a fixer is launched with no credential; the sanitized output in a repair Imperative carries no value; by reference. |
| Publication: GitHub as an optional publisher | The publisher's Spoor looked up through `dispatch` and resolved through the launch primitive into each `gh` invocation, by reference. |
| Assimilation on the Mac | Holding a Spoor cannot widen authority over a protected base, by reference. Requirement closure checked, unchanged. |
| Explicit decisions on inherited mismatches | Row, Credentials in files and environments, by reference. |
| Implementation evidence still required | Package and dependency boundaries row: the platform utility faked; Claude GitHub execution row: no credential beyond the dispatch identity's Spoor; row, Secrets in the platform's store: the Mac-only qualification with the made-up marker and the hermetic fake. |
