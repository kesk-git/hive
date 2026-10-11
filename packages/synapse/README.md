# Synapse

Synapse owns validation obligations, review and fix custody, evidence and readiness, and the guarded Assimilation interfaces.

| | |
| --- | --- |
| Executable | `hive-synapse` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-synapse` |
| Built in plan stage | 5 |

Code lives under `cmd/` (the one main package, `cmd/hive-synapse/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
