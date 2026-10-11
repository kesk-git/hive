# Lane status: examples for the record schemas

## What was done

Valid and invalid examples now prove the twelve record schemas through the
shared fixture walker in `contract/schema/fixtures_test.go`, which was not
changed. Each schema has its own directory under `contract/testdata/schemas/`,
named for its `$id` with the colons of a subdirectory schema written as
underscores, holding only `valid/` and `invalid/`. Every example is one `.json`
file named for the rule it proves: one accepted example per shape the schema
admits, and one refused example per rule it states (required members, unknown
members, patterns and their bounds, lengths, enumerations, constants,
conditional and cross-member rules, and the type of every member).

| Directory | Accepted | Refused |
| --- | --- | --- |
| `home` | 4 | 39 |
| `workspace` | 5 | 36 |
| `project` | 10 | 59 |
| `clone` | 9 | 62 |
| `session-message` | 11 | 82 |
| `receipts_dispatch.session-input.accepted` | 3 | 22 |
| `import-record` | 13 | 155 |
| `package` | 8 | 126 |
| `ops_hive-fleet.provision.request` | 5 | 48 |
| `ops_hive-fleet.provision.response` | 3 | 37 |
| `tx-intent` | 9 | 69 |
| `events_hive.journal.tail-repaired` | 3 | 21 |

The accepted examples of `package` include the committed declarations of
`dispatch`, `ui`, `synapse`, `pool` and the contract library as real instances.
Every identity in the examples is invented, in the style of the envelope
examples already there.

Every refused example was checked, outside the test, to fail first for the rule
its name states. Where a mutation necessarily trips a second rule as well (an
empty path fails both its minimum length and its pattern; removing a
discriminator such as `kind` also fires the conditional rules that depend on
it) the first violation is still the named one.

## The one edit under `spec/`

`spec/schemas/import-record.v1.schema.json`: the `pattern` of
`upstream.repository` held ten non-ASCII whitespace characters written raw
inside its character class (no-break space, Ogham space mark, the en quad and
hair space that bound a range, the line and paragraph separators, the narrow
no-break space, the medium mathematical space, the ideographic space and the
byte-order mark). Each is now written as its JSON escape, `\uXXXX`. The ASCII
members of the class, already written as JSON escapes or as a plain space, are
unchanged.

The parsed schema is the same value as before. This was verified mechanically
with two scripts kept outside the repository: a Go program using the standard
library's JSON decoder reports that the two files' bytes differ, that the
decoded documents are deeply equal and that the two pattern strings are
identical; a Python check with `json.load` agrees. The import-record fixtures
were also run against the schema as it was before the rewrite (with the edit
stashed) and give the same verdicts: every example accepted or refused the
same way.

Reading of the change rule in `spec/README.md`: a contract change edits the
prose, the schema and the declarations it touches and raises a version; a
minor change adds something, a major change is anything else. This rewrite adds
nothing and removes nothing from the schema as a JSON value, and `spec/README.md`
makes the schema file normative as a schema, which the validator reads as a
JSON value. The set of records it accepts is unchanged, so it changes no
contract and raises no version. No prose or declaration was touched.

The refused set is proved by the import record's examples: one refused example
per character the class names (tab, line feed, vertical tab, form feed,
carriage return, space, and the ten above), one for a character in the middle
of the ` `-` ` range, and accepted examples for the characters just
outside the class: a zero-width space (`​`), the character just before
the range (`῿`), the next-line control (`\u0085`) and ordinary non-ASCII
letters.

## Commands

Run with the installed Go, which fetched the module's pinned toolchain itself.

`make check` printed:

```text
go vet ./...
```

and exited 0 (the formatter in check mode printed nothing).

`make test-contract` printed:

```text
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.039s
ok  	github.com/kesk-git/hive/contract/clock	(cached)
ok  	github.com/kesk-git/hive/contract/ids	0.010s
ok  	github.com/kesk-git/hive/contract/schema	1.782s
ok  	github.com/kesk-git/hive/contract/store	3.778s
?   	github.com/kesk-git/hive/spec	[no test files]
```

and exited 0. Before the examples were added, both commands passed the same
way, so the fixture walker's refusal of a mistaken tree is what the new
examples were held to.

## Nothing left out

No file outside this lane's list was needed. The validator and the shared test
accepted every example as written, so nothing in `contract/schema/` is reported
as wrong. The specification was not found to contradict itself for these
files.

done claude/record-schema-fixtures-h70fc4
