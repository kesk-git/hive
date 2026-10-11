# The Spawning Pool's names

The set of isolated worktrees that Hive owns, and the leases on them, is the **Spawning Pool**. A local Swarm or Lictor is given its working copy from it. The name replaces "worktree pool" everywhere in Hive.

## Every form of the name

| Where | Form |
| --- | --- |
| Prose, headings, the vocabulary table | the Spawning Pool: two words, both capitalized, always in full |
| Package | `spawning-pool` |
| Package directory | `packages/spawning-pool/` |
| Package declaration | `packages/spawning-pool/hive-package.json` |
| Executable and command | `hive-spawning-pool` |
| Operations | `hive-spawning-pool acquire`, `hive-spawning-pool observe`, `hive-spawning-pool release` |
| Test target | `test-spawning-pool` |
| Directory in a private home | `spawning-pool/` |
| Record kinds | `spawning-pool.<name>`; the lease record is `spawning-pool.lease` |
| Receipt kinds | `spawning-pool.acquire`, `spawning-pool.observe`, `spawning-pool.release` |
| Event kinds | `spawning-pool.<subject>.<fact>` |
| Error codes | `spawning-pool.<code>` |
| Identifier prefix of a lease | `lease_` |
| Go package name, where one is needed | `spawningpool` |
| Import record of the Treehouse adaptation | `imports/treehouse.json` |
| UI text | "Spawning Pool", as in "Spawning Pool lease" |

The three operations and the three receipts keep the shape they had under the old name: the command changes from `hive-worktrees` to `hive-spawning-pool`, the package from `worktrees` to `spawning-pool` and the target from `test-worktrees` to `test-spawning-pool`.

A single worktree is still a worktree, a lease is still a lease and a slot is still a slot. Only the pool of them is renamed.

Upstream names are not Hive's to rename. Treehouse's own worktree pool and the worktree pools of an existing firstmate installation keep those words, and a sentence about them says whose they are.

## Keeping it apart from the Pool

The Pool is the durable work queue. The Spawning Pool holds working copies and never a work item. Both names end in the same word, so the following rules apply to prose, identifiers and the UI alike.

1. **Pool alone always means the work queue.** The word "Pool", "the Pool" or `pool` with nothing before it never refers to the Spawning Pool.
2. **The Spawning Pool is never shortened.** Not "the Pool", not "the pool", not "the spawn pool", not "SP", and not `pool` or `sp` in an identifier. A sentence that has already said "the Spawning Pool" says it again rather than "the pool".
3. **No lowercase generic "pool" for either.** A sentence that needs a common noun says "the work queue" or "the set of worktrees". The one exception is an upstream's own name, as above.
4. **One sentence, one meaning.** Where both appear in a sentence, each is written in full and the Pool is named by what it holds if the sentence could otherwise be read two ways: "the Pool's task generations", never "Pool generations" beside "Spawning Pool".
5. **No pattern that matches both.** A build file, a glob or a filter never selects by the suffix `pool`. Targets, directories and commands are listed by their full names. `hive-pool` and `hive-spawning-pool` share no prefix beyond `hive-`, and neither is a prefix of the other.
6. **In the UI the Pool is a place and the Spawning Pool is a field.** **Pool** stays the name of the direct destination. "Spawning Pool" appears only as the label of a worktree lease, in the task inspector and in a preserved-copy finding. It is never a lane, a filter, a tab or a count inside the Pool view, and the Pool view never shows a control labelled with it.
7. **Nothing spawns from the Pool.** Text never says work is "spawned from the Pool" or "waiting in the Spawning Pool". Work is queued in the Pool and dispatched from it; a worker's copy is acquired from the Spawning Pool.

`test-contract` proves rule 5 for the build files and proves that no package declaration other than `packages/pool` uses the bare prefix `pool`.
