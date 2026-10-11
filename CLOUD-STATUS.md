# Status: the secrets contract change

Branch `claude/spec-secrets-contract-yc6qd7`. One contract change, version 1.4 of `spec/`, makes every amendment the design's section "Secrets: Hive keeps none" listed under "Accepted contracts this section amends", and deletes that subsection from the design. The two sentences that linked to the subsection, in the stage-1 plan item and in "Where the parts enter the plan", now say the amendments were made.

## Rows applied

One line per row of the list, with the file changed.

- `records.md`, text kept exactly: a writer refuses whole, `invalid` with `secret_shape_recognized`, a body holding a listed credential shape; no body holds a value of the secret type, which has no encoding. `spec/records.md`.
- `records.md`, migration and `unknown`: the owner's no-value check runs over the whole body, `unknown` included, before the first byte; a recognized value refuses the record whole. `spec/records.md`.
- `records.md`, writing one record: a first step validates the content with the no-value check before any temporary file is opened; the store's writer validates nothing. Steps renumbered. `spec/records.md`.
- `records.md`, writing several records: a first step validates every body of the transaction before the intent's temporary file exists. Steps renumbered. `spec/records.md`.
- `commands.md`, environment: a command sets a child's environment only through Tendril; a resolved value appears in no argument, response, receipt or log. `spec/commands.md`.
- `commands.md`, exit 3: a request carrying a listed credential shape is `invalid`. `spec/commands.md`.
- `commands.md`, exit 8: a missing or unusable platform utility and a locked, incomplete or unsupported secret store are `unavailable`. `spec/commands.md`.
- `commands.md`, `invalid` codes: `secret_shape_recognized` added. `spec/commands.md`.
- `commands.md`, `unavailable` codes: `secret_store_locked`, `secret_missing`, `platform_utility_missing`, `platform_utility_unusable` and `secret_store_unsupported` added, with one sentence saying what they name. `spec/commands.md`.
- `commands.md`, the `error` row: `message` and `detail` hold no value and never a request's text; the descriptions of `error.message` and `error.detail` say the same. `spec/commands.md`, `spec/schemas/response.v1.schema.json`.
- `commands.md`, capabilities: an operation also declares the platform utilities it may run, in its own `platform` member, which `test-contract` compares with the package's list. `spec/commands.md`.
- `distribution.md`, what an executable may run: a fifth kind, a platform utility declared under `platform`. `spec/distribution.md`.
- `distribution.md`, `contract/exec`: it builds the child's environment from nothing; the one declared variable Tendril sets is the only way a value enters a child; a program that needs a value is started directly, never through tmux. `spec/distribution.md`.
- `distribution.md`, the `platform` lists: empty at stage 1 and otherwise empty except for a utility the design names, the first being the keychain tool, declared by `dispatch` and `synapse` in the stages that build them; a platform utility is not a seventh dependency. `spec/distribution.md`.
- `distribution.md`, network: or under the one value Tendril resolved for a program run under the identity a Spoor names. `spec/distribution.md`.
- `distribution.md`, proof item 6: a recorded fake of the keychain tool is reachable, standing for the declared platform utility. `spec/distribution.md`.
- `packages.md`, `dispatch` may call `word`, from stage 4, for the one Mind's word about the store; the graph stays acyclic. `spec/packages.md`.
- `packages.md`, `synapse` may call `dispatch`, from stage 5, for the publisher's Spoor lookup. `spec/packages.md`.
- `packages.md`, what `dispatch` owns: the Spoor records, their enrolment check and their status projection. `spec/packages.md`.
- `packages.md`, the contract library: the no-value check with its shape list; Tendril and the value type. `spec/packages.md`.
- `packages.md`, rule 6: or the platform utilities listed under `platform`, each with its reason. `spec/packages.md`.
- `identities.md`: the prefix `spoor_`, a Spoor, minted by `dispatch`. `spec/identities.md`; the constant, the registry entry and a mint vector in `contract/ids/`.
- `events.md`, rule 4: validated also for the fixed list of credential shapes, a match refusing the event whole. `spec/events.md`.
- `session-messages.md`: a part holding a listed shape is refused whole, `invalid` with `secret_shape_recognized`, never redacted; a refused streaming update leaves the message as last recorded. `spec/session-messages.md`.
- `review.md`, rule 1: the same exception, refused whole under rule 2 with `secret_shape_recognized`. `spec/review.md`.
- `test-targets.md`, rule 3: the keychain tool joins the programs a test runs as a recorded fake; real keychain behaviour is a Mac-only qualification outside the targets. `spec/test-targets.md`.
- `test-targets.md`, the `test-distribution` row: the recorded fake of each declared platform utility is reachable. `spec/test-targets.md`.
- `test-targets.md`, proofs by stage: the proofs the Secrets section names added to the rows of `test-contract`, `test-pool`, `test-dispatch`, `test-synapse` and `test-ui`, each saying the stage it enters. `spec/test-targets.md`.
- `decisions.md`, decision 6: after the owner's no-value check. `spec/decisions.md`.
- `decisions.md`, open points: the adapter for a secret store a user chooses on Linux. `spec/decisions.md`.
- `schemas/capabilities.v1.schema.json`: the `executes` enumeration kept; an optional `platform` member added to the operation, an array of names under the pattern of `platform.name` in `package.v1`. `spec/schemas/capabilities.v1.schema.json`, with one accepted and two refused examples under `contract/testdata/schemas/capabilities/`.
- `schemas/package.v1.schema.json`: "Expected to stay empty." dropped from the `platform` description. `spec/schemas/package.v1.schema.json`.
- `packages/dispatch/hive-package.json` and `packages/synapse/hive-package.json`: not changed, by the lane's fence. The declarations gain their calls and their `platform` entry in the stages that build them, as the design's "Where the parts enter the plan" says; the contract sentences that name those entries and their stages are in `spec/distribution.md` and `spec/packages.md`.
- `README.md`: contract version raised to 1.4, the next free minor number, with a sentence in the form of the earlier ones. `spec/README.md`.

