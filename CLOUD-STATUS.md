# Cloud status: schema envelope fixtures

## What was built

- `contract/testdata/schemas/<name>/valid/` and `.../invalid/` for the six
  contract-level schemas: `common`, `request`, `response`, `capabilities`,
  `receipt`, `event`. 700 example files in all, each pure JSON in the record
  encoding (UTF-8, no byte-order mark, whole numbers, no duplicate member,
  one trailing line feed), with invented identities and no machine path,
  private name or credential.

  | Schema | Accepted | Refused |
  | --- | --- | --- |
  | common | 94 | 232 |
  | request | 6 | 63 |
  | response | 13 | 70 |
  | capabilities | 3 | 60 |
  | receipt | 6 | 77 |
  | event | 5 | 71 |

  The accepted examples are complete records of each kind the schema
  distinguishes: every caller kind and authority kind for a request; every
  one of the eleven command-outcome statuses for a response (plus a replayed
  response carrying two receipts and the `capabilities` response whose
  `request_id` is null); a capabilities declaration with no operations and
  one with a supported unary, a supported resident, an unsupported and a
  parked operation; the six stage-1 receipt kinds; and events with a receipt,
  an observation and a record basis, a Shadow source with an observed
  source, an unknown `occurred_at`, and the journal's own tail-repaired
  event. Each refused example breaks one rule and is named for it: every
  required member missing, a wrong type, a member outside the closed object,
  each pattern, enum, const, length, size and range bound, each branch of
  every if/then/else and allOf, and the scope and authority cross-member
  rules. Each was checked to trip the rule its name claims and no other.

  `common` has at least one accepted and several refused examples for every
  one of its 47 definitions, named `<definition>.<label>.json`.

- `contract/schema/fixtures_test.go` (package `schema`, standard library
  only), with two tests:
  - `TestFixturesProveTheCommittedSchemas` is the one walker for every
    schema's fixtures. It uses `Embedded()` and `Registry.Validate`. A
    directory `<name>` names the schema `urn:hive:schema:<name>:v1`, colons
    written as underscores for a schema under a subdirectory (`receipts_x`).
    A file under `common/` is checked against
    `urn:hive:schema:common:v1#/$defs/<definition>`. A directory naming no
    schema, a common file naming no definition, a file that is not `.json`,
    a folder inside `valid/` or `invalid/`, a stray entry beside them, a
    stray file at the root, and a `valid/` or `invalid/` with no example all
    fail. A refused example means `*ValidationError` or `*SyntaxError`
    (`errors.As`); any other error is a failure. A failing file is reported
    by path with every violation.
  - `TestFixtureWalkerRefusesAMistakenTree` builds a temporary tree holding
    each kind of mistake and checks that every one is reported, by path, and
    that a correct example beside them is not. It found one gap while being
    written (an accepted example naming an unknown definition was reported
    as refused rather than unchecked), which was fixed.

  Later lanes add example files for the other schemas and need no change to
  the test.

## Choices stated

- "A schema directory holding no example in either folder fails" is read as
  each of `valid/` and `invalid/` must hold at least one example, since
  `spec/test-targets.md` asks that every schema be proved against valid and
  invalid fixtures. A schema directory with examples on one side only fails.
- The refused examples break schema rules only. None is syntactically
  invalid JSON or outside the record encoding, so every committed example is
  a record in the Hive encoding as the lane asks; the walker still counts a
  `*SyntaxError` as a refusal for a lane that wants one.
- The walker uses the unexported `Registry.lookup` once per directory to
  tell a directory naming no schema from an empty one; it is in package
  `schema`, as the lane names.

## Validator behaviour

Nothing looked wrong. Every accepted example is accepted and every refused
example is refused on the intended rule. One case worth knowing: a response
with no `status` member at all trips every exit-code branch, because each
`if` with `properties.status` is vacuously true without the member; that is
draft 2020-12 semantics, and the example is still refused as it should be.

## What each command printed

`make check` (Go 1.27.0, linux/amd64):

```
go vet ./...
```

exit 0.

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.053s
ok  	github.com/kesk-git/hive/contract/clock	0.004s
ok  	github.com/kesk-git/hive/contract/ids	0.012s
ok  	github.com/kesk-git/hive/contract/schema	1.462s
?   	github.com/kesk-git/hive/spec	[no test files]
```

exit 0.

## Not done

Nothing in the lane was left out. No file outside the lane's list was
touched: `spec/`, `docs/`, the `Makefile`, the landed code under
`contract/schema/` and every other package are unchanged.

done claude/schema-fixtures-test-frn0ii
