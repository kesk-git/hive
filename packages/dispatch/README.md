# Dispatch

Dispatch owns session launch and control; Fleet provisioning and the registry of workspaces, projects and clones, including each home's identity record; effective policy; the native quota, context and usage readers; reservations; scheduled-job records; steering and lifecycle control; and continuity.

| | |
| --- | --- |
| Executable | `hive-dispatch` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-dispatch` |
| Built in plan stage | 4 |

Code lives under `cmd/` (the one main package, `cmd/hive-dispatch/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