The design: the subsection deleted; its two incoming links replaced by sentences saying the amendments were made. `docs/design.md`.

## Sweep

Sentences elsewhere in `spec/` that the section made untrue, applied the same way:

- `records.md`, the home table, the `dispatch/` row: it also holds, from stage 4, the Spoor records. `spec/records.md`.
- `commands.md`, standard error: like every log, it carries no value. `spec/commands.md`.

Checked and left standing: `receipts.md`, `imports.md`, `layout.md`, `naming.md`, the remaining rules of `events.md` and `review.md`, the request rules of `commands.md`, the platform decision of `decisions.md`, and the descriptions of the intent, outcome and open-member schemas.

## Notes for the maintainer

- The contract library's envelope package carries a constant naming the contract version it implements, and that constant was raised together with the specification's version in the change that added it. It is landed code outside this lane's files and was not changed: this build does not implement the shape recognizer, the value type or the `platform` member, so the version it reports is the one it implements. Raise it in the stage that builds those parts, or now if the two are meant to move together.
- The capabilities decoder in the same package reads named members only, so a declaration carrying the new `platform` member is read without error and the member is ignored until the stage that compares it with the package's list.

## Checks

`make check` printed:

```
go vet ./...
```

and exited 0, with the formatter reporting no file.

`make test-contract` printed:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon
ok  	github.com/kesk-git/hive/contract/cli
ok  	github.com/kesk-git/hive/contract/clock
ok  	github.com/kesk-git/hive/contract/envelope
ok  	github.com/kesk-git/hive/contract/ids
ok  	github.com/kesk-git/hive/contract/journal
ok  	github.com/kesk-git/hive/contract/schema
ok  	github.com/kesk-git/hive/contract/store
?   	github.com/kesk-git/hive/spec	[no test files]
```

The Go toolchain the module requires was fetched and used; both targets ran in full.

## Commits

- `424224d1e01f07efe5ea7a1db10d468538ecffa7`: the contract change.
- The commit that adds this file follows it on the same branch.

done claude/spec-secrets-contract-yc6qd7
