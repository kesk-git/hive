# Shadow

The Shadow owns bounded observations, due polls, due-time wakes for scheduled jobs, dated holds and rule expiries, durable wakes and their acknowledgments, and reconciled state.

| | |
| --- | --- |
| Executable | `hive-shadow` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-shadow` |
| Built in plan stage | 4 |

Code lives under `cmd/` (the one main package, `cmd/hive-shadow/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
