# Test targets

Each package proves itself through its own target, and the calls between packages are proved separately. This file names every target, says what it runs and what it must prove, and fixes the rules all of them follow. The targets are entries of the root `Makefile`; none exists until the build creates it.

Owner: each package owns its target; the contract library owns `test-contract`; the repository owns the rest.

## The targets

| Target | Runs | Must prove |
| --- | --- | --- |
| `test-contract` | The tests of `contract/` and `spec/` | Every schema against valid and invalid fixtures; that every schema stays inside the [profile](records.md#schema-notation) and every reference resolves; identifier and canonical-form vectors; the envelopes and the mapping of status to exit code; size errors; atomic replacement, roll-forward recovery and journal recovery, including a torn tail; and that the package declarations agree with this specification: each validates, every command has one owner, the declared call graph has no cycle, no package reads another's records but the three shared sets of [packages.md](packages.md#what-ownership-means) and none of those reads adds a call or takes a lock, and the [naming rules](naming.md) hold for the build files. |
| `test-pool` | The tests under `packages/pool/` | Record round trips, transitions, concurrent writes, migration and recovery, and rule expiry by the clock and lift by receipt. From stage 2, as [pool.md](pool.md#what-test-pool-proves) lists: the byte-exact legacy round trip and identity migration; every transition of the table; a hold surviving completion; admission with each requested place, Codex Void refused, a Void place refused for a local-only project and held for an unpushed branch; criteria carried from Pool to Imperative by revision; deterministic rendering; a home inside a repository refused; a persistent Fleet never an ordinary task; no worktree assumed; dependencies satisfied only by a completion kept through archival and checked before every generation opens; an imported dated word hold still blocking after its date; a temporary rule never retiring the rule it supersedes; projection freshness computed from the rules in force; specification invalidation for every bound revision; an export that never resurrects a released hold and puts each item in the section of its current state; a dependency satisfied by the completion marker through archival, including a completion of unknown time; identity reads that recover first; secrets by reference only, a value where a reference belongs `invalid`. |
| `test-spawning-pool` | The tests under `packages/spawning-pool/` | Isolation, concurrency, interrupted operations and preservation. |
| `test-dispatch` | The tests under `packages/dispatch/` | Admission, profile resolution, observation provenance, exact-target recovery and scheduled-job transitions from filing through due, run, hold and retirement. From stage 1: the [identity records](identities.md#creating-identities). |
| `test-shadow` | The tests under `packages/shadow/` | Replay, unknown and stale observations, backoff, wake ownership and one wake per record, schedule revision and due occurrence without a timer process. |
| `test-synapse` | The tests under `packages/synapse/` | Head-bound proof, finding gates, one fixer and landing authority. |
| `test-word` | The tests under `packages/word/` | Complete rounds, authorized answers, deduplication and unresolved holds. |
| `test-ui` | The tests under `packages/ui/` | Projection coverage and freshness, stale and incomplete cards, revision-bound submissions, same-session continuity, durable review rounds, receipt-driven motion, reconnect, reduced motion, context restoration, receipt display, and scheduled-job and temporary-rule lists read from records with incomplete coverage shown. |
| `test-integration` | The tests under `integration/calls/`, against the seven built executables | Every call one package declares on another, through the real command line: the request and response envelopes, each exit state the callee can return, replay, and a record of 2,097,152 bytes crossing each call whole or failing with `too_large` and no partial result. A declared call whose callee operation is not yet built is proved up to its refusal; the size proof for it arrives with the stage that builds the operation. At stage 1 the transport itself is proved against the test-only command under `integration/testcmd/`. |
| `test-distribution` | The checks under `integration/distribution/`, against the built artefact | The artefact's contents and links; that no executable needs anything beyond the platform's own libraries; that every execution of an external program goes through the one path and is declared; and a run of the integration tests with only the six core executables reachable. |
| `check-imports` | The checks under `integration/imports/` | Every import record is valid, every path it names exists, every third-party module and every file under an `upstream/` directory is covered by a record, the root licence is present if any record is, and `NOTICE` matches the records. |
| `check` | The Go formatter in check mode and the Go vet tool over the whole module | Formatting and vet cleanliness. |
| `build` | The build of all seven executables into `dist/` | That the module builds, with the settings [distribution.md](distribution.md#the-artefact) fixes. |
| `test` | `check`, `test-contract`, the seven package targets, `build`, `test-integration`, `test-distribution`, `check-imports`, in that order | Everything. |

The seven package targets and their scopes are the design's. What each must prove grows with its stage: a stage adds its proofs to its package's row here in the change that defines its contracts.

## Rules for every target

1. **Independent.** A package target builds that package and the contract library it depends on, and runs that package's tests and no others; the contract library's own tests belong to `test-contract`. It passes or fails the same whether or not any other package exists, builds or passes. Where a package calls another, its own tests stand in a fake for the other's command, built from the other's declared schemas; the real call is `test-integration`'s to prove.
2. **Named in full.** The `Makefile` lists each target and each package directory by its full name. No target is derived from a pattern, and none selects by a suffix; see [naming.md](naming.md#keeping-it-apart-from-the-pool).
3. **No network, no model, no account.** No target contacts a network, starts a model turn, reads a credential or touches an account. A test that needs `gh`, `tmux`, `claude` or `codex` runs a recorded fake that the test itself places first on the search path. A test may run the real `git`, against repositories it creates.
4. **No private home, no real clone.** Every test creates the homes, clones and worktrees it uses under a temporary directory and removes them. No test reads or writes an existing private home, an existing worktree pool, a project's clone or any state of an existing installation.
5. **Deterministic.** A test that depends on time takes the clock as an input. A test of concurrency runs its interleavings by control, not by sleeping and hoping.
6. **One command each.** Each target is one command from the repository root with no argument and no environment to set, exits 0 on success, and prints the name of each failing test otherwise.
7. **Bounded.** Each package target completes within the bound the build plan sets for one validation run. A target that outgrows it is split by the package's owner, never skipped.
8. **A target that has nothing to run fails.** A target whose package holds no test exits nonzero, so an empty package cannot pass by default.

## What the targets do not cover

A project's own test, build and lint commands belong to that project's validation configuration. Hive's targets validate Hive and are never a substitute for them.

The end-to-end proof of stage 7, the qualification of each execution route and the measurements the design asks for are separate work with their own evidence. They are named in the design's list of implementation evidence and are not targets of this file.
