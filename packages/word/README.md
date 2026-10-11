# Mind's word

Mind's word is the single decision intake: held questions, exact answers and completion receipts. It keeps no store of its own and persists through the Pool.

| | |
| --- | --- |
| Executable | `hive-word` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-word` |
| Built in plan stage | 2 |

Code lives under `cmd/` (the one main package, `cmd/hive-word/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
