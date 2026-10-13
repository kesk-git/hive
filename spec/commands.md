# Command contract

Every `hive-*` command is called the same way, answers the same way and fails the same way. This file is that one contract. A package's own operations add their bodies to it and change nothing in it.

Owner: the contract library implements the harness; each package owns its commands. Schemas: [`request.v1`](schemas/request.v1.schema.json), [`response.v1`](schemas/response.v1.schema.json), [`capabilities.v1`](schemas/capabilities.v1.schema.json). Proved by `test-contract` for the envelopes and the harness, by each package's target for its own operations, and by `test-integration` for every call between packages.

## Executables and command names

Each package builds **one executable**, named in its declaration: `hive-pool`, `hive-spawning-pool`, `hive-dispatch`, `hive-shadow`, `hive-synapse`, `hive-word` and `hive-ui`, each with the `.exe` suffix on Windows. Every other command name of a package is a link to that package's executable, and the executable acts as the command whose name it was started under: the base name of the program as started, without its `.exe` suffix on Windows, where it is compared without regard to case as Windows compares names. The delivered `bin/` directory therefore holds seven executables and twenty-one links, and all twenty-eight names behave as separate commands on all three platforms; [distribution.md](distribution.md#the-artefact) says what a link is on each.

A name the executable does not own exits 2.

## Invocation

```text
hive-<command> <operation> --home <directory> [--request-file <path>] [--response-file <path>]
hive-<command> capabilities
hive-<command> --version
```

