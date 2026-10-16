# Cloud status

## Files changed and what each creation site now does

All changes are in test files under `integration/calls/`. No production file, spec, contract package or `integration/distribution` file was touched.

- `support_test.go`
  - `newHome` makes the root with `store.MakeDir`, then secures it with `store.SecureDir`. It creates `home.json` with `store.CreateFile`, then writes and closes it. It no longer uses `os.Mkdir` or `os.WriteFile`.
  - New helpers `secureHome`, `makeDir` and `makeFile` wrap the public store primitives. `contract/internal/...` is not imported.
  - `makeFile` opens with `O_WRONLY|O_CREATE|O_TRUNC`, writes and closes. A file that is already there is emptied and keeps its owner and access.
- `pool_callers_test.go`
  - `plantBudget` creates `pool/records/budget` with `makeDir` (`store.MakeDir`) and `memory.json` with `makeFile` (`store.CreateFile`). The raw `os.MkdirAll` and `os.WriteFile` are gone.
- `word_pool_support_test.go`
  - `wpWrite` goes through `makeFile`. It empties and rewrites a file that is already in the home and keeps its owner and access.

Searched for every other creation site in `integration/calls`: no copy or clone helper of a home exists.

- Every home is built by `newHome`, directly or through `newWpFixture` and `newPcWorld`.
- `os.WriteFile`, `os.Create` and `os.Mkdir` remain only outside homes, and they stay as they are:
  - the per-call `request.json` in a temporary directory;
  - the oversize request file in the work root;
  - the fake executables and their directories in `searchpath_test.go`;
  - the pass-through `hive-pool` directory and its `arrived` and `release` marker files;
  - the root of the work directory.
- Homes handed to real commands get only a secured root and `home.json`. Production code writes everything else.

## Deliberate-mutation sites audited

All three go through `wpWrite`, which only empties and rewrites a file that already exists. Each file keeps the owner and access the production code gave it, so the only fault is its content.

- `word_pool_test.go`, in `TestWordPoolUnavailableWhenARecordedOutcomeIsMalformed`: the Pool's recorded outcome is overwritten with a truncated JSON document. The fault is a malformed recorded outcome (`record_malformed`).
- `pool_callers_test.go`, `spoil(…, "malformed")`: a record is overwritten with `this is not a record`. The fault is a malformed record.
- `pool_callers_test.go`, `spoil(…, "version")`: a record is rewritten with `schema_version` 99.0. The fault is an unsupported version.

Test readers: none changed. No test reads a home while a store lock is held.

- `snapshot` never reads `locks/`.
- The held-handoff test in `word_pool_test.go` snapshots after the first request returns. Word holds no store lock, and the held pass-through has not yet reached the real Pool.

## Gaps and file stops

None. No edit outside my files was needed. No store primitive was found to create an entry with the wrong owner or access, though I could only read the code, not run it on Windows.

## Check results

- The declared command `make check test-contract test-pool test-spawning-pool test-dispatch test-shadow test-synapse test-word test-ui build test-integration check-imports` passed in this session (Go 1.27.0, linux/amd64), exit 0, no failing package. The `integration/calls` suite passed. `TestNoOtherDescriptorReachesTheChild` did not fail here.
- `GOOS=windows go vet ./integration/...` is clean. `GOOS=darwin go vet ./integration/...` is clean too.
- Windows itself was not run. The Windows reading of these changes is the hosted run on main after the merge.
- The first commit has no red-first run: the macOS and Linux paths check mode bits, and the failing owner and access-list checks are Windows-only.

## Session facts

- model equals the one this session was launched with: yes
- effort equals high and permission mode equals auto: yes

done claude/attachment-preflight-check-qli6p0
