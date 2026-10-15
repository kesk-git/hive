# Identities

A private home, a workspace, a project and a clone are four separate things with four separate identities, even in a deployment that starts with one of each. This file defines the form of every identifier, the four identity records, the scope that every request, receipt and event carries, and how identities come to exist.

Owner: `dispatch`, through `hive-fleet`. Schemas: [`home.v1`](schemas/home.v1.schema.json), [`workspace.v1`](schemas/workspace.v1.schema.json), [`project.v1`](schemas/project.v1.schema.json), [`clone.v1`](schemas/clone.v1.schema.json). Proved by `test-contract` for the shapes and by `test-dispatch` for the behavior.

## Identifier form

Every identifier Hive mints is a registered prefix, an underscore and a ULID in lowercase:

```text
<prefix>_<26 characters>        ws_01jb2m3n4p5q6r7s8t9vwxyz0a
```

- The ULID is the standard one: 48 bits of millisecond time and 80 bits from the operating system's random source, written in Crockford base 32.
- It is written in **lowercase only**. An identifier becomes a file name, and the default file systems of macOS and Windows do not tell upper case from lower, while Linux's do: lowercase keeps every identifier one name on all three.
- An identifier is opaque. Nothing reads meaning from its time part, and nothing sorts by it where order matters; order comes from sequences and revisions.
- An identifier is minted once, by the party the table below names, and is never reused, shortened or derived from a name.
- An identifier is unique across homes, and it still never selects an owner on its own: a request that names a task also names the task's home, and a mismatch is refused.

| Prefix | Identifies | Minted by |
| --- | --- | --- |
| `home_` | a private home | the caller that provisions it; `dispatch` adopts it |
| `ws_` | a workspace | `dispatch` |
| `proj_` | a project | `dispatch` |
| `clone_` | a clone | `dispatch` |
| `sess_` | a supervisor conversation | `dispatch` |
| `msg_` | a session message | `dispatch` |
| `inp_` | one input to a session | the client that sends it |
| `task_` | a Pool item | `pool` |
| `q_` | a question | `word` |
| `dr_` | a decision round | `word` |
| `card_` | a decision card | `word` |
| `lease_` | a Spawning Pool lease | `spawning-pool` |
| `rev_` | a review | `ui` |
| `fev_` | a review feedback event | the page that captures it |
| `note_` | a review note | the page that captures it |
| `resp_` | a response in a review reply | `ui` |
| `req_` | a request | the caller |
| `corr_` | a correlation | the first caller of a chain |
| `rcpt_` | a receipt | the package that writes it |
| `evt_` | an event | the package that writes it |
| `spoor_` | a Spoor, the reference to one secret under the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section | `dispatch` |
| `adm_` | an admission record | `pool` |
| `spec_` | a specification | `pool` |
| `rule_` | a rule | `pool` |
| `hold_` | a hold on an item | `pool` |
| `mem_` | a knowledge projection | `pool` |
| `migr_` | a legacy migration | `pool` |
| `mnote_` | a maintenance note | `pool` |
| `miss_` | a miss record | `pool` |

A later stage registers a new prefix by adding a row here. A prefix is one to eight lowercase letters and digits, begins with a letter, and is never reused for another kind of thing.

Two identities are not ULIDs. A **generation** is a whole number from 1, scoped to its task or its session. A **round** and a review **revision** are whole numbers from 1, scoped to their review. None is ever reused.

## The four identity records

Each record follows the [record format](records.md) and is replaced atomically with its `record_revision` raised by one.

| Record | File in the owning home | Identity | Bound to |
| --- | --- | --- | --- |
| Home | `home.json` | `home_id` | The directory that holds the file, and for a Fleet home its one `workspace_id` and its parent's `home_id`. |
| Workspace | `dispatch/registry/workspaces/<workspace_id>.json` | `workspace_id` | Its Fleet home and the Hive's home. Its `slug` is fixed. |
| Project | `dispatch/registry/projects/<project_id>.json` | `project_id` | Its workspace, and its repository: a GitHub repository by host, owner and name, or `local-only` with no forge identity. Its `slug` is fixed and unique in the workspace. |
| Clone | `dispatch/registry/clones/<clone_id>.json` | `clone_id` | Its project, and the physical path of its Git common directory. |

**A home** is the Hive's own (`role` `hive`, no workspace, no parent) or the home of one workspace's Fleet (`role` `fleet`). The record holds no path: a home is found by being named, and may be moved as a directory without changing its identity. A home is never inside a Git working tree; a command given such a home refuses with `home_inside_repository`.

**A workspace** is named by its `name`, which is also the name of its Fleet. A name is a label: changing it changes no identity, re-homes nothing and moves no project.

**A project** keeps its `project_id` if its repository is renamed or moved; the repository binding is an attribute that changes under a new `record_revision`. Validation and publication defaults and authority references are not identity. They belong to the workspace charter, which stage 4 defines, and refer to the project by `project_id`.

