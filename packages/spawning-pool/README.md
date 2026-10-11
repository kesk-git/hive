# Spawning Pool

The Spawning Pool holds the Hive-owned worktrees and their leases, and the acquire, observe and release receipts that bind a physical worktree to one home, workspace, project, clone, task generation and branch. It holds working copies and never work items.

| | |
| --- | --- |
| Executable | `hive-spawning-pool` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-spawning-pool` |
| Built in plan stage | 3 |

Code lives under `cmd/` (the one main package, `cmd/hive-spawning-pool/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
