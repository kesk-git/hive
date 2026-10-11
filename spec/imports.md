# Import record

Hive takes source from other projects in two places, the Pool from tasks-axi and the Spawning Pool from Treehouse, and may later take a library. Each time, a record is written first. This file fixes when a record is required, what it holds and what checks it.

Owner: the repository; each record names the package that maintains what it covers. Schema: [`import-record.v1`](schemas/import-record.v1.schema.json). Proved by `check-imports`.

## When a record is required

A record is required before any of the following enters the repository:

| Kind | What it covers |
| --- | --- |
| `adaptation` | Upstream source text copied and then changed. |
| `translation` | Upstream behavior re-expressed in another language by working from its source. The result is derived from the upstream and is recorded as such. |
| `dependency` | A third-party module compiled into a delivered executable. |
| `asset` | A non-code file taken as it is: a fixture, a font, an image. |

No record is required, and none is written, for a **native rebuild**: a responsibility implemented from its behavioral contract without the upstream's source, tests or data. A rebuild that takes a fixture, a test case or a table from an upstream's files is no longer only a rebuild; what it took is an import.

## Preconditions

1. **The licence first.** The root `LICENSE` is in the repository before the first record is. It is added by its own change.
2. **The record with or before the source.** A record is committed in the same change as the material it covers, or an earlier one. Material is never committed ahead of its record.
3. **Audited before imported.** The selected source, its tests, its generated assets and its transitive libraries are examined first. A root licence file upstream does not settle the terms of every file under it; the record states the terms established for the portions actually taken, and lists what was looked at and left out.
4. **Rights established.** Material whose licence or provenance is not established is not imported. It is not relabelled as MIT, and a record is not a way to defer the question.

## The record

One file, `imports/<import_id>.json`. The `import_id` is a short name fixed at first import: `tasks-axi`, `treehouse`.

| Field | Holds |
| --- | --- |
| `import_id`, `kind`, `owner` | The name, the kind above, and the package that maintains the material. |
| `recorded_on` | The date the record was first committed. |
| `upstream` | The upstream's name, its public repository, the **exact commit** taken from, the tag if it has one, and for a dependency the module path and version the build pins. |
| `licence` | The SPDX expression established for the portions taken; every copyright notice that applies, verbatim; and the upstream licence and notice texts kept in this repository, each by path and digest. |
| `portions` | Every file here that holds imported material, each with the upstream files it came from and whether it was `copied`, `adapted` or `translated`. |
| `retained_tests` | The upstream tests kept, each with where it lives here and how it was carried over. A translated test names the upstream test it re-expresses. |
| `transitive` | Every third-party library the taken portions bring, each with its version and licence and whether it has its own record, was replaced natively or was dropped. |
| `excluded` | What the audit examined and left out, and why. |
| `local_changes` | What Hive changed, in plain words, kept current as the import is maintained. |
| `audit` | When the audit was made and what it found. |

The upstream's licence and notice texts are kept unchanged under `packages/<owner>/upstream/<import_id>/`. A file that holds imported material keeps the upstream's copyright header where it had one.

The root `NOTICE` is the notices of all records together. It is committed, and `check-imports` fails if it differs from what the records produce.

## Maintaining an import

- The record is the import's history. A later sync with the upstream updates `upstream.revision`, the portions and `local_changes` in the same change that brings the source.
- A file that is rewritten until nothing of the upstream remains is removed from `portions` by a change that says so. The record itself stays for as long as any portion or retained test does.
- An import is never moved between packages without a change to its `owner`.
- Nothing private enters an import: no credential, no private home state, no decision text and no configuration note.

## What `check-imports` proves

1. Every file under `imports/` other than `README.md` is a valid record whose name matches its `import_id`.
2. Every path a record names exists, and every digest it gives matches.
3. Every file under any `upstream/` directory is named by a record.
4. Every module in `go.mod` beyond the standard library has a `dependency` record, and every package declaration's `libraries` and `imports` lists name existing records.
5. If any record exists, the root `LICENSE` exists.
6. `NOTICE` equals the notices the records produce.

It cannot prove that an unrecorded copy was never made. That is a matter for review, and the rule for a reviewer is simple: source that resembles an upstream and has no record is a finding.

## The two planned imports

Neither is made by this definition. Each is made by its stage, record first.

| Import | Owner | Kind | Upstream baseline named by the design |
| --- | --- | --- | --- |
| `tasks-axi` | `pool` | `translation` | Release 0.2.6 |
| `treehouse` | `spawning-pool` | `adaptation` | Release v3.1.2 |

tasks-axi is written in TypeScript and Hive is written in Go, so the Pool's adaptation is a translation: its parser, transitions, archives and moves are re-expressed, and its tests are carried over as translated tests, each naming the upstream test it re-expresses. Treehouse is written in Go and is adapted directly. See [decisions.md](decisions.md#1-go-for-everything-delivered).
