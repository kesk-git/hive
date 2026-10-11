# Package owners

Hive has seven packages. Each is the single owner of its commands, its records, its receipts and its part of a private home, and each has its own test target. They are ownership and testing boundaries inside one repository, not services.

Each package states what it owns in its declaration, `packages/<name>/hive-package.json`, which follows [`package.v1.schema.json`](schemas/package.v1.schema.json). The declarations are the registry: this file explains them and does not repeat their lists. Where this file and a declaration differ, the declaration governs.

## The seven

| Package | Owns | Executable | Home paths | Test target | Built in plan stage |
| --- | --- | --- | --- | --- | --- |
| `pool` | The Pool: work items, admission, Imperatives, specifications, rules and generated knowledge | `hive-pool` | `pool/` | `test-pool` | 2 |
| `spawning-pool` | The Spawning Pool: Hive-owned worktrees, leases and their receipts | `hive-spawning-pool` | `spawning-pool/` | `test-spawning-pool` | 3 |
| `dispatch` | Sessions, Fleet provisioning and the identity registry, policy, metering, reservations, scheduled jobs, steering, continuity, and the Spoor records of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section with their enrolment check and their status projection | `hive-dispatch` | `home.json`, `dispatch/` | `test-dispatch` | 4 |
| `shadow` | Observations, due polls, wakes and acknowledgments | `hive-shadow` | `shadow/` | `test-shadow` | 4 |
| `synapse` | Validation obligations, review and fix custody, evidence, readiness, Assimilation | `hive-synapse` | `synapse/` | `test-synapse` | 5 |
| `word` | The single decision intake | `hive-word` | none | `test-word` | 2 |
| `ui` | The Hive UI and every file of a review directory | `hive-ui` | `ui/`, `reviews/` | `test-ui` | 6 |

`word` keeps no store. It persists through the commands of `pool`, as the design requires.

## Commands by owner

The design names twenty-eight `hive-*` commands. Each belongs to exactly one package.

| Package | Commands |
| --- | --- |
| `pool` | `hive-pool`, `hive-admit`, `hive-imperative`, `hive-spec`, `hive-rule`, `hive-memory` |
| `spawning-pool` | `hive-spawning-pool` |
| `dispatch` | `hive-dispatch`, `hive-fleet`, `hive-schedule`, `hive-session-start`, `hive-policy`, `hive-context`, `hive-park`, `hive-resume`, `hive-send`, `hive-control`, `hive-stow`, `hive-recover`, `hive-sync`, `hive-retire` |
| `shadow` | `hive-shadow`, `hive-state` |
| `synapse` | `hive-synapse`, `hive-assimilate`, `hive-assimilate-local` |
| `word` | `hive-word` |
| `ui` | `hive-ui` |

The design's package table names some of these outright and covers the rest with a phrase. The reading taken here is: "session/control/policy/context helpers" and "continuity" place `hive-session-start`, `hive-policy`, `hive-context`, `hive-park`, `hive-resume`, `hive-send`, `hive-control`, `hive-stow`, `hive-recover`, `hive-sync` and `hive-retire` in `dispatch`; "guarded Assimilation interfaces" places `hive-assimilate` and `hive-assimilate-local` in `synapse`. See [decisions.md](decisions.md#11-every-command-has-one-owner).

