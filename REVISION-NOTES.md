# Revision notes: the secret-handling contract

Documentation-only revision of `docs/design.md`. The new section is [Secrets: Hive keeps none](docs/design.md#secrets-hive-keeps-none), anchor `#secrets-hive-keeps-none`. Nothing under `spec/` was edited; `README.md` was checked and no sentence there became untrue, so it is unchanged.

## (a) Sentences under `spec/` that the contract makes untrue or incomplete

Each entry: file, the exact sentence, the change it needs. The repository's owner decides when these land, under the rule `spec/README.md` gives. A file not listed under "needs a change" was read and agrees with the contract.

### Needs a change

| File | Exact sentence | Change it needs |
| --- | --- | --- |
| `spec/distribution.md` | "A package that needs a platform utility as a separate process lists it in its declaration under `platform` with its reason. Every list is empty at stage 1, and the expectation is that they stay empty: what the earlier shell implementation asked of `ps`, `stat`, `shasum` and the like, the standard library does in process." | Keep the first sentence. Replace the expectation with: the lists stay empty except for a platform utility the design names, the first being `security`, the keychain's own tool, declared by each package that runs the launch primitive against the keychain (`dispatch` at stage 4, `synapse` at stage 5); a platform utility is part of the platform and is not a seventh dependency. Minor change. |
| `spec/distribution.md` | "A Hive executable starts another program only through `contract/exec`, and only one of:" (list of four kinds) | Add a fifth kind: a platform utility the package declares under `platform`. Minor change. |
| `spec/distribution.md` | "`contract/exec` takes the program as one of those named kinds and its arguments as a list. It never passes a command line through a shell, and never builds one from text." | Add: it builds the child's environment itself, minimal, and the only path by which a resolved secret value enters a child process is the launch primitive of the design's Secrets section, which sets one declared variable in that environment and nothing in the argument list; a program that needs a value is started directly, never through tmux. |
| `spec/distribution.md` | "Hive makes no network connection of its own. Every connection is made by one of the six, under that program's own configuration and credentials." | Make the second sentence complete: "...under that program's own configuration and credentials, or, where the design names a Spoor for the identity the program runs under, under a value the launch primitive resolved from the platform's store into that one process." |
| `spec/distribution.md` | "The integration tests fail when run with a search path that holds only the artefact's `bin/`, recorded fakes of `gh`, `tmux`, `claude`, `codex` and `no-mistakes`, and the real `git`: that is, with no seventh program reachable." | Add a recorded fake of `security` to the list of fakes, and say that the fake stands for the declared platform utility, not for a seventh dependency. The Mac-only qualification against the real keychain is outside the targets, as `test-targets.md` already says of route qualification. |
| `spec/schemas/package.v1.schema.json` | `"description": "Platform utilities the package runs as separate processes, each with its reason. Expected to stay empty."` | Drop "Expected to stay empty." or qualify it: empty unless the design names the utility. Minor change; no record changes shape. |
| `spec/packages.md` | "\| `synapse` \| `word` \| Assimilation refuses an unresolved Mind's word hold. \|" | Add `dispatch` to what `synapse` may call, for the lookup of the publisher's Spoor at each use (a reference, never a value). The graph stays acyclic: `dispatch` calls `pool` and `spawning-pool` only. Minor change, of the kind the file already anticipates. |
| `spec/packages.md` | "`dispatch` \| Sessions, Fleet provisioning and the identity registry, policy, metering, reservations, scheduled jobs, steering, continuity" | Add "Spoor records and their enrolment check" to what `dispatch` owns, and the record kind `dispatch.spoor` to its declaration when stage 4 adds it. Minor change. |
| `spec/records.md` | "1. Write the new content to a temporary file in the package's `tmp/` directory, named for the request that writes it." | Add a step before it: validate the record against its schema and the rule that no record holds a secret value (the value type has no encoding; a schema violation is refused) before any temporary file is opened. The design places this check before the temporary-file phase, not only before the rename. |
| `spec/test-targets.md` | "No target contacts a network, starts a model turn, reads a credential or touches an account. A test that needs `gh`, `tmux`, `claude` or `codex` runs a recorded fake that the test itself places first on the search path." | Add `security` to the programs a test runs as a recorded fake, so the hermetic targets prove the Secrets contract against the fake; say that real keychain behavior is a Mac-only qualification outside the targets. |
| `spec/test-targets.md` | "\| `test-dispatch` \| ... \| Admission, profile resolution, observation provenance, exact-target recovery and scheduled-job transitions from filing through due, run, hold and retirement. From stage 1: the identity records. \|" | At stage 4, add: Spoor enrolment, resolution and refusal against the recorded fake store, and that no launch passes a credential. The file already says each row grows with its stage. |
| `spec/commands.md` | "A command reads no environment variable as an input to the contract. It uses the environment only as any process does: to find the external executables it runs, and for those executables' own configuration." | Agrees for what a command reads. Add one clause so it is complete for what a command writes: a command sets a child's environment only through the launch primitive, and a value it resolved appears in no argument, response, receipt or log. |
| `spec/decisions.md` | "## 16. Platforms ... Not supported on Windows." | Agrees. Add to the open-points table at the end: the store choice record and the adapter for a chosen store on Linux (and Windows, should it ever be delivered), first needed by the stage that first runs a secret-needing operation on that platform; the design states the contract and the refusal and designs no adapter for a store nobody has chosen. |

### Checked and agree

- `spec/README.md`: the conventions and the change rule; agrees. The changes above are minor under its rule, except where the owner judges the new shared call or the new record kind otherwise.
- `spec/receipts.md`: "A receipt never leaves its home ... and never enters a repository, a Harvest, a comment or a log." Agrees; a receipt holds a Spoor at most.
- `spec/events.md`: rule 5, "An event's body holds references to records, never a canonical Imperative, a private report, a credential or the Hive Mind's words beyond what its kind's schema allows." Agrees.
- `spec/session-messages.md`: text parts are kept exactly; agrees, and the design says a value pasted there is a found copy, never redacted.
- `spec/review.md`: words kept exactly; agrees, same handling.
- `spec/imports.md`: "Nothing private enters an import: no credential, no private home state, no decision text and no configuration note." Agrees.
- `spec/layout.md`: rule 7 (one process-execution path) and rule 11 (nothing private committed); agree. The launch primitive sits in `contract/exec`.
- `spec/identities.md`: no credential sentence; agrees. A Spoor is a record of `dispatch` under `dispatch/records/spoor/`, not an identity record, so the three shared reads are unchanged.
- `spec/naming.md`: the names Spoor and Tendril collide with nothing there or elsewhere.
- `spec/schemas/*`: no schema has a field for a credential value; the closed-object rule is what refuses one. The Spoor schema is stage 4's to add.

## (b) Open product choices

Each key names a choice the ruled points leave open. The document is whole either way.

### `secrets-store-choice-scope`

Where the Linux or Windows store choice is recorded. Options: (1) in the home, as a record of `dispatch`, one per home; (2) once per host, in a place outside every home. Recommendation: (1). A home is explicit, already selected for every command and tested hermetically; a per-host file is the ambient input the command contract rules out. A home moved to another host re-chooses. The design is written for (1) and says so in one clause ("records that choice in the home").

### `secrets-void-references`

Whether an Imperative for a Void session names the Spoors its work needs, or the Void host's store is provisioned by the operator with no mention in the Imperative. Options: (1) the Imperative names Spoor name and purpose under allowed access, and the session resolves them in its own host; (2) the Imperative names nothing and the operator provisions the Void environment separately. Recommendation: (1), because a named gap ("this Spoor does not resolve here") is the hold the design wants, while (2) makes a missing credential an unexplained failure. The design allows both ("at most a Spoor's purpose and name").

### `secrets-detection`

What counts as a detected unauthorized copy. Options: (1) only a value-typed field reaching the record writer, which the design makes impossible by construction, plus an operator's instruction naming a record; (2) also a scan of records, journals and transcripts against the shapes of enrolled values, run as a scheduled job; (3) also an external scanner's report (the inventory lists gitleaks as a development tool) read as an instruction. Recommendation: (1) now, with (3) as the operator's ordinary instruction; (2) would require resolving every value to compare, which is itself a read the contract forbids outside a named operation. The design describes the incident and its handling without fixing the detector.

### `secrets-worker-same-user`

Workers run as the same user as Hive, so the platform grants a worker's shell the same keychain access it grants Hive. Options: (1) accept the platform boundary and state it, as the design does; (2) enrol Hive's secrets in a dedicated keychain file that is locked except during a resolve, which narrows the window but not the principal; (3) run workers as a separate user, which is a change to the Spawning Pool and tmux contracts. Recommendation: (1) for the pilot, with (2) as the first hardening and a note that (3) is a new ruling. The design claims only that nothing Hive builds widens the boundary.

### `secrets-publisher-env-variable`

How the publication and dispatch identities' tokens reach gh. gh reads a token from one environment variable; the design routes it through the launch primitive's minimal environment, which the ruled points allow for a command that takes a credential only through the environment. Options: (1) that; (2) gh's own multi-account store with a per-invocation account switch, which writes gh's global state and is a side effect the launch rules forbid. Recommendation: (1), as written.

### `secrets-order`

Not applicable. The contract was written without moving any stage or any package ownership: the Spoor record, enrolment check and status projection enter with `dispatch` at stage 4, the launch primitive and the keychain adapter enter the contract library at stage 4, the publisher's Spoor enters `synapse` at stage 5 and the status surface enters `ui` at stage 6.

## (c) Section-by-section log of `docs/design.md`

Every reference points at `#secrets-hive-keeps-none` unless another anchor is named.

| Section | What changed |
| --- | --- |
| Opening paragraphs | One sentence added at the end of the third paragraph naming the section as the one statement of the contract. |
| Vocabulary and placement | Unchanged; the two new names (Spoor, Tendril) are defined in the new section, which refers back here. |
| What Hive keeps from firstmate; Operator contract; Retained capabilities | Checked, unchanged. |
| Native workflow, configurable policy and executors | The adapter-registration sentence now says the authentication reference is a Spoor, by reference. |
| Tooling (five points) | Checked, unchanged: six dependencies stay. |
| Repository and package contracts | `dispatch` row gains the Spoor records and their enrolment check and `test-dispatch` gains Spoor proofs; `ui` row gains the enrolment status surface; one sentence added on the `platform` declaration and why a platform utility is not a seventh dependency, by reference. |
| Inventory and disposition | The macOS utilities row names `security` as the first declared platform utility, by reference. |
| Fork and adapt | Spawning Pool paragraph: a lease, a receipt and a checkout hook carry no credential, by reference. |
| Native rebuild contracts | Session launch/restart: a launch passes no credential; quota reading: a reader holds a Spoor at most; metering: observation caches expose no value, each by reference. |
| `ui`: Home and navigation | New table row, Secrets: the one enrolment status surface, reached from a credential gap, by reference. |
| `ui`: map, animation, masthead, dock, decision cards, Scheduled, record model | Checked, unchanged. |
| `ui`: Review (serving and login, capture, sharing) | Login holds or shows no value; a value typed into a note is a found copy; the export holds no Spoor or value; each by reference. Overlay, submission, wake, Done, durability and qualification checked, unchanged. |
| Kept executors and migration boundaries | New row, Platform utility (`security`), by reference; the profile sentence says a declared platform utility is part of the platform. The import sentence says an import record names no Spoor and no value, by reference. |
| Ordered implementation plan | Stage 1: what it leaves room for; stage 2: the Imperative names Spoors and has no value field; stage 3: leases and hooks carry no credential; stage 4: where the Spoor, the launch primitive and the keychain adapter enter, with the fake-store proofs; stage 5: the publisher's Spoor through `dispatch` and the launch primitive; stage 6: the status surface; stage 7: no value found after the lifecycle. Order and ownership unchanged. |
| Components and private state | Intro: no store holds a value; Observability UI, Fleet owner, Dispatcher and Synapse rows refer to the section; the execution record binds the Spoor a Hive identity used, never a value. |
| The Pool and the Mind's word; Definitions; Complete review rounds | Checked, unchanged. |
| Secrets: Hive keeps none | New section, placed after Components and private state and before Imperatives. Subsections: what Hive holds; how a value reaches exactly one command; which store on which platform; enrolment; a copy where none may be; the command contract and a value; no store of Hive's own; why a platform utility is not a seventh dependency; where the parts enter the plan. Each contract sentence has its "Prove" case. |
| Imperatives: Admission | One sentence: Imperatives name access as Spoors, have no value field, and one offered is refused before anything is written, by reference. |
| Imperatives: Render and deliver | Setup's allowed access named as Spoors; "Secrets and unrelated private history never belong in the prompt" rewritten as the Spoor rule, by reference; launchers pass no credential and the harness resolves its own login, by reference. |
| Eight dispatch classes | An unusable authentication is a Spoor that does not resolve, by reference. |
| Effective policy and resource admission | `hive-resume` resolves the same Spoor again, by reference. |
| Runtime adapters (intro) | The capability record's authentication context reference is a Spoor, by reference. |
| Local Claude and Codex | The launch passes no credential; the harnesses resolve their own login; a tmux session receives no value; by reference. |
| Fleets with local-only custody | Checked, unchanged. |
| Claude in the Void | Environment preparation verifies the Void host's own store; Hive ships no value; by reference. |
| Claude through the official GitHub integration | The credential reference is a Spoor resolved into the one `gh` trigger invocation; the workflow's credentials live in the forge's store; by reference. |
| Codex: three backends | The legacy reader's authentication-context reference is a Spoor, by reference. Enabling and retiring checked, unchanged. |
| The Shadow | An authentication refusal names the Spoor and never a value, by reference. Context and capacity checked, unchanged. |
| Restart is a non-event | Session start loads Spoors and no value; a restarted process resolves at each use; by reference. |
| Synapse (intro, obligations, local delivery, findings, custody) | Checked, unchanged. |
| Checks and review contracts | A project's credential belongs to the project and never passes through Hive; the reviewers resolve their own login; by reference. |
| Native local gate; Fix rounds and completion | Checked, unchanged. |
| Publication: GitHub as an optional publisher | The publisher's own credential reference is a Spoor looked up through `dispatch` and resolved through the launch primitive into each `gh` invocation, by reference. |
| Assimilation on the Mac | Holding a Spoor cannot widen authority over a protected base, by reference. Requirement closure checked, unchanged. |
| Explicit decisions on inherited mismatches | New row, Credentials in files and environments: inherited handling not carried over, by reference. |
| Implementation evidence still required | Package and dependency boundaries row: the platform utility faked, no seventh dependency reachable; Claude GitHub execution row: no credential beyond the dispatch identity's Spoor; new row, Secrets in the platform's store: the Mac-only qualification with the made-up marker, and the hermetic fake. |