**A clone** is one physical copy. Two clones of the same origin are two clones with two identities, and the recorded origin is evidence, never the key. The identity is bound to the physical path of the clone's Git common directory, with every symbolic link and, on Windows, every junction resolved, recorded in the form the platform's file system reports for it, as [records.md](records.md#encoding) states for an absolute path. Before any operation that acts on a clone, the acting package resolves the common directory of the path it was given and compares it with the record by file identity, the device and inode on macOS and Linux and the volume and file index on Windows, never by the spelling of the path: on the default file systems of macOS and Windows two spellings that differ only in case name one directory, and a link or a junction gives a directory a second name on all three. A difference is refused with `clone_identity_mismatch`. A linked worktree of a clone resolves to that clone's common directory and so to that clone. A clone that has been moved is re-bound by an explicit `hive-fleet` operation of a later stage, never by resemblance.

Identity records are retired by `state`, never deleted, so that an old receipt always resolves.

## Scope

Every request, receipt and event says where it belongs with the same flat fields, and the schemas apply one shared rule to them.

| `scope_level` | Meaning | Required | Must be absent |
| --- | --- | --- | --- |
| `hive` | The Hive's own projectless work | `home_id` | `workspace_id`, `project_id`, `clone_id` |
| `workspace` | One workspace as a whole | `home_id`, `workspace_id` | `project_id`, `clone_id` |
| `project` | One registered project | `home_id`, `workspace_id`, `project_id` | none; `clone_id` is given where the operation touches a clone |

`task_id` and `generation` are given wherever the operation concerns a task; a `generation` is never given without its `task_id`.

- Scope is declared, never inferred. A missing `scope_level` is a schema violation, and project work never inherits a project from a default, a current directory or the last one used.
- `home_id` in a request must equal the `home_id` of the home the command was pointed at. A difference is refused with `home_mismatch` before anything is read.
- The callee checks that the workspace, project and clone named exist in that home's registry, are `active` and belong to each other. It reads the home record and the registry records through the contract library's validated reader, takes no lock and does not call `dispatch`. A failure is refused with `scope_mismatch`.

## Creating identities

Stage 1's build delivers one operation of `dispatch` ahead of the rest of the package, because the Pool at stage 2 and the Spawning Pool at stage 3 need a real home and real identities before stage 4 builds provisioning in full.

`hive-fleet provision`, body version 1.0, creates identity records in the one home it is pointed at and does nothing else: no charter, no parent channel, no session and no write to any other home. Its request body follows [`ops/hive-fleet.provision.request.v1`](schemas/ops/hive-fleet.provision.request.v1.schema.json) and its response body [`ops/hive-fleet.provision.response.v1`](schemas/ops/hive-fleet.provision.response.v1.schema.json). Every other operation of `hive-fleet` is declared `unsupported` until stage 4.

| Request names | The command | Idempotent on |
| --- | --- | --- |
| A home with `role` `hive` | Creates the directory's `home.json`, adopting the request's `home_id` as the home's identity. | The directory: an existing `home.json` with the same `home_id` and role is returned unchanged. |
| A home with `role` `fleet`, and a workspace | Creates `home.json` and the workspace record, each naming the other, with the parent's `home_id` as given. | The directory and the workspace `slug`. |
| Projects | Creates each project record under the home's workspace. | The project `slug` within the workspace. |
| Clones | Resolves the physical Git common directory and top level of the path given, reads the object format and the named remote, and creates the clone record under its project. | The physical common directory: registering the same one again returns the same clone. |

- Provisioning runs before the scope it creates exists. Its request therefore always carries `scope_level` `hive`, names the workspace and projects by `slug` in the body, and proposes the home's identity in its own `home_id` field: every request names a home, and a new home has no identity until this request gives it one. The caller mints that `home_id`; the command mints every `workspace_id`, `project_id` and `clone_id`.
- A directory that already holds a `home.json` with a different `home_id` is refused with `home_mismatch`. A caller adding a project to an existing home reads the home's identity first and sends it.
- The response returns every record the request named, each marked `created` true or false, so a repeated request reads exactly as the first did apart from those marks.
- The response lists in `unknown_entries`, sorted, the entries at the home's top level that this specification does not name, which the command leaves alone as [records.md](records.md#the-home) says; the member is absent where there are none. It lists what the home holds when the request runs, and a replay returns the list its response recorded.
- A name or repository given again for an identity that already exists (the name of an existing home or workspace, or the name or repository of an existing project) is not a change of a fixed attribute: the existing record is returned unchanged, marked `created` false, and nothing is updated.
- A request that would change a fixed attribute of an existing identity (a home's role, a Fleet home's parent home, a workspace's `slug`, a clone's common directory under a different project) is a `conflict` with code `already_exists`, and nothing is written.
- All records of one request are written in one transaction, and each record created is an entry of its intent that states the record is expected to be absent, so a crash before any of them reached disk rolls forward like any other. There is one receipt per record created, so a request that creates two projects writes two `dispatch.project.registered` receipts, each with its own `subject`: `dispatch.home.created`, `dispatch.workspace.registered`, `dispatch.project.registered`, `dispatch.clone.registered`. Their bodies are empty; the receipt's `subject` names the record.
- The operation runs Git, to read a clone. It writes nothing into a clone or its Git directory.
- A request that names a remote the clone does not have is refused whole as `unavailable` with code `source_unreadable`, and nothing is written; a request that names no remote registers the clone with origin `none`. A path that is not a directory, lies outside a Git repository or is in a bare repository, which has no working tree to record, is refused the same way. A Git too old for the options the command uses is `unavailable` with code `dependency_unusable`.

Provisioning in full, including the registration of a Fleet in the Hive's own home, the charter, routing, recovery and the guarded move of a project between workspaces, is stage 4's and is not defined here.