A declaration lists, for each command, the operations the design names. Every command also has the `capabilities` operation of the [command contract](commands.md#capabilities). A stage that builds a command adds its further operations to the declaration in the same change that adds their schemas.

## What ownership means

1. **One writer.** Only the owning package writes its home paths, its records and its receipts. Another package that needs a change asks for it through the owner's command.
2. **One reader of private state.** Only the owning package reads its home paths. Another package that needs a value asks for it through the owner's command and receives a complete response. Three sets of records are the exception, and are read by any package of the same home through the contract library's validated reader, never by opening the files directly: (a) the `journal/` and `locks/` paths, which are also written only through the contract library; (b) the identity records, meaning the home record and the workspace, project and clone records, so a callee can check that the scope it was given exists, is active and belongs together without calling `dispatch`; (c) the files of a review directory, so the Shadow can read registered review files without calling `ui`. The identity records and the review files are still written only by their owner, or by the writers [review.md](review.md#who-writes-what) names. These reads take no lock: each record is replaced atomically, and the parent links the check relies on are fixed attributes. They add no call and no lock, so the graph below and the lock order are unchanged. See [records.md](records.md#the-home).
3. **Names carry the owner.** Every record kind, receipt kind, event kind and package error code begins with the owning package's name. A package never writes a name that begins with another package's.
4. **One test target.** A package's target runs that package's tests and nothing else's, and passes or fails without any other package being built. See [test-targets.md](test-targets.md).
5. **Declared calls only.** A package calls the commands of the packages its declaration lists under `calls`, and no others.
6. **Declared executables only.** A package runs the external executables its declaration lists under `executes`, or the platform utilities it lists under `platform`, each with its reason, and no others. See [distribution.md](distribution.md).
7. **Declared libraries only.** A package links the Go standard library, `contract/` and the third-party modules its declaration lists under `libraries`, each with an [import record](imports.md). At stage 1 every list is empty.

## Calls between packages

| Package | May call | From the design |
| --- | --- | --- |
| `pool` | none | It is the base store. |
| `spawning-pool` | none | It receives the identities it binds in each request. |
| `word` | `pool` | `word` uses `pool` for persistence. |
| `dispatch` | `pool`, `spawning-pool`, `word` | The dispatcher reads accepted admission and specification revisions and acquires an isolated worktree. It calls `word`, from stage 4, for the one Mind's word that asks which secret store a host uses. The graph stays acyclic: `word` calls only `pool`. |
| `synapse` | `word`, `dispatch` | Assimilation refuses an unresolved Mind's word hold. Synapse calls `dispatch`, from stage 5, for the lookup of its publisher's Spoor at each use, a reference and never a value. |
| `shadow` | `pool`, `dispatch`, `synapse`, `word` | The Shadow reads scheduled-job records, dated holds, rule expiries and the attributed Synapse run. |
| `ui` | `pool`, `dispatch`, `shadow`, `synapse`, `word` | The UI reads scoped projections, sends answers to `word` and submits session input and policy requests to `dispatch`. |

These lists are the flows the design states and no more. They are a starting declaration: the stage that builds a package amends that package's own list when it finds a call it needs, as a minor change of this specification.

Two rules hold for every amendment.

- **The graph has no cycle.** A command may hold its own store's lock while it calls a package it declares, so a cycle of calls would be a cycle of locks. `test-contract` proves the declared graph is acyclic.
- **`ui` writes through two packages only.** It may read from any package it declares, and it mutates only through `hive-word` for a decision answer and through the `dispatch` commands for session input and a policy request. Its own home paths are its own to write.

## The contract library

`contract/` is not a package in the sense above. It owns no command, no home path and no record, and it decides nothing that belongs to one package. It is the single implementation of the rules this specification states once for everyone, so that no package carries its own copy:

- typed identifiers, timestamps and digests ([identities.md](identities.md));
- canonical JSON and the schema validator, with the owner's no-value check and its fixed list of credential shapes, which the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section defines ([records.md](records.md));
- atomic record files, the home event journal and transaction locks ([records.md](records.md));
- the validated reader through which any package reads the journal, the identity records and the files of a review directory ([what ownership means](#what-ownership-means));
- the request and response envelopes, the command-line harness and the exit states ([commands.md](commands.md));
- the one process-execution path, with Tendril, the launch primitive of the same section, and the value type it defines ([distribution.md](distribution.md)).

Its test target is `test-contract`. A change to it is a change to every package and is reviewed as a contract change.

## Extraction

A package may later be moved out of the repository behind the same commands. The layout keeps that possible: a package's code links nothing of another package, its executable is its own, and its declaration names everything it needs from outside. An extracted package is not merged back.
