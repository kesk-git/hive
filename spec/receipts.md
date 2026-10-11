# Receipts

A receipt is an immutable record that one named fact was established: by whom, for which request, in which scope, on what evidence. Everything Hive later claims rests on receipts, so a receipt says exactly one thing and never more than was proved.

Owner: the package that writes the receipt owns its kind. Schema: [`receipt.v1`](schemas/receipt.v1.schema.json). Proved by `test-contract` for the envelope and by the owning package's target for each kind.

## The envelope

| Field | Meaning |
| --- | --- |
| `schema`, `schema_version` | `hive.receipt` and the envelope's version. |
| `receipt_id` | The receipt's own identity. |
| `kind`, `kind_version` | What fact it establishes, and the version of that kind's body. |
| `written_at` | When it became durable. |
| `writer` | The package, command, operation and implementation version that wrote it. |
| `request_id`, `correlation_id` | The request whose handling wrote it and the chain it belongs to. |
| `scope_level`, `home_id`, `workspace_id`, `project_id`, `clone_id`, `task_id`, `generation` | The [scope](identities.md#scope) the fact holds in. |
| `source_revision` | The Git revision the fact is bound to, where it is bound to one. |
| `authority` | The authority the operation relied on, as the request gave it and the writer verified it. |
| `subject` | The record the fact is about, at the revision and digest it holds for. |
| `evidence` | The records the fact rests on, each with its role. Empty when the receipt is its own evidence. |
| `body` | The kind's own content. |

## Kinds

A kind is the owning package's name followed by one to three segments, in lowercase with hyphens inside a segment: `spawning-pool.acquire`, `dispatch.session-input.accepted`.

- A kind is registered in its package's declaration with the **one fact it asserts**, written as a sentence. A kind that could be read as asserting two facts is two kinds.
- A kind whose content needs more than the envelope has a body schema at `spec/schemas/receipts/<kind>.v<major>.schema.json`, named in the declaration. A kind that registers none has an empty body.
- Stages after the first register their kinds as they build them. Registering a kind is a minor change.

The kinds registered at stage 1:

| Kind | Asserts | Body |
| --- | --- | --- |
| `spawning-pool.acquire` | A worktree was leased to one task generation, with its physical worktree, base and branch. | Defined at stage 3 |
| `spawning-pool.observe` | The state of one lease and its worktree was read at the stated time. | Defined at stage 3 |
| `spawning-pool.release` | A lease was ended and its worktree returned, after the preservation checks passed. | Defined at stage 3 |
| `dispatch.home.created` | A private home was given its identity record. | Empty |
| `dispatch.workspace.registered` | A workspace was registered with the Fleet home that owns it. | Empty |
| `dispatch.project.registered` | A project was registered under one workspace. | Empty |
| `dispatch.clone.registered` | A physical clone was bound to one project. | Empty |
| `dispatch.session-input.accepted` | One input was placed durably in a session's canonical input sequence. | [`receipts/dispatch.session-input.accepted.v1`](schemas/receipts/dispatch.session-input.accepted.v1.schema.json) |

Stage 2 registers the thirty-two kinds of the Pool in [`packages/pool/hive-package.json`](../packages/pool/hive-package.json), each with an empty body, as [pool.md](pool.md#receipts-and-events) describes; the Mind's word registers none, because it writes none: the receipts `hive-word` returns are the Pool's, as [word.md](word.md#how-word-persists-through-the-pool) says.

## Rules

1. **Written once.** A receipt is a file at `<package>/receipts/<receipt_id>.json` in the writer's part of the home. It is never replaced, amended or deleted. A fact that later stops holding is answered by a new receipt of another kind, and the old one stays as history.
2. **One fact.** The design keeps these apart, and so does every kind: an accepted launch, observed activity, collected output, a published change, a passing gate, a readiness, an authorized request to land and a verified landing. Transport acceptance is not acknowledgment; acknowledgment is not completion; a Fleet's handling is not a worker's.
3. **Bound to what it holds for.** A receipt names its scope, its task generation where there is one, and its subject at a revision. A fact about a head names the head. A later revision of the subject, a new generation or a new head is not covered by it, and nothing carries it forward.
4. **Idempotent with its request.** Handling the same request again writes no second receipt. A receipt is found by `request_id`, kind and `subject`, because one request may write several receipts of one kind, one for each record it creates; recovery finds them by the receipt identifiers the transaction's intent retains. Every receipt of the request is returned.
5. **Complete or absent.** A receipt is written inside the transaction that establishes its fact. If the transaction does not commit, the receipt does not exist.
6. **Evidence, never authority.** A receipt proves that something was recorded. It grants nothing. Whether an authority suffices is decided by the operation that reads the authority's own record.
7. **Read through the owner.** Another package obtains a receipt from the response that wrote it or from the owning package's command. It never reads the file.
8. **Private.** A receipt never leaves its home except as a summary its owner writes for the parent, and never enters a repository, a Harvest, a comment or a log.