- `<operation>` is the first argument and is one of the operations the command declares.
- `--home` names the private home. It is required for every operation except `capabilities`. There is no default home, no home taken from the environment and no search for one. Where a home sits is [records.md](records.md#the-home)'s rule, Hive's root, which tells a caller which directory to name and never names it for a command.
- **The request is never an argument.** It is read from the file `--request-file` names, or from standard input when that option is absent.
- The response is written to the file `--response-file` names, or to standard output when that option is absent.
- A command reads no environment variable as an input to the contract. It uses the environment only as any process does: to find the external executables it runs, and for those executables' own configuration. A command sets a child's environment only through Tendril, the launch primitive of the [one process-execution path](distribution.md#what-a-hive-executable-may-run), and a value it resolved appears in no argument, response, receipt or log, under the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section.
- No argument is longer than 4,096 bytes. A longer one exits 2 before anything is read.
- `--version` prints the implementation version as one line of text and exits 0. It is the only invocation that answers without a response document.
- Standard error carries diagnostics for a person. It is never parsed, and nothing in it is part of the contract. Like every log, it carries no value.

No command offers a short form, for a person at a terminal or for any caller: the request document is the one form, as [decision 8](decisions.md#8-requests-never-travel-as-arguments-and-the-home-is-always-named) states, and no flag or positional argument beyond those of the invocation above builds a request. A shape the design writes, such as `hive-assimilate <task-id> <harvest-url>`, names for illustration the identities the request document carries, and is not a command line. Calls between packages always send the request document.

## The request

One JSON document, following `request.v1`:

| Field | Meaning |
| --- | --- |
| `schema`, `schema_version` | `hive.request` and the envelope's version. |
| `command`, `operation` | Must equal the command and operation invoked. A difference is `invalid`. |
| `operation_version` | The version of the operation's body schema the caller wrote against. |
| `request_id` | The replay identity. See [Replay](#replay). |
| `correlation_id` | Shared by every request, receipt and event of one chain of work. A caller that acts because of something passes on the identifier it was given. |
| `causation` | The request, receipt or event that directly caused this one, where there is one. |
| `issued_at` | When the caller wrote the request. |
| `caller` | Who issued it: a package's command, a supervisor session, a worker, the operator or a test. It is attribution. It authenticates no one and grants nothing. |
| `scope_level`, `home_id`, `workspace_id`, `project_id`, `clone_id`, `task_id`, `generation` | The [scope](identities.md#scope). |
| `source_revision` | The Git revision the operation is bound to, where it is bound to one. |
| `expected_revisions` | Each record the mutation depends on and the revision the caller read. |
| `authority` | A reference to the recorded authority the operation relies on: a Mind's word, a recorded posture, a charter, a rule or an accepted instruction, or `none`. The callee reads the record and decides whether it suffices. A reference is never itself authority. |
| `body` | The operation's own request, validated against the schema its capability declaration names. |

A request is **closed**: an unknown field anywhere outside `body` is `invalid`, and so is one inside `body` under the operation's own schema. A callee never ignores what it does not understand.

## The response

One JSON document, following `response.v1`, for every invocation that gets as far as starting.

| Field | Meaning |
| --- | --- |
| `request_id`, `command`, `operation` | Echo the request. `request_id` is null only when none could be read. |
| `status`, `exit_code` | The outcome, as the table below fixes it. |
| `responder` | The package, command, implementation version and contract version that answered. |
| `completed_at` | When the response was written. |
| `replayed` | True when this repeats the recorded outcome of an earlier invocation. |
| `receipts` | Every [receipt](receipts.md) the invocation wrote, in full. |
| `body` | The operation's own response. Present only with `ok`. |
| `error` | Present with every other status: a `code`, a `message` for a person, `retryable`, and the fields the status requires. `message` and `detail` hold no value and never the text of a request: a refusal names what it refused by field and shape only. |

**A response is complete or it is not a response.** It is one document followed by a line feed. A caller accepts it only if the whole document parses, follows the schema, echoes the caller's `request_id` and agrees with the process's exit code. A `request_id` that is not null is the caller's in every case. A null `request_id` is accepted for a request only with a status the callee reaches before it can read the request's identity: `usage`; `invalid` with code `malformed_request`; and `too_large` that names the request as what exceeded the limit, with code `record_too_large`, `detail.record` of `request` and `actual_bytes` above `limit_bytes`. Any other response with a null `request_id`, a `too_large` about the response included, is `unknown`. The `capabilities` invocation takes no request, and its response is accepted only with a null `request_id` and the operation `capabilities`; no other response is accepted against a request's identity as a capabilities response. Empty output, output that does not parse, or a status that disagrees with the exit code is treated as `unknown`, never as success and never as an empty result. With any status but `ok` there is no `body`: no partial result is ever returned.

## Exit and error states

| Exit | `status` | Meaning | What was changed |
| --- | --- | --- | --- |
| 0 | `ok` | The operation did what it defines. Every change is durable. | What the operation defines |
| 1 | `failed` | The command failed for a reason of its own. | Nothing |
| 2 | `usage` | The invocation itself was wrong: an unknown command name or operation, a missing option, an argument too long. | Nothing |
| 3 | `invalid` | The request was read and rejected: it does not parse, breaks a schema, names a version the command does not accept, or carries a string of a credential shape on the fixed list. | Nothing |
| 4 | `refused` | The request is well formed and the contract forbids it: missing or insufficient authority, a scope or home that does not match, an operation that is unsupported or parked. | Nothing, or a record of the refusal |
| 5 | `conflict` | The request lost to current state: an expected revision differs, a lock is held, the identifier exists with other content. | Nothing |
| 6 | `not_found` | The thing named does not exist in this home. | Nothing |
| 7 | `too_large` | The request, the response or a record in it exceeds a declared limit. | Nothing |
| 8 | `unavailable` | Something the operation needs cannot be used: a core executable is missing or refuses, a source cannot be read, an authoritative record is unreadable or malformed, a platform utility is missing or unusable, or the secret store in use is locked or closed to the process, lacks the item or has no adapter. | Nothing |
| 9 | `unknown` | The outcome cannot be established. An intent is recorded and something outside Hive may or may not have happened. | Unknown; reconcile before any retry |
| 10 | `held` | The operation is valid and waits on a recorded hold, which `error.hold` names. | A record of the hold, where the operation writes one |

- Exit codes 11 to 63 are reserved for this contract. Hive never exits with 64 or above, so no code a platform gives a process that something else ended is mistaken for one of Hive's: on macOS and Linux a shell reports a signal as 128 plus its number, and on Windows a process ended by an exception or by Control-C exits with a status of `0xC0000000` or above.
- An outcome is read from a complete response that agrees with the exit code, never from an exit code alone. A process that ends without a complete response has given no response, whatever its exit code, and the caller treats the outcome as `unknown`: one ended by a signal on macOS or Linux; one ended on Windows by `TerminateProcess` or `TerminateJobObject`, whose exit code is whatever the terminator chose, and is 1, Hive's `failed`, when Go's own `Kill` chose it; and one ended by a caller's timeout. How the [one process-execution path](distribution.md#what-a-run-reports) reports every way a run can end is stated there.
- `ok` means what the operation defines and no more. An operation that records a launch and receives the adapter's acceptance returns `ok` for exactly that; it says nothing of the worker running, finishing or delivering.
- `refused`, `held` and `unknown` may leave a record: the refusal, the hold or the intent. Every such record is a receipt returned in `receipts`. No other non-`ok` status leaves anything.
- `retryable` is true only when repeating the same request unchanged can succeed without anyone acting first, as with `lock_held`. It is false for `unknown`: an unknown outcome is reconciled, not retried.

### Common error codes

A package's own codes are written `<package>.<code>`. These common ones are written bare, and a package never redefines one.

| Status | Codes |
| --- | --- |
| `failed` | `internal`, `io_failure` |
| `usage` | `usage`, `unknown_operation`, `argument_too_large` |
| `invalid` | `malformed_request`, `schema_violation`, `unsupported_version`, `envelope_mismatch`, `secret_shape_recognized` |
| `refused` | `unsupported_operation`, `operation_parked`, `authority_missing`, `authority_insufficient`, `home_mismatch`, `home_inside_repository`, `scope_mismatch`, `clone_identity_mismatch` |
| `conflict` | `revision_conflict`, `lock_held`, `already_exists`, `replay_mismatch` |
| `not_found` | `not_found`, `home_not_found` |
| `too_large` | `record_too_large` |
| `unavailable` | `dependency_missing`, `dependency_unusable`, `authentication_refused`, `source_unreadable`, `record_malformed`, `secret_store_locked`, `secret_missing`, `platform_utility_missing`, `platform_utility_unusable`, `secret_store_unsupported` |
| `unknown` | `outcome_unknown` |
| `held` | `held` |

Adding a code is a minor change. A code is never reused for another meaning. `secret_shape_recognized` is the code of the shape recognizer of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section; the five codes from `secret_store_locked` to `secret_store_unsupported` are the conditions of the platform's secret store and, on macOS, its utility that the same section lists, for each of the three platforms, and an error carrying one names the Spoor, or for a tool's own login the tool, the store and the condition, and never the value.

## Size

- Each command declares `max_request_bytes` and `max_response_bytes`. The default for both is 67,108,864 bytes; a command may declare more or less, but never less than holds every `outcome_unknown` answer of a durable transaction: that of a commit that was interrupted, and that of a complete transaction whose recorded response is larger than the limit now declared. Neither answer is ever replaced by `too_large`, which would promise that nothing changed.
- A request larger than the limit is `too_large` before it is parsed. A response that would be larger is `too_large` in place of the response, unless its transaction is already durable, when the answer is `outcome_unknown`.
- The error carries `code` `record_too_large`, `limit_bytes` and `actual_bytes`, and names in `detail` which record or field was too large where the command knows.
- Because a request never travels as an argument, no request is bounded by the platform's limit on arguments. The integration target proves this for every call between packages with a record of 2,097,152 bytes, which is above that limit on every supported platform: the record arrives whole, or the caller receives `too_large` and nothing else.
- A caller passes a large request by file, never by building a longer argument.

## Replay

`request_id` makes a mutating operation safe to send again.

- A mutating command records the outcome of each request it completes, with the digest of the request's canonical form, or for a [handoff](#a-command-with-no-store) the digest its causation names, under its store's `requests/`, as a record of schema [`hive.request-outcome`](schemas/request-outcome.v1.schema.json). A recorded outcome that fails its schema is `unavailable` with code `record_malformed` and is never replayed.
- The same `request_id` with the same digest returns the recorded response with `replayed` true, and writes nothing.
- The same `request_id` with a different digest is a `conflict` with code `replay_mismatch`.
- A request that ended `unknown` is not recorded as complete. Sending it again makes the command reconcile first: it establishes what happened from its intent and from the outside system, and only then completes, refuses or reports `unknown` again. It never repeats an outside effect it cannot rule out.
- Read-only operations record nothing and may be repeated freely.
- Recorded outcomes are kept for at least as long as the task or session the request is bound to, and their retention beyond that is the owning package's configuration.

### A command with no store

A package whose declaration names no home path has no store, no lock and no `requests/`, and records nothing itself. At stage 2 `word` is the one, and it persists through `pool`, the one package its declaration calls. The harness of the contract library gives such a command its replay through that callee's store, in three steps, and the callee's harness keeps its side.

1. **Read first.** For a mutating operation, after the request is validated and the home, scope and caller are checked and before the operation reads anything else or mints an identity, the harness reads the callee's recorded outcome of the request's `request_id`, through the callee's read of kind `request-outcome`, `hive-pool read` for `word`. That read recovers an interrupted transaction of the callee first. An outcome whose `request_digest` equals the digest of the request in hand is returned, adapted to the operation's own response as the operation defines, with `replayed` true and the recorded receipts, and nothing else is done; a different digest is a `conflict` with code `replay_mismatch`, and nothing is sent; no outcome lets the operation run.
2. **One handoff.** The operation then sends exactly one mutating request to the callee, a **handoff**: the same `request_id`, `correlation_id` and scope, `caller` the package and its command, and `causation` its own request, a reference of schema `hive.request` whose `id` is that same `request_id` and whose `digest` is the digest of its canonical form. Its response carries the callee's receipts in full. A callee response of `unknown`, or none, is `unknown`: nothing is recorded, and a retry reads first again.
3. **Keyed on the original.** The callee's harness treats a request as a handoff when its caller names a package that declares no home path and its causation is a `hive.request` of its own `request_id` with a `digest`; every other request is keyed on its own digest, as above. For a handoff, the digest it records as the outcome's `request_digest`, and compares on a repeat, is the causation's: the same `request_id` with the same causation digest returns the recorded response with `replayed` true, though the callee's own body differs by the identities the caller minted anew, and a different causation digest is `replay_mismatch`. Two identical requests of the command with no store that race past the first step therefore both receive the outcome of whichever reached the callee first.

A callee operation may accept only handoffs, as the six operations of [word.md](word.md#how-word-persists-through-the-pool) do. Read-only operations of a command with no store read through its callee and record nothing, like any other read.

## Capabilities

`hive-<command> capabilities` takes no request and no home, touches no store, runs no external executable and exits 0 with a response whose `body` follows `capabilities.v1`. Its `request_id` is null.

It declares the command's package and executable, the contract and implementation versions, the limits, and for every operation of the command other than `capabilities` itself:

- its `state`: `supported`, `unsupported` or `parked`, with a reason for the last two;
- its `mode`: `unary` for one request and one response, or `resident` for an operation that writes its response once it has durably started and then runs until stopped, as the Shadow's observer, Hive's own app and a session host of the terminal host do;
- whether it mutates, the body versions it accepts and the schemas of its request and response bodies;
- the receipt kinds it writes, the external executables it may run and the commands of other packages it may call;
- the platform utilities it may run, carried in the operation's own `platform` member, each a name from the package's declared `platform` list. The member exists from `schema_version` 1.1 of the declaration; a declaration at 1.0 holds none and is still read. `test-contract` compares that member with the package's `platform` list as it already compares `executes`.

An operation the design names and the build has not reached is declared `unsupported`; calling it is `refused` with `unsupported_operation`. An operation that is built and held closed by a recorded decision is `parked`; calling it is `refused` with `operation_parked`. Neither ever falls back to another route.

A command whose only operation is `capabilities` declares an empty list of operations: no operation is invented to fill it, and its package's declaration lists none either. What a command declares must agree with its package's declaration: `test-contract` compares them.

## What a command guarantees

1. It validates the whole request before it reads or writes anything else.
2. It checks the home, the scope and the authority before it changes anything.
3. It makes its changes in one [transaction](records.md#writing-several-records), or says exactly how its several transactions are joined.
4. It records an intent before any effect outside Hive and a receipt after, so that a crash between the two is found and reconciled.
5. It returns `ok` only after every change is durable.
6. It never reports a pending, unverified or unreadable outcome as success, and never turns an unsupported or unavailable route into another one.
