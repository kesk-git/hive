# Import plan: tasks-axi

This is the plan for the one import stage 2 makes: the translation of tasks-axi's backlog grammar, store rules, transitions and archives into the `pool` package. The upstream's multi-item move is not translated at this stage; see the exclusions below. It follows [imports.md](imports.md) and the [`import-record.v1`](schemas/import-record.v1.schema.json) schema field by field, so that the build can write the record from it. Nothing is imported by this plan: no source, test, fixture or documentation of the upstream is in this repository, and none enters it before the record does.

**Who writes the record.** Work item 2, lane a, of the stage's build writes `imports/tasks-axi.json`, the kept licence text under `packages/pool/upstream/tasks-axi/LICENSE` and the root `NOTICE`, in the same commit as the first translated source file, or an earlier one, and adds `tasks-axi` to the `imports` list of `packages/pool/hive-package.json` in that commit. `check-imports` holds the record to paths that exist, which is why this definition does not write it: no translated source exists yet.

## The record, field by field

| Field | Value the build writes |
| --- | --- |
| `import_id` | `tasks-axi` |
| `kind` | `translation`: upstream behavior re-expressed in Go by working from its TypeScript source. See decision 20 of [decisions.md](decisions.md#20-translating-from-source-counts-as-an-import). |
| `owner` | `pool` |
| `recorded_on` | The date of the commit that writes it. |
| `upstream.name` | `tasks-axi` |
| `upstream.repository` | `https://github.com/kunchenguid/tasks-axi` |
| `upstream.revision` | `9401ff899c0d1d8ae6b4fd8727b9025abda2032c`, the release commit of 0.2.6, committed 2026-09-22; on 2026-10-11 also the head of the upstream's main branch. The baseline the design names. |
| `upstream.tag` | `tasks-axi-v0.2.6` |
| `licence.spdx` | `MIT`, established from the root `LICENSE` file at that commit, not from the manifest's label alone. The manifest also declares MIT, and the README ends with the MIT notice. |
| `licence.copyright` | `Copyright (c) 2026 Kun Chen`, verbatim, the one notice the upstream carries. No file under its `src/` or `test/` carries a copyright or licence header. |
| `licence.files` | One entry: `packages/pool/upstream/tasks-axi/LICENSE`, the upstream's root `LICENSE` kept unchanged, 1,065 bytes, 21 lines, digest `sha256:945016bd37e1ba7211622ef60ee1d23ab727896ba7710edd21e8fbe983863969`. The upstream has no `NOTICE` file. |
| `portions` | The table below. |
| `retained_tests` | The table below. |
| `transitive` | The table below. |
| `excluded` | The table below. |
| `local_changes` | The paragraph below, kept current as the import is maintained. |
| `audit.audited_on`, `audit.statement` | The date and the statement of the audit below, completed by the build before the first translated file is committed. |

## Portions

Every Go file that holds translated material, with the upstream files it is translated from. Each is `translated`. Paths under `packages/pool/internal/` are the build plan's names; a lane that splits a file keeps the upstream mapping on each part and updates the record in the same commit.

| Hive file | Upstream files | What it carries |
| --- | --- | --- |
| `packages/pool/internal/legacy/grammar.go` | `src/backends/markdown-grammar.ts` | The pure parser and renderer of the legacy backlog, no I/O: sections and their suffixes, the three bullet forms, identifiers, the trailing tag region and every canonical tag, dependency edges with reasons, links derived from prose, the leading kind word, indented continuation lines, the public-followup marker kept as text, carriage returns, the final newline and the preamble, and the byte-exact round trip: an unmodified entry and every free-form line is emitted verbatim, a dirty entry is re-rendered canonically. |
| `packages/pool/internal/legacy/normalize.go` | `src/backends/markdown.ts` (normalization) | Normalization of titles, tags, links, holds, dates and dependency reasons as the store applies it before rendering a dirty entry. |
| `packages/pool/internal/legacy/importer.go` | `src/backends/markdown.ts` (load and validation), `src/config.ts` (the two default backlog names only, as documentation of what a caller may point the importer at) | Reading a file whole, the conflict rule that a file changed since it was loaded is refused, and the mapping of parsed entries to Pool items, holds and the migration record of [pool.md](pool.md#legacy-import-and-export). |
| `packages/pool/internal/legacy/exporter.go` | `src/backends/markdown.ts` (render and the `render` verb), `src/commands/maintain.ts` (`render`) | Export in the legacy form: verbatim for an unmodified item, canonical for a modified one, each item in the section of its current state, and the normalize mode that re-renders every item. |
| `packages/pool/internal/item/transition.go` | `src/backends/markdown.ts` (`transition`), `src/commands/state.ts` (`start`, `done`, `reopen`) | The queued, in-flight, done transitions and reopen, the closing date, and the evidence link recorded at done. |
| `packages/pool/internal/item/deps.go` | `src/backends/markdown.ts` (`addDep`, `removeDep`), `src/derive.ts`, `src/commands/state.ts` (`block`, `unblock`, `ready`) | Dependency edges with existence checks, the refusal to archive an item that active dependents are blocked by, and the derived blocked, held and ready facts, including the date-gated hold. |
| `packages/pool/internal/item/hold.go` | `src/backends/markdown.ts` (hold set and clear), `src/commands/state.ts` (`hold`, `unhold`) | Hold kinds, reasons and the `until` date, and the rule that a hold survives completion. |
| `packages/pool/internal/item/patch.go` | `src/backends/markdown.ts` (`update`), `src/commands/crud.ts` (`update`), `src/fields.ts`, `src/body.ts` | The patch that reports which fields changed, the idempotent no-op, body line addition, and the field and body helpers. |
| `packages/pool/internal/item/links.go` | `src/pr-url.ts`, `src/backends/markdown.ts` (link derivation) | Recognition of pull request URLs in their GitHub and Forgejo forms, report paths and generic URLs. |
| `packages/pool/internal/legacy/archive.go` | `src/backends/markdown.ts` (`prune`, the two archive files, the restore point) | Archive lookup for the export: how a legacy backlog's archive files are read so that an item pruned upstream still resolves on import, and the never-delete rule. Hive's own records are never pruned. |

`src/id.ts` is not a portion: Hive's identifiers are ULIDs minted by the contract library, and only its identifier pattern is carried, inside `grammar.go`, as the legacy identifier the parser accepts. `src/backends/lock.ts` is not a portion: Hive's transaction lock is the contract library's; its tests are re-expressed against that lock in work item 3 as native tests, not retained ones.

## Retained tests

Each is a Go test file that re-expresses the upstream test file it names, case for case where the behavior is kept, with the identifier, date and path forms changed to Hive's. Each test carries, in its file comment, the upstream path and the case names it re-expresses.

| Hive file | Upstream test | Cases |
| --- | --- | --- |
| `packages/pool/internal/legacy/grammar_test.go` | `test/backends/markdown-grammar.test.ts` | About 41: parsing and the byte-exact round trip. |
| `packages/pool/internal/legacy/store_test.go` | `test/backends/markdown.test.ts` | About 76: create, update, remove, transition, dependencies, holds, prune and the conflict on a changed file, re-expressed against the importer, exporter, patch, transition and archive files above. |
| `packages/pool/internal/item/state_test.go` | `test/commands/state.test.ts` | About 58: start, done, reopen, block, unblock, hold, unhold, ready; its move cases are left with the move. |
| `packages/pool/internal/item/crud_test.go` | `test/commands/crud.test.ts` | About 62: add, list, show, update, rm, as `add`, `read`, `update` and `transition` to archived. |
| `packages/pool/internal/legacy/maintain_test.go` | `test/commands/maintain.test.ts` | About 9: prune and render, as the archive lookup and normalize. |
| `packages/pool/internal/item/derive_test.go` | `test/derive.test.ts` | 9: blocked, held, ready, active blockers. |
| `packages/pool/internal/item/links_test.go` | `test/pr-url.test.ts` | The table of pull request URL forms. |
| `packages/pool/internal/legacy/id_test.go` | `test/id.test.ts` | 5: the legacy identifier validation only; minting is not carried. |

Fixtures: the two sample backlogs `test/fixtures/backlog.md` and `test/fixtures/firstmate-backlog.md` are **not taken as assets**. Each is re-expressed as a Hive fixture written for the purpose under `packages/pool/testdata/legacy/`, with invented identifiers and no private name, so that the round-trip tests run against files this repository owns. If a lane finds a case that needs the upstream bytes exactly, it takes that fixture as an `asset` portion and adds it to the record in the same commit, under the same licence.

## Transitive libraries

| Library | Version | Licence | Disposition |
| --- | --- | --- | --- |
| `@toon-format/toon` | `^2.1.0` | MIT by registry metadata, licence file not read | `replaced`: it encoded command output; Hive's command contract replaces it. Nothing of it is taken. |
| `axi-sdk-js` | `^0.1.10` | MIT by registry metadata, licence file not read | `replaced`: it supplied the CLI runner, the error type and exit codes, and the session-start hook installer; Hive's command contract and exit states replace all three. Nothing of it is taken. |

The development dependencies (`@eslint/js`, `@types/node`, `eslint`, `globals`, `prettier`, `tsx`, `typescript`, `typescript-eslint`, `vitest`, `yaml`) are compiled into nothing and are `dropped`. Because nothing of either runtime library is taken, their licence files need not be established for this import; the audit says so rather than labelling them.

## Excluded

| Upstream path | Reason |
| --- | --- |
| `src/public-followup.ts`, `src/commands/public-followup.ts`, `test/public-followup.test.ts`, `test/commands/public-followup.test.ts` | The upstream's own operating workflow for a promised public reply. The Pool keeps the marker and its body as text and translates none of the state machine. |
| `src/cli.ts`, `src/args.ts`, `src/toon.ts`, `src/format.ts`, `src/view.ts`, `src/confirm.ts`, `src/suggestions.ts`, `src/context.ts`, `src/commands/home.ts`, `src/commands/setup.ts`, `src/skill.ts`, `src/version.ts`, `bin/tasks-axi.ts`, and their tests `test/cli.test.ts`, `test/commands/home.test.ts`, `test/commands/setup.test.ts`, `test/suggestions.test.ts`, `test/skill.test.ts`, `test/format.test.ts`, `test/bin/version-fast-path.test.ts` | Presentation and packaging: argument parsing, output encoding, suggestions, the home and setup commands, the skill asset and the version fast path. None is the Pool's behavior; Hive's command contract is the interface. |
| `src/config.ts`, `test/config.test.ts` | Configuration from flags, environment variables and two configuration files. Hive reads no environment and no configuration file as an input to the contract; only the two default backlog names are carried, as documentation. |
| `src/errors.ts` | Error codes bound to the SDK's error type; Hive's exit states and codes replace them. The six legacy codes map to Hive's as `VALIDATION_ERROR` to `invalid`, `NOT_FOUND` to `not_found`, `LOCKED` to `conflict` `lock_held`, `CONFLICT` to `conflict`, `UNSUPPORTED` to `refused` `unsupported_operation`, `UNKNOWN` to `failed`. |
| `src/backends/lock.ts`, `test/backends/lock.test.ts` | The advisory lock with a token and stale time; Hive's transaction lock is the contract library's and is released by the operating system. The nine lock cases inform native tests of the Pool under the contract lock and are not retained. |
| `src/backends/markdown.ts` (`moveTo`, `moveManyTo`, `requireNoSplitDeps`), the move cases of `test/backends/markdown.test.ts` and `test/commands/state.test.ts` | The multi-item move is left for a later stage: the stage that builds routing defines moving items between homes on it as its base, and settles what [pool.md](pool.md#left-undecided) lists. Nothing of it is translated at stage 2. |
| `src/store.ts`, `src/model.ts` | The backend seam and the TypeScript data model: Hive's records are the schemas under `spec/schemas/records/`, and there is no second backend. Their shape informed the record schemas and nothing of them is translated. |
| `src/id.ts` (minting), `test/commands/mint-collision.test.ts` | Minting of `slug-xx` identifiers; Hive mints ULIDs. |
| `skills/tasks-axi/SKILL.md`, `scripts/build-skill.ts`, `scripts/guard-generated-files.sh`, `.github/workflows/*`, `test/release-ci-exclusions.test.ts`, `test/workflows/guard-generated-files.test.ts`, the release tooling | A generated asset and the upstream's build, release and workflow files. |
| `test/fixtures/module-trace-hook.mjs`, `test/fixtures/module-trace-register.mjs`, `test/helpers.ts` | Tooling of the version fast path and test helpers for the TypeScript test runner. |
| `README.md` and every other documentation file | Documentation; the plan quotes none of it. |

## Local changes

The translated material keeps the grammar, the round-trip rule, the transitions, the dependency and hold semantics, and the archive lookup of the upstream. Hive changes: identities are ULIDs, with the legacy identifier kept on the item; dates are UTC instants with the zone they were read in and the date as written; the three legacy states gain `archived`, and `rm` becomes a transition to `archived`; holds are records of their own with Hive's kinds, `captain` read as `word` and `load` as `resource`; prune is not carried for Hive's records and survives only as archive lookup for import; a persistent-supervisor entry is excluded from import rather than made an item; and every write goes through the contract library's atomic record and transaction instead of the upstream's temporary-file rename and lock.

## The audit still owed

Before the first translated file is committed, work item 2, lane a:

1. Reads the upstream at the exact revision, read-only, and confirms the licence file's digest and the absence of per-file headers, as stated above.
2. Confirms that the two fixtures, if any is taken as an asset, carry no private name, path or figure, and otherwise re-expresses them.
3. Confirms that no file listed as a portion embeds text from a third source: the grammar file's comments cite a design report of the upstream's own, which is not copied.
4. Records the audit's date and statement in the record, and the `NOTICE` the records produce.

Material whose provenance the audit cannot establish is not imported, and the lane says so in the record's `excluded` rather than relabelling it.
