# Repository layout

Hive is one repository and one Go module. The seven packages are directories of that module, each closed to the others by the layout itself.

```text
README.md
LICENSE                              added by its own change, before any import
NOTICE                               third-party notices, generated from imports/
go.mod                               the one module and the pinned Go release
Makefile                             the test, check and build targets
docs/
  design.md
spec/
  README.md                          index of the contracts
  *.md                               one contract per file
  embed.go                           embeds schemas/ so executables validate against these files
  schemas/
    <name>.v<major>.schema.json      contract-level records
    receipts/<kind>.v<major>.schema.json
    events/<kind>.v<major>.schema.json
    records/<kind>.v<major>.schema.json
    ops/<command>.<operation>.request.v<major>.schema.json
    ops/<command>.<operation>.response.v<major>.schema.json
contract/
  hive-package.json                  declaration of the shared library
  <area>/                            Go packages any Hive package may import
packages/
  <name>/                            pool, spawning-pool, dispatch, shadow, synapse, word, ui
    hive-package.json                the package declaration
    README.md                        what the package owns, for a reader of the code
    cmd/<executable>/                the package's one main package
    internal/                        all of the package's code
    testdata/                        fixtures for the package's own tests
    upstream/<import_id>/            upstream licence and notice texts, unchanged
integration/                         tests of the command contracts between packages
imports/
  README.md
  <import_id>.json                   one import record per body of upstream material
dist/                                build output, never committed
```

Of these, this definition creates `spec/`, the eight `hive-package.json` files and `imports/README.md`. Everything else is named here and created by the build.

## Rules

1. **One module.** `go.mod` sits at the root and is the only one. Its module path is the repository's public path. There is no workspace file and no nested module.
2. **A package's code is under its own `internal/`.** Go refuses an import of `packages/<a>/internal/...` from anywhere outside `packages/<a>/`. That is the mechanical form of the design's rule that no package opens another's store: a package cannot link another package's code at all, so the only way across is the other package's command.
3. **No Go file sits directly in `packages/<name>/`.** The directory name of the Spawning Pool contains a hyphen, which a Go package name cannot. Keeping that directory free of Go files means the question never arises; code lives in `cmd/` and `internal/`.
4. **One main package per Hive package**, at `packages/<name>/cmd/<executable>/`, where `<executable>` is the `executable` of the declaration. See [commands.md](commands.md#executables-and-command-names).
5. **`contract/` is the only shared code.** Any package may import it. It imports nothing from `packages/`. It holds no store, no command and no rule that belongs to one package.
6. **`spec/` is embedded, not copied.** `spec/embed.go` is the one Go file under `spec/`. It embeds `schemas/` and exports it to `contract/`. No schema is duplicated into a package.
7. **Process execution has one path.** Only `contract/exec` imports the standard library's process-execution package. Every other package runs an external executable or another Hive command through it. See [distribution.md](distribution.md#what-a-hive-executable-may-run).
8. **Browser files are hand-written.** The `ui` package's HTML, CSS and JavaScript live under `packages/ui/internal/assets/` as standard files that a browser loads as they are, and are embedded into `hive-ui`. The repository has no bundler, no transpiler, no package manifest of another ecosystem and no generated browser file.
9. **Fixtures stay with their owner.** A package's fixtures are under its `testdata/`. Fixtures for the schemas are under `contract/testdata/schemas/<name>/valid/` and `.../invalid/`. Nothing under a `testdata/` directory is read by a delivered executable.
10. **Upstream notices stay with the package that maintains the import**, under `packages/<name>/upstream/<import_id>/`, as the [import record](imports.md) lists them.
11. **Nothing private is committed.** No private home, no record from one, no credential, no machine path and no decision text enters the repository. Fixtures are written for the purpose and use invented identities.

## File naming

- Schema files: `<name>.v<major>.schema.json`, with `$id` `urn:hive:schema:<name>:v<major>`. Under a subdirectory the directory joins the identifier with a colon: `receipts/dispatch.session-input.accepted.v1.schema.json` has `$id` `urn:hive:schema:receipts:dispatch.session-input.accepted:v1`.
- Go source follows the standard formatter and the standard layout of a package directory; test files end `_test.go` and sit beside the code they test.
- Markdown files are lowercase with hyphens, except `README.md`.
