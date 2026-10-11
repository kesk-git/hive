# Pool

The Pool is the durable work queue: Pool storage and migrations, admission records, Imperatives, accepted specifications, original rules and their projections (including temporary rules with an expiry or lift condition), and generated knowledge projections. It is the one store in which Mind's word persists decisions.

| | |
| --- | --- |
| Executable | `hive-pool` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-pool` |
| Built in plan stage | 2 |

Code lives under `cmd/` (the one main package, `cmd/hive-pool/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
