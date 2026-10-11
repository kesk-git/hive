# Hive specification

This directory holds the contracts of the first two stages of the [ordered implementation plan](../docs/design.md#ordered-implementation-plan). The first stage's are a definition, accepted on 2026-10-11 with all twenty of its decisions; the second stage's, the Pool and the Mind's word, are defined on top of them and extend them without reopening them. The build of each follows as separate work. Nothing of stage 2 is implemented: no executable, no test and no imported source of the Pool exists yet.

[The design](../docs/design.md) says what Hive is and why. This specification says where each thing lives, who owns it and what proves it. Where the two differ, the design governs and the difference is a defect in this specification.

Contract version: **1.4**. Version 1.0 was accepted on 2026-10-11; 1.1, later the same day, corrects the distribution contract to six dependencies, with no-mistakes as the long-lived plugin point behind Synapse, by the Hive Mind's ruling on the shape of the gate's stage. It is a minor change: no schema, record or package declaration changed. Version 1.2, later the same day, commits the schema of the transaction intent, `tx-intent.v1`, which [records.md](records.md#writing-several-records) requires of every record. It also names `tmp/`, the one directory of each package's own directory that holds the temporary files of writes in progress. It is a minor change: it adds a schema and a directory and changes no record that existed. Version 1.3, later the same day, commits the schema of the recorded outcome of a mutating request, `request-outcome.v1`, which [records.md](records.md#schema-notation) requires of every record, and states which responses a caller accepts with a null `request_id`. It is a minor change: it adds a schema and makes explicit a rule the response already stated, and changes no record that existed. Version 1.4, later the same day, adds the stage 2 contracts: the Pool's ten records, thirty-four operations, thirty-one receipt kinds, twenty-one event kinds, six identifier prefixes and the Mind's word's intake, in [pool.md](pool.md), [word.md](word.md) and the schemas they name, with the `pool` and `word` declarations amended. It is a minor change: every record, request and declaration valid under 1.3 is valid under 1.4, and no stage 1 schema changed.

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
| [distribution.md](distribution.md) | The six-dependency distribution contract. |
| [imports.md](imports.md) | The import record that precedes any upstream source. |
| [pool.md](pool.md) | Stage 2: the Pool's store, records, operations, the execution place, admission, the Imperative, specifications, rules, generated knowledge, legacy import and export, receipts, events and error codes. |
| [word.md](word.md) | Stage 2: the Mind's word, the single decision intake, and how it persists through the Pool. |
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

The contract version in this file moves with the largest change made to any contract in the directory.
