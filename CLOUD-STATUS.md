# Cloud status: Pool contract levels and the four corrections

Branch: `claude/pool-contract-levels-zaqvo1`, from `fm/hive-stage2-hierarchy-amend`.

## Checks run

`make check`:

```
go vet ./...
```

exit 0 (the formatter in check mode printed nothing).

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon
ok  	github.com/kesk-git/hive/contract/cli
ok  	github.com/kesk-git/hive/contract/clock
ok  	github.com/kesk-git/hive/contract/envelope
ok  	github.com/kesk-git/hive/contract/ids
ok  	github.com/kesk-git/hive/contract/journal
ok  	github.com/kesk-git/hive/contract/schema
ok  	github.com/kesk-git/hive/contract/store
?   	github.com/kesk-git/hive/spec	[no test files]
```

exit 0. The Go toolchain the module requires was fetched without trouble.

Beside the two targets, a throwaway test (not committed) ran the new conditionals of the item record, the `add` request and the `hive-rule amend` request through the embedded registry: a Target with a `wave` and no placement, a Hunt with a placement, a legacy item without a level and a decision item without one are accepted; a decision item with a level, a Target with a placement or a deliverable, a `wave` off a Target, an item off a Target without a placement, a `wave` with ordinal 0, an `add` without a level for a non-decision, and a lift without its receipt are refused.

No Pool fixtures were needed: the fixture tree under `contract/testdata/schemas/` holds none for the Pool's schemas, and `contract/` is fenced.

## One sentence not applied: blocked

The sentence "In `packages/pool/hive-package.json`: the two error codes `pool.hierarchy` and `pool.derived_state`, and `pool.not_dispatchable` if it is not already there" cannot be applied with the files of this change.

- The file: `spec/schemas/package.v1.schema.json`, a stage 1 schema outside the list.
- Why: the package declaration has no member that lists error codes. Its schema is closed (`additionalProperties` false) and registers codes only by prefix: the `kind_prefixes` member is "the first segment of every record, receipt, event and error name the package owns", and `pool` is already declared there, so `pool.not_dispatchable` and the two new codes are owned by the declaration as it stands. Adding a member for the codes would make the declaration invalid under its schema, which `test-contract` is to validate, and the schema is not mine to change. The declaration is left unchanged; the three codes are in the error-code table of `spec/pool.md`.

Every other sentence of both parts is in the contract, below. The contract version moved to 1.5.

## Added mid-task

The two sentences that used the old word for a level, in `spec/packages.md` (the Pool's row of the owners table) and `spec/naming.md` (the opening of "Keeping it apart from the Pool"), now say a Pool item at the level of a Target, a Prey or a Hunt. Nothing had been committed when the addition arrived, so the correction is in the one commit of this change. The three sentences about scheduled jobs in `spec/packages.md`, `spec/records.md` and `spec/test-targets.md` are untouched.

## Where each sentence landed

Part one, the levels.

- What an item is, the three levels and the decision without one, who works what: `spec/pool.md`, section "Items", opening paragraph.
- The member `level`, required by `add` for a non-decision, lacking on an imported item until set: `spec/pool.md`, "Items" > "Levels" (first bullet), "What an item carries" (the `level` row and the `add` paragraph); `spec/schemas/records/pool.item.v1.schema.json` (`level` property, `$defs/level`, the conditional that forbids it on a decision); `spec/schemas/ops/hive-pool.add.request.v1.schema.json` (`level` property and the conditional requiring it off a decision); `spec/schemas/ops/hive-pool.update.request.v1.schema.json` (`level` in the patch).
- A Target carries no deliverable, placement, delivery axes or generations; `hive-imperative create` refuses it and a Prey with a Hunt with `pool.not_dispatchable`: `spec/pool.md`, "Levels" (second bullet), "What an item carries" (the `deliverable`, `placement_requested`, `delivery` and `generations` rows), "The Imperative" (the `create` bullet), "Error codes" (the `pool.not_dispatchable` row); `spec/schemas/records/pool.item.v1.schema.json` (the Target conditional, and `placement_requested` required only off a Target); `spec/schemas/ops/hive-pool.add.request.v1.schema.json` (the Target conditional); `spec/schemas/ops/hive-imperative.create.request.v1.schema.json` (description).
- The member `wave`, on a Target only, a grouping and never a level or an owner: `spec/pool.md`, "Levels" (third bullet) and "What an item carries" (the `wave` row); `spec/schemas/records/pool.item.v1.schema.json` (`wave` property, `$defs/wave`, forbidden where the level is not `target`); `spec/schemas/ops/hive-pool.add.request.v1.schema.json` and `spec/schemas/ops/hive-pool.update.request.v1.schema.json` (`wave`).
- The `parent` edge's direction, `pool.hierarchy`, a Target's `blocked-by` edges: `spec/pool.md`, "Levels" (fourth bullet), "What an item carries" (the `deps` row and the `add` paragraph), "Error codes" (the `pool.hierarchy` row); `spec/schemas/records/pool.item.v1.schema.json` (`$defs/dependency` description); the `deps` and `add_deps` descriptions of the `add` and `update` request schemas.
- Derived `landed`, and a Target `building`: `spec/pool.md`, "Items" > "Derived facts" (the new paragraph); `spec/schemas/ops/hive-pool.read.response.v1.schema.json` (`landed` and `building` in `derived_item`, and the file's description).
- The two refusals, `pool.derived_state` on a transition to done and `pool.not_dispatchable` on opening a generation: `spec/pool.md`, "Items" > "State" (the transition table, rows to `in-flight` and to `done`), "Derived facts" (last sentence), "Generations" (the `open-generation` bullet), "Error codes" (the `pool.derived_state` row); `spec/schemas/ops/hive-pool.transition.request.v1.schema.json` and `spec/schemas/ops/hive-pool.open-generation.request.v1.schema.json` (descriptions).
- The definition states on a Target, inherited by a Prey or a Hunt: `spec/pool.md`, "Items" > "State", the paragraph on `definition`.
- Legacy import takes no level and no parent from a title: `spec/pool.md`, "Legacy import and export", the `import-legacy` paragraph.
- "schedule record" for "scheduled-job record": `spec/pool.md`, "What an item carries", the `schedule` row.
- `spec/naming.md`: rule 6 of "Keeping it apart from the Pool" says column.
- No new identifier prefix: `spec/pool.md`, "Identifiers", last sentence.
- The declaration's error codes: blocked, above.

Part two, the four corrections.

- A lift carries its receipt whole and the Pool checks it, calling nothing: `spec/pool.md`, "Rules", the `amend` bullet; `spec/schemas/ops/hive-rule.amend.request.v1.schema.json` (`receipt`, required for a lift, and the description); `spec/schemas/records/pool.rule.v1.schema.json` (`receipt` on an amendment, recorded with the lift).
- Export suppresses no archived item: `spec/pool.md`, "Legacy import and export", the `export-legacy` paragraph; `spec/schemas/ops/hive-pool.export-legacy.request.v1.schema.json` (description).
- The return from in flight to queued: `spec/pool.md`, "Items" > "State", the new row of the transition table and the last bullet below it; `spec/schemas/ops/hive-pool.transition.request.v1.schema.json` (description).
- Freshness compared by the rules digest: `spec/pool.md`, "Generated knowledge", the last bullet; `spec/schemas/ops/hive-memory.check.response.v1.schema.json` (`state` description); `spec/schemas/ops/hive-pool.read.response.v1.schema.json` (`derived_projection` description).

The proofs these sentences add: `spec/pool.md`, "What `test-pool` proves", and `spec/test-targets.md`, the `test-pool` row. The version: `spec/README.md`, the contract-version paragraph, 1.5 with its clause.

Two reworded phrases beyond the sentences, so that the Pool contract uses none of the old words for a level: "the first change of the build" for a phrase that used one of them, in "Identifiers" and in decision 13 of `spec/pool.md`.

blocked claude/pool-contract-levels-zaqvo1
