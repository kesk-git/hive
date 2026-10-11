# UI

The UI owns the Hive UI: the Fleet overview, the Pool and the Swarm, the session dock and the optional reader, the on-demand task, Harvest, Shadow, History and Scheduled views, the Review artifact surface, artifact serving, decision cards over scoped projections, and every file of a review directory.

| | |
| --- | --- |
| Executable | `hive-ui` |
| Declaration | [`hive-package.json`](hive-package.json) |
| Test target | `test-ui` |
| Built in plan stage | 6 |

Code lives under `cmd/` (the one main package, `cmd/hive-ui/`) and `internal/` (everything else); no Go file sits directly in this directory. Fixtures for this package's own tests are under `testdata/`. The contracts are in [`spec/`](../../spec/README.md), starting with [`packages.md`](../../spec/packages.md) and [`layout.md`](../../spec/layout.md).
