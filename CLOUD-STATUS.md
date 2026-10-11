# Cloud status: stage 2 definition

## What was done

The definition of the job for stage 2 of the ordered implementation plan, the Pool and the decision intake, as specification only:

- `spec/pool.md`, `spec/word.md` and `spec/import-plan-tasks-axi.md`, new.
- Eleven record schemas under `spec/schemas/records/` and seventy operation body schemas under `spec/schemas/ops/`, new. Every one passes the schema profile of `contract/schema`, every reference resolves, and no stage 1 schema changed.
- `packages/pool/hive-package.json` and `packages/word/hive-package.json` amended; both validate against `package.v1`.
- `spec/README.md` (index and version 1.2), `spec/test-targets.md`, `spec/receipts.md`, `spec/events.md` and `spec/decisions.md` edited by a sentence or a row each.
- `DEFINITION-SUMMARY.md` at the root, with the eight points.
- After the supervisor's review, four corrections in commit `8c098d9`: `spec/pool.md` states that stage 2 refuses `codex-void` unconditionally in the check itself and names the later change that un-parks it; `spec/word.md` states the secrets rule for the intake and what happens to free text kept exactly; `spec/import-plan-tasks-axi.md` names `src/backends/lock.ts` by its real path; the summary's point 5 records how the build plan reads the plan's order inside the stage, and the unconditional refusal, each with its alternative.
- After the definition's acceptance, corrections A to D in commit `3424074`, touching only `spec/pool.md`, `spec/word.md` and `spec/schemas/records/pool.item.v1.schema.json`: the store names five places and adds `pool/tmp/<request_id>.tmp`, from which nothing of the Pool's is read; every decision carries its alternative beside it in the two files, with the six decisions the files did not list added, so all seventeen of the summary's point 5 are in the repository; `secret_ref` holds `store`, `host`, `account` and `purpose`, all required and nothing else, grants nothing, and the two No secret value rules say so, with the refusal of a value in free text or a preserved `unknown` member left to Hive's secret-handling contract; and the four display choices are left to the stage that builds the UI, each with the recommendation recorded as the default. `DEFINITION-SUMMARY.md` and the contract-version sentence of `spec/README.md` were not edited, as instructed.

## Branch

The work is on `claude/hive-stage2-pool-129teu`, the branch this session was opened on. `fm/hive-stage2-define` exists on the remote at the same base commit (`492ed1f`) and holds none of this work; the session's rules allow pushing only to its own branch, so the `claude/` branch is the one that holds the definition, as the task's fallback permits.

## Checks

Run on the final tree, after corrections A to D, with Go 1.27.0 as `go.mod` requires:

```
$ make check
go vet ./...
exit=0
$ make test-contract
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.039s
ok  	github.com/kesk-git/hive/contract/clock	(cached)
ok  	github.com/kesk-git/hive/contract/ids	0.007s
ok  	github.com/kesk-git/hive/contract/schema	14.946s
?   	github.com/kesk-git/hive/spec	[no test files]
exit=0
```

The schema tests ran uncached against the corrected `secret_ref` definition. Every JSON file under `spec/schemas/` and every package declaration parses; neither declaration changed in this round.

## Rulings

No `needs-ruling:` line: no stage 1 contract had to change beyond the minor additions the change rule allows. The product rulings that need the Hive Mind are listed in point 5 of `DEFINITION-SUMMARY.md`, each with a recommendation. The four display choices among them are now recorded in spec/pool.md and spec/word.md as defaults the UI stage starts from.

done claude/hive-stage2-pool-129teu
