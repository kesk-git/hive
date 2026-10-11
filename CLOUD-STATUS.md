# Cloud status: stage 2 definition

## What was done

The definition of the job for stage 2 of the ordered implementation plan, the Pool and the decision intake, as specification only:

- `spec/pool.md`, `spec/word.md` and `spec/import-plan-tasks-axi.md`, new.
- Eleven record schemas under `spec/schemas/records/` and seventy operation body schemas under `spec/schemas/ops/`, new. Every one passes the schema profile of `contract/schema`, every reference resolves, and no stage 1 schema changed.
- `packages/pool/hive-package.json` and `packages/word/hive-package.json` amended; both validate against `package.v1`.
- `spec/README.md` (index and version 1.2), `spec/test-targets.md`, `spec/receipts.md`, `spec/events.md` and `spec/decisions.md` edited by a sentence or a row each.
- `DEFINITION-SUMMARY.md` at the root, with the eight points.
- After the supervisor's review, four corrections in commit `8c098d9`: `spec/pool.md` states that stage 2 refuses `codex-void` unconditionally in the check itself and names the later change that un-parks it; `spec/word.md` states the secrets rule for the intake and what happens to free text kept exactly; `spec/import-plan-tasks-axi.md` names `src/backends/lock.ts` by its real path; the summary's point 5 records how the build plan reads the plan's order inside the stage, and the unconditional refusal, each with its alternative.

Nothing under `contract/`, `packages/` other than the two declarations, `Makefile`, `go.mod`, `spec/embed.go`, `integration/` or any `.go` file was touched. No source was imported, nothing was installed, no pull request was opened, and `main` was not touched.

The seven new identifier prefixes (`adm_`, `spec_`, `rule_`, `hold_`, `mem_`, `migr_`, `move_`) are declared in `spec/pool.md` and in the `$defs` of `records/pool.item.v1`, not in the table of `spec/identities.md` or in `common.v1`: `test-contract` holds that table and the `common.v1` patterns to the registry in `contract/ids`, which this job may not edit. Work item 1 of the build adds the rows and the constants in one commit.

## Branch

The work is on `claude/hive-stage2-pool-129teu`, the branch this session was opened on. `fm/hive-stage2-define` exists on the remote at the same base commit (`492ed1f`) and holds none of this work; the session's rules allow pushing only to its own branch, so the `claude/` branch is the one that holds the definition, as the task's fallback permits.

## Checks

Run on the final tree, after the corrections, with Go 1.27.0 as `go.mod` requires:

```
$ make check
go vet ./...
exit=0
$ make test-contract
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	(cached)
ok  	github.com/kesk-git/hive/contract/clock	(cached)
ok  	github.com/kesk-git/hive/contract/ids	(cached)
ok  	github.com/kesk-git/hive/contract/schema	(cached)
?   	github.com/kesk-git/hive/spec	[no test files]
exit=0
```

The cached results are those of the uncached run on the same schema tree, which the corrections did not touch: canon 0.040s, clock 0.005s, ids 0.010s, schema 15.065s, all ok.

Every JSON file under `spec/schemas/` and every package declaration parses. The amended declarations were validated against `package.v1` with a throwaway test that was not committed.

## Rulings

No `needs-ruling:` line: no stage 1 contract had to change beyond the minor additions the change rule allows. The product rulings that need the Hive Mind are listed in point 5 of `DEFINITION-SUMMARY.md`, each with a recommendation.

done claude/hive-stage2-pool-129teu
