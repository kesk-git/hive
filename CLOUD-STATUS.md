# Cloud status: stage 2 definition

## What was done

The definition of the job for stage 2 of the ordered implementation plan, the Pool and the decision intake, as specification only:

- `spec/pool.md`, `spec/word.md` and `spec/import-plan-tasks-axi.md`, new.
- Eleven record schemas under `spec/schemas/records/` and seventy operation body schemas under `spec/schemas/ops/`, new. Every one passes the schema profile of `contract/schema`, every reference resolves, and no stage 1 schema changed.
- `packages/pool/hive-package.json` and `packages/word/hive-package.json` amended; both validate against `package.v1`.
- `spec/README.md` (index and version 1.2), `spec/test-targets.md`, `spec/receipts.md`, `spec/events.md` and `spec/decisions.md` edited by a sentence or a row each.
- `DEFINITION-SUMMARY.md` at the root, with the eight points.

Nothing under `contract/`, `packages/` other than the two declarations, `Makefile`, `go.mod`, `spec/embed.go`, `integration/` or any `.go` file was touched. No source was imported, nothing was installed, no pull request was opened, and `main` was not touched.

The seven new identifier prefixes (`adm_`, `spec_`, `rule_`, `hold_`, `mem_`, `migr_`, `move_`) are declared in `spec/pool.md` and in the `$defs` of `records/pool.item.v1`, not in the table of `spec/identities.md` or in `common.v1`: `test-contract` holds that table and the `common.v1` patterns to the registry in `contract/ids`, which this job may not edit. Work item 1 of the build adds the rows and the constants in one commit.

## Branch

The work is on `claude/hive-stage2-pool-129teu`, the branch this session was opened on. `fm/hive-stage2-define` exists on the remote at the same base commit (`492ed1f`) and holds none of this work; the session's rules allow pushing only to its own branch, so the `claude/` branch is the one that holds the definition, as the task's fallback permits.

## Checks

Run on the final tree, with Go 1.27.0 as `go.mod` requires:

```
$ make check
go vet ./...
exit=0
$ make test-contract
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.040s
ok  	github.com/kesk-git/hive/contract/clock	0.005s
ok  	github.com/kesk-git/hive/contract/ids	0.010s
ok  	github.com/kesk-git/hive/contract/schema	15.065s
?   	github.com/kesk-git/hive/spec	[no test files]
exit=0
```

Every JSON file under `spec/schemas/` and every package declaration parses. The amended declarations were validated against `package.v1` with a throwaway test that was not committed.

## Rulings

No `needs-ruling:` line: no stage 1 contract had to change beyond the minor additions the change rule allows. The product rulings that need the Hive Mind are listed in point 5 of `DEFINITION-SUMMARY.md`, each with a recommendation.

done claude/hive-stage2-pool-129teu
