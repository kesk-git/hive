# Record format

This file fixes how Hive writes anything durable: the encoding, the schema notation, versioning, the layout of a private home, atomic replacement, the event journal and locks. Every package follows it through the contract library, which is its single implementation.

Owner: the contract library. Proved by `test-contract`.

## Encoding

- Every record is one **JSON** document in **UTF-8**, with no byte-order mark, ending in a single line feed.
- A record is one file. A journal is a file of records, one per line.
- **Text is kept exactly as written.** Nothing normalizes Unicode, trims, reflows or re-escapes a string that came from the Hive Mind, a worker, a vendor or a forge. A writer that cannot keep a string whole refuses the write.
- **Numbers are whole.** A record holds no fractional or exponent number and no number outside ±9,007,199,254,740,991. A value that is not whole is written as a string in a form its schema states.
- **Times** are RFC 3339 in UTC with millisecond precision and a literal `Z`: `2026-10-11T08:00:00.000Z`. A wall-clock time in a declared zone is two fields, the UTC instant and the zone's name.
- **Digests** are SHA-256, written `sha256:` and 64 lowercase hexadecimal digits. The digest of a file is the digest of its bytes. The digest of a record is the digest of its canonical form. A digest that binds a part of a file, which a later write may extend, says which part and is defined where it is used, as the [events digest](review.md#the-events-digest) of a review round is.
- **Canonical form** is RFC 8785. Because numbers are whole, its number rule reduces to writing integers in decimal.
- **Git revisions** are full object names, 40 or 64 hexadecimal digits, never abbreviated.
- **Paths inside a record** are relative to a base the schema names, use forward slashes and contain no empty, `.` or `..` segment. An absolute path appears only where a schema asks for a physical location, and such a record never leaves the home that holds it.
- File names are lowercase. A record's file name is its identifier with `.json`.

## Schema notation

Every record has a schema in **JSON Schema, draft 2020-12**, restricted to a profile that the contract library's own validator implements in full:

`$schema`, `$id`, `$ref`, `$defs`, `title`, `description`, `type`, `enum`, `const`, `properties`, `required`, `additionalProperties`, `items`, `minItems`, `maxItems`, `minLength`, `maxLength`, `pattern`, `minimum`, `maximum`, `allOf`, `anyOf`, `oneOf`, `not`, `if`, `then`, `else`, and the boolean schemas `true` and `false`.

- A schema that uses any other keyword fails `test-contract`. In particular there is no `format` assertion, no `patternProperties`, no `unevaluatedProperties` and no dynamic reference.
- `additionalProperties` is only ever a boolean.
- A `$ref` names another schema by its `$id`, with a JSON pointer where it selects a part: `urn:hive:schema:common:v1#/$defs/home_id`. There is no relative reference and no network fetch; every `$id` resolves to a file under `spec/schemas/`.
- A `pattern` is written in the syntax common to RE2 and ECMAScript, so the same schema gives the same answer in Go and in a browser.
- **Every object is closed.** Each object schema lists its properties and sets `additionalProperties` to `false`. The exceptions are the designated open members: `body` in an envelope, `detail` in an error and `unknown` in a record. A designated open member is declared as a bare object.

## Versioning

Every record carries `schema`, the name of its schema (`hive.request`, `pool.item`), and `schema_version`, written `MAJOR.MINOR`.

- A schema file accepts one major version and lists the minor versions it accepts.
- A **minor** version adds what an older record does not need: every record valid under 1.0 is valid under 1.1 unchanged.
- A **major** version is anything else. It has its own schema file and a named migration from the version before.
- A reader given a record whose version is newer than it knows, major or minor, refuses it with `unsupported_version` and never rewrites it. Hive's executables are built and delivered together, so this arises only when an older build meets a newer home, and refusing is the safe answer.
- A reader given an older major version reads it through its migration. A migration is explicit, versioned and tested, and it changes no word of the original: a field it does not recognize is carried into the record's `unknown` member unchanged, and every later writer preserves `unknown` as it found it.
- A migration that rewrites records in place does so under the store's lock, one transaction per record, and can be interrupted and resumed.

## The home

A private home is a directory outside every project repository, selected explicitly for every command. Its top level is fixed:

| Path | Owner | Holds |
| --- | --- | --- |
| `home.json` | `dispatch` | The home's [identity record](identities.md). |
| `journal/` | shared, through the contract library | The home's [event journal](events.md). |
| `locks/` | shared, through the contract library | Transaction lock files. |
| `pool/` | `pool` | The Pool's store, including what `word` persists through it. |
| `spawning-pool/` | `spawning-pool` | Leases and their receipts. |
| `dispatch/` | `dispatch` | The identity registry, execution records, scheduled jobs, sessions, policy. |
| `shadow/` | `shadow` | Observations, cursors and wakes. |
| `synapse/` | `synapse` | The run ledger and evidence. |
| `ui/` | `ui` | The UI's own state. |
| `reviews/` | `ui` | [Review directories](review.md), at the path the design fixes. |

- A home directory is created with access for its owner only, and so is every file and directory in it.
- A package writes only its own paths and reads only its own paths, with the three exceptions of [packages.md](packages.md#what-ownership-means): the journal and the locks, the identity records and the files of a review directory, each read through the contract library's validated reader and written only by its owner. Nothing else at the top level of a home is created by Hive, and a command that finds an entry it does not know leaves it alone and says so in its response.
- Inside its own directory every package uses the same five places, and defines the rest itself:

| Path under `<package>/` | Holds |
| --- | --- |
| `records/<kind>/<id>.json` | Replaceable records of one kind, the kind written without its package prefix. |
| `receipts/<receipt_id>.json` | The [receipts](receipts.md) the package wrote. Written once. |
| `requests/<request_id>.json` | The recorded outcome of each mutating request, for [replay](commands.md#replay). |
| `tx/<request_id>.json` | The intent of a transaction in progress. |
| `tmp/<request_id>.tmp` | The temporary file of a write in progress, for a record anywhere under the package's directory and for `home.json` alike. |

The identity registry is the one store this specification places beyond that: `dispatch/registry/`, as [identities.md](identities.md) lists.

## Writing one record

A record is replaced, never edited in place.

1. Write the new content to a temporary file in the package's `tmp/` directory, named for the request that writes it.
2. Flush the file to stable storage.
3. Rename it over the record's name. The home is one file system, so the rename is atomic wherever the record sits in it.
4. Flush both directories the rename touched.

A reader therefore sees the whole old record or the whole new one. A temporary file left by a crash is removed by the next writer that takes the store's lock, which empties `tmp/` and removes nothing else; it is never read as a record.

Each replacement raises `record_revision` by one and sets `updated_at`. A mutation names the revision it expects for every record it depends on; a different revision on disk is a `conflict` with code `revision_conflict` and nothing is written.

A record that fails its schema, or cannot be read, is never repaired by guessing. The mutation that needs it stops with `record_malformed`, and a read of it reports the same. A cached or displayed copy never stands in for it.

## Writing several records

A mutation that changes more than one record of a store is one transaction.

1. Take the store's lock.
2. Check every expected revision, and that every record to be created is absent.
3. Write the **intent** to `tx/<request_id>.json`, a record of schema [`hive.tx-intent`](schemas/tx-intent.v1.schema.json): every record to be written, with its expected revision and the digest of its new content, and every receipt and event to be written. The entry of a record being created has no revision to expect and instead states that the record is expected to be absent. Flush it.
4. Write the records, then the receipts, then append the events.
5. Write the outcome to `requests/<request_id>.json`.
6. Remove the intent and release the lock.

Recovery **rolls forward**. The next command to take the store's lock that finds an intent completes it: a record already at the intended digest is left, a record at the expected revision is written, a record expected to be absent and still absent is written, and anything else stops the store with `record_malformed` for that transaction, naming the intent. An intent is never discarded to make recovery convenient. A receipt or event the intent names is written at most once, because each is keyed by an identifier the intent already holds.

A transaction lives in one store of one home. Work that spans two packages or two homes is a sequence of transactions joined by receipts, each replayable, with the order and the recovery stated by the operation that needs it.

## Locks

There are two kinds, and they are not the same thing.

**A transaction lock** serializes writers of one store for the length of one command. It is an advisory lock taken on `locks/<package>.lock`, or on `locks/journal.lock` for the journal. The operating system releases it when the process ends, so a crash leaves nothing to clean. A command waits a bounded time for it and then stops with `conflict` and code `lock_held`. A reader of a single record takes no lock; a reader that needs several records to agree takes the lock shared. The scope check of [identities.md](identities.md#scope) is the exception: it takes no lock, because each record it reads is replaced atomically and the parent links it relies on never change.

Lock order is fixed: a store's lock first, then the journal's. A command may hold its own store's lock while it calls a package it [declares](packages.md#calls-between-packages); since the declared graph has no cycle, neither do the locks.

**An ownership lock** says that a session, a task or a worktree belongs to someone for longer than one command: the home lock a session holds, a lease on a worktree, the task lock of an Assimilation. It is a record of the package that owns it, with the holder's identity and generation, and it outlives the process that wrote it. It is defined with its package, never here, and is never implemented as a file lock.

## Size

A record has no fixed size limit of its own. Limits are stated where a record crosses a boundary, in the [command contract](commands.md#size), and a record too large to cross fails whole with a named error. A writer under pressure refuses; it never truncates, summarizes or drops part of a record.
