# Cloud status: secret-handling contract revision

Branch: `claude/secret-handling-contract-4qx7lc`, created from `fm/hive-design-secrets-in-keychain`. Documentation only: `docs/design.md`, `REVISION-NOTES.md` and this file. `README.md` checked and unchanged; nothing under `spec/` edited.

## What ran

1. Read `docs/design.md` in full and the contracts under `spec/`.
2. Added the section "Secrets: Hive keeps none" to `docs/design.md`, pushed.
3. Replaced every restatement elsewhere in the document with a reference to that section (49 references), pushed.
4. Wrote `REVISION-NOTES.md`: spec sentences needing change, open choices under `secrets-*` keys, and the per-section log; pushed.
5. Ran the project's checks on the final document state.

## Results of the checks

| Check | Result |
| --- | --- |
| `make check` | exit 0 (gofmt check and go vet clean) |
| `make test-contract` | exit 0 (`contract/clock`, `contract/ids`, `contract/schema`, `contract/store` ok; `spec` has no test files) |

Go toolchain: go.mod requires 1.27; `go1.27.0 linux/amd64` was fetched and used.

done claude/secret-handling-contract-4qx7lc
