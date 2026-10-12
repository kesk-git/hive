# Hive specification

This directory holds the contracts of the first two stages of the [ordered implementation plan](../docs/design.md#ordered-implementation-plan). The first stage's are a definition, accepted on 2026-10-11 with all twenty of its decisions, of which version 1.9 amends one, decision 16 on platforms, and version 1.10 amends four, decisions 1, 4, 15 and 16, for the operator's decisions on the platforms; the second stage's, the Pool, are defined on top of them and extend them without reopening them. The build of each follows as separate work. Nothing of stage 2 is implemented: no executable, no test and no imported source of the Pool exists yet.

[The design](../docs/design.md) says what Hive is and why. This specification says where each thing lives, who owns it and what proves it. Where the two differ, the design governs and the difference is a defect in this specification.

Contract version: **1.11**. What each version changed is recorded under [versions/](versions/), one file per version.

## What is defined

| File | Contract |
| --- | --- |
| [decisions.md](decisions.md) | Every decision taken where the design was silent, with its reason and the alternatives rejected. |
| [layout.md](layout.md) | The repository's directory layout and what may live where. |
| [naming.md](naming.md) | The Spawning Pool's names in every form, and the rules that keep it apart from the Pool. |
| [packages.md](packages.md) | The seven package owners, the command each owns, the calls each may make and the shared contract library. |
| [identities.md](identities.md) | Home, workspace, project and clone identities, the identifier form, scope and how identities are created. |
| [records.md](records.md) | The on-disk record format: encoding, versioning, atomic replacement, the home layout, the event journal and locks. |
| [commands.md](commands.md) | The command contract: invocation, the request and response envelopes, exit and error states, size errors, replay and capability declarations. |
| [receipts.md](receipts.md) | The receipt envelope, kind names and the rule that a receipt states one fact. |
| [events.md](events.md) | The event envelope, the three times, ordering, deduplication and the journal. |
| [session-messages.md](session-messages.md) | The session message and the input receipt of the one supervisor conversation. |
| [review.md](review.md) | The four files of a review directory. |
| [test-targets.md](test-targets.md) | The test targets, what each runs and what each must prove. |
| [distribution.md](distribution.md) | The five-dependency distribution contract: the three platforms, macOS, Windows and Linux, the artefact of each, the standard library and its two named exceptions, and the one process-execution path. |
| [imports.md](imports.md) | The import record that precedes any upstream source. |
| [pool.md](pool.md) | Stage 2: the Pool's store, records, operations, the execution place, admission, the Imperative, specifications, rules, generated knowledge, legacy import and export, receipts, events and error codes. |
| [import-plan-tasks-axi.md](import-plan-tasks-axi.md) | Stage 2: the plan for the translation of tasks-axi into the Pool, field by field of the import record the build writes. |
| [schemas/](schemas/) | The schemas themselves, one file per record. They are normative. |

The package declarations sit with their packages: [`packages/<name>/hive-package.json`](../packages) for the seven owners and [`contract/hive-package.json`](../contract/hive-package.json) for the shared library. The import records will sit in [`imports/`](../imports/README.md).

## Conventions

- **Must** and **never** state requirements. A sentence that describes behavior without either word is still a requirement unless it says it is an example.
- A schema file and the prose that describes it say the same thing. Where they differ, the schema governs for the shape of a record and the prose governs for its meaning, and the difference is a defect to correct in the same change.
- Field names are written `like_this`; package, command, kind and target names `like-this`.
- A name in the vocabulary of the design keeps its capital: the Pool, the Spawning Pool, the Shadow, a Harvest.
- "The design" means `docs/design.md` at the same revision as this directory.

## Changing a contract

A contract changes in one commit that edits the prose, the schema and every package declaration the change touches, and raises the version it affects.

- A **minor** change adds something an existing reader can ignore: an optional field, a new kind, a new operation, a new error code. Every record valid before the change is valid after it. The schema's list of accepted versions grows by one.
- A **major** change is anything else. It adds a new schema file beside the old one, with the next major number in its name and identifier, and names the migration that carries old records forward. The old file stays for as long as records of that version can exist.
- A change that moves ownership of a command, a record or a path of the home from one package to another is a major change of this specification, whatever it does to a schema.

The contract version in this file moves with the largest change made to any contract in the directory. A change raises the version stated in this file and adds its own file under [versions/](versions/), named for the version it introduces; a change rebased behind another that took its number renames only its own file and that one line.
