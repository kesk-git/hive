# Repository layout

Hive is one repository and one Go module. The seven packages are directories of that module, each closed to the others by the layout itself.

```text
README.md
LICENSE                              added by its own change, before any import
NOTICE                               third-party notices, generated from imports/
.gitattributes                       line feeds kept in every checkout, on every platform
.github/
  workflows/                         the one workflow that runs make test on GitHub's hosted Windows and Linux runners, on main, on its nightly schedule and on demand at the head landing a Target's last Prey
.claude/
  settings.json                      repository configuration for Claude Code: includeCoAuthoredBy and attribution.sessionUrl, both false, keep AI co-author and session trailers off
.no-mistakes.yaml                    repository configuration for the validation pipeline: holds the fixed gate command, `make check` and every built target of class `check`, so no model chooses what runs; a change that makes such a target built adds it there in the same change
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
integration/
  calls/                             tests of the command contracts between packages
  distribution/                      the checks of the distribution contract
  imports/                           the checks of the import records
  testcmd/                           a test-only command built from contract/, never delivered
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
7. **Process execution has one path.** Only `contract/exec` starts a process, by any means: it alone imports the standard library's process-execution package, `os/exec`, calls `os.StartProcess` or a process-creation function of `syscall`, or reaches a platform's own process-creation call through a system library. Every other package runs an external executable or another Hive command through it. See [distribution.md](distribution.md#the-one-process-execution-path).
8. **Page files are hand-written.** The `ui` package's HTML, CSS and JavaScript live under `packages/ui/internal/assets/` as standard files that the platform's own web engine loads as they are, inside Hive's own app, and are embedded into `hive-ui`. The repository has no bundler, no transpiler, no package manifest of another ecosystem and no generated page file.
9. **Fixtures stay with their owner.** A package's fixtures are under its `testdata/`. Fixtures for each schema are under `contract/testdata/schemas/<name>/` (for major 1) or `contract/testdata/schemas/<name>_v<major>/` (for later majors): accepted examples are files under `valid/`, schema-refused examples are rows of `invalid.jsonl`, and decoder-refused or byte-sensitive examples are files under `invalid/`. Nothing under a `testdata/` directory is read by a delivered executable.

    Each line of `invalid.jsonl` is one UTF-8 JSON object followed by a line feed, with exactly `name`, `base` and `edits`; empty lines, a byte-order mark, carriage-return line endings, invalid UTF-8, duplicate members and unknown members are refused. Rows are sorted by their distinct `name`, the case's filename-safe basename without `.json` or slash, backslash or dot segments, and `base` is a direct `.json` basename under the same directory's `valid/`. A row and a file under `invalid/` must not name the same case. A schema directory holds only `valid/`, `invalid.jsonl` and `invalid/`, with at least one accepted file and at least one refused row or file for each schema whose fixtures are owed under [test-targets.md](test-targets.md#schema-fixture-obligations).

    `edits` is a nonempty ordered array of objects holding `op` and `path`, and exactly one `value` for `add` or `replace` and no `value` for `remove`. `path` is a JSON pointer, empty for the root or beginning with `/`, whose only escapes are `~0` and `~1`; it is not a URI fragment. Each edit applies to a fresh decoded copy of the accepted base in the row's schema reference, in array order: `add` creates an absent object member, `remove` deletes an existing object member, and `replace` changes an existing object member, an existing array element or the whole root. Every parent must exist and be a container; array indexes are in-bounds nonnegative decimal integers with no leading zero except `0`, and array additions, array removals and `-` indexes are refused. A changed array length is written as replacement of that array. Each metadata string, edit `value` and base is decoded separately with the existing Hive JSON decoder without changing its accepted numbers, strings, object members or value-depth limit; table and edit envelopes use a standard-library token reader that refuses duplicate and unknown members. An invalid base, an edit that cannot be applied or a constructed row that the decoder refuses is a fixture error, never proof of a schema refusal. A constructed invalid row must decode successfully and be refused with a schema validation error; a raw invalid file may be refused by either the decoder or the schema.

    For the schema directory `common`, a file or row name is `<definition>.<label>`, with `.json` appended for a file, and selects that definition under `#/$defs/`; a row's base selects the same definition. Other directories select the schema named by the directory, with colons of its identifier written as underscores and a terminal `_v<major>` selecting a major after 1; an unsuffixed name selects major 1. Table rows use the Go standard JSON encoder with HTML escaping disabled, no indentation and one final line feed. A schema refusal of a decodable document is a row; a case the decoder refuses, or that depends on its byte spelling, is a file under `invalid/` with its original bytes.
10. **Upstream notices stay with the package that maintains the import**, under `packages/<name>/upstream/<import_id>/`, as the [import record](imports.md) lists them.
11. **The landing test.** The reader and tense questions apply to every path a change adds or changes; the exposure question applies to every path and to the Harvest body and the commit messages.
    - *Reader:* who reads it: the build, a test, the delivered application, or a person using or changing the current code? If only the work's supervisors, the operator or an audit read it, it is a record.
    - *Tense:* does the path state the present, edited in place, or record an event: a run, a lane, a head, a date, a result, who said what? An event is a record. A version note under `spec/versions/` is the one exception. An import record's pinned upstream identity and recording date describe the dependency as it is now and pass; the licence and copyright notices an import keeps verbatim are a third party's notice, not anyone's own words.
    - *Exposure:* read it as a stranger would. It holds no home or machine path or user name, no account, token or credential detail, no instruction text, no one's verbatim words and no private decision id standing in for its substance.

    A path that fails the first or second question does not land: it moves to its owner in the private home, in the same lane. A line that fails the third is rewritten first. A Harvest body and the commit messages state the requirement and the outcome, what changed, the risk, the testing and the gate's findings; they never quote the Hive Mind and they pass the exposure question. What never enters a path: notes, Imperatives, proof, captures, logs, scan results, progress or review records, transcripts and anyone's own words. Fixtures are written for the purpose and use invented identities. The same test governs the repositories of the projects Hive runs; a record already committed there moves out in one cleanup lane for that project, and git history keeps it, with nothing rewritten.
12. **The same bytes in every checkout.** Every committed text file keeps its line feeds in a checkout on every platform, so that the schemas embedded into an executable and the fixtures a test reads are the same bytes on macOS, Windows and Linux; the repository's own attributes say so, and no checkout setting of a platform may translate them. No two committed paths differ only in case, and no committed name holds a character or a device name that one of the three platforms refuses, under the rules [records.md](records.md#encoding) states for a path.
13. **No AI watermark.** No AI co-author or session trailer appears in a commit or a merge commit, and no generated-by line in a pull request, a comment or a file. The repository's `.claude/settings.json` keeps Claude Code's co-author trailer and session trailer off.

## File naming

- Schema files: `<name>.v<major>.schema.json`, with `$id` `urn:hive:schema:<name>:v<major>`. Under a subdirectory the directory joins the identifier with a colon: `receipts/dispatch.session-input.accepted.v1.schema.json` has `$id` `urn:hive:schema:receipts:dispatch.session-input.accepted:v1`.
- Go source follows the standard formatter and the standard layout of a package directory; test files end `_test.go` and sit beside the code they test.
- Markdown files are lowercase with hyphens, except `README.md`.
