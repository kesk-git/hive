# Cloud status: the level-names residue

## Checks

- `make check` printed `go vet ./...` and nothing else, and exited 0. The formatter in check mode listed no file.
- `make test-contract` printed `go test ./contract/... ./spec/...` and then `ok` for each of the eight contract packages (`canon`, `cli`, `clock`, `envelope`, `ids`, `journal`, `schema`, `store`) and `[no test files]` for `spec`, and exited 0.

The toolchain the module requires was fetched by the cloud machine, so both commands ran here.

## Point 1: the Wave is the fourth level

- `docs/design.md`, vocabulary row for Wave: replaced "A grouping of Targets on a project's roadmap, for the roadmap's order. A Wave is never a level with an owner or a state." with "The fourth level of the work hierarchy: an ordered group of Targets with an outcome on a project's roadmap. It is recorded as grouping data on each of its Targets, its slug, title and ordinal, and has no worker, no Pool item of its own, no owner and no state of its own."
- `docs/design.md`, hierarchy paragraph, first sentence: "The work hierarchy is the Hive, a Fleet, a project, a Target, a Prey, with Hunts under a Prey where it needs them." became "The work hierarchy is the Hive, a Fleet, a project, then a Wave, a Target, a Prey and a Hunt, with Hunts under a Prey where it needs them."
- `docs/design.md`, hierarchy paragraph, the sentence on what a Fleet supervises: "a project's roadmap holds its Targets in Waves;" became "a project's roadmap holds its Targets in Waves, and a Wave groups Targets, in order, under one outcome;".
- `docs/design.md`, hierarchy paragraph: "Nothing above a Hunt has a worker." became "A Hunt is the only level with a worker."
- `docs/design.md`, mapping table, row for Target: "its Wave is a field on it, never a level" became "it carries its Wave, the fourth level, as grouping data, the Wave's slug, title and ordinal".
- `docs/design.md`, mapping table: added a row for Wave between Project and Target, because the paragraph says each level has its record. Record: "No record of its own: its slug, title and ordinal are grouping data on the Pool item of each of its Targets". Shown as: "The roadmap, which orders its Targets by their Wave". The row states only what point 1 states and what the design already says of the roadmap.
- `spec/pool.md`, the `wave` bullet under Levels: "A Wave is a grouping for the roadmap's order, never a level and never an owner." became "A Wave is the fourth level of the design's work hierarchy, an ordered group of Targets with an outcome on the project's roadmap; it is recorded as this grouping data on each of its Targets and has no worker, no Pool item of its own, no owner and no state of its own."
- `spec/schemas/records/pool.item.v1.schema.json`, description of the `wave` member: "A grouping for the roadmap's order, never a level and never an owner." became "The Wave is the fourth level of the work hierarchy, an ordered group of Targets with an outcome on the roadmap, recorded as this grouping data on each of its Targets: it has no worker, no Pool item of its own, no owner and no state of its own." The schema and the prose must say the same thing.

Kept as they were, because they count Pool items and a Wave has no Pool item of its own: "Pool items at three levels" in the design's record-model table, "the three levels of a Pool item" in the design's stage 2 entry, and "one unit of work at one of three levels" in the Pool contract's Items section.

## Point 2: a schedule belongs to a Hunt or to a single-Hunt Prey

- `spec/schemas/records/pool.item.v1.schema.json`: the variant for a Target now states `schedule` as a member a Target never carries, beside `deliverable`, `placement_requested` and `delivery`. The schema cannot state the variant for a Prey that holds Hunts, since that depends on other items, so the `schedule` member's description now says: "Carried by a Hunt or by a Prey with no Hunt beneath it, never by a Target, which this schema states, nor by a Prey that has a Hunt beneath it, which spec/pool.md, Levels, states."
- `spec/schemas/ops/hive-pool.add.request.v1.schema.json`: the variant for a Target now refuses `schedule` the same way, the `schedule` member's description says "Absent on a Target, which carries no schedule.", and the `deps` description's list of refusals gains "or a first Hunt attached to a Prey that carries a schedule".
- `spec/schemas/ops/hive-pool.update.request.v1.schema.json`: the request description and the `level` description now say the conversion to a Target is allowed only while the item has no schedule, the `level` description adds "a schedule being cleared first by clear_schedule", the `add_deps` description's list of refusals gains "or a first Hunt attached to a Prey that carries a schedule", the `schedule` member gains the description "Refused with pool.hierarchy on a Target and on a Prey that has a Hunt beneath it: a schedule belongs to a Hunt or to a Prey that is a single Hunt.", and `clear_schedule` gains the description "True removes the item's schedule, which the owner does before converting the item to a Target or attaching a first Hunt beneath it."
- `spec/pool.md`, Levels, the `level` bullet: "allowed only while it has no generation, no Imperative and no `parent` edge and is `queued`, and is refused with `pool.hierarchy` otherwise;" became "allowed only while it has no generation, no Imperative, no `parent` edge and no `schedule` and is `queued`, and is refused with `pool.hierarchy` otherwise, a schedule the owner clears first by an ordinary update;".
- `spec/pool.md`, Levels: added a bullet. "A schedule belongs to a Hunt or to a Prey that is a single Hunt, never to a Target or to a Prey that holds Hunts, as the design says. `add` refuses a Target with a `schedule`, which its request schema states; `update` refuses a patch that sets one on a Target or on a Prey that has a Hunt beneath it with `pool.hierarchy`; and converting an item to a Target, and attaching a first Hunt beneath a Prey, are refused with `pool.hierarchy` while that item carries a schedule. The owner clears it first by an ordinary update, with `clear_schedule`." The clause on `update` setting a schedule on a Target or on a Prey with Hunts is the one sentence that goes past the letter of point 2: without it the rule had no code for that patch, and `pool.hierarchy` is the code point 2 gives the other two refusals.
- `spec/pool.md`, Levels, the parent rule bullet: the list of refusals gains "and a first Hunt attached to a Prey that carries a schedule, which the owner first clears by an ordinary update".
- `spec/pool.md`, the `schedule` row of What an item carries: added "Carried by a Hunt or by a Prey with no Hunt beneath it; a Target and a Prey that holds Hunts have none, by the schedule rule."
- `spec/pool.md`, error codes, the `pool.hierarchy` row: "or sets `target` on an item that has a generation, an Imperative or a `parent` edge or is not queued." became "or sets `target` on an item that has a generation, an Imperative, a `parent` edge or a schedule or is not queued; or, by the schedule rule, `update` sets a schedule on a Target or on a Prey that has a Hunt beneath it, or a first Hunt is attached to a Prey that carries a schedule."
- `spec/pool.md`, What `test-pool` proves: added "a Target refused by `add` with a schedule, and a scheduled Prey refused its first Hunt with `pool.hierarchy` until its schedule is cleared, then accepting it".
- `spec/test-targets.md`, the `test-pool` row: added "a Target that cannot be added with a schedule, and a scheduled Prey that cannot receive a first Hunt until its schedule is cleared".

## Point 3: the import plan speaks in the fleet's words

- `spec/import-plan-tasks-axi.md`, "Who writes the record": "Work item 2, lane a, of the stage's build writes" became "Story 2, lane a, of the stage's build writes".
- `spec/import-plan-tasks-axi.md`, the paragraph after the Portions table: "re-expressed against that lock in work item 3 as native tests" became "re-expressed against that lock in story 3 as native tests".
- `spec/import-plan-tasks-axi.md`, "The audit still owed": "Before the first translated file is committed, work item 2, lane a:" became "Before the first translated file is committed, story 2, lane a:".

## Point 4: the sweep

Swept every file under `docs/`, `spec/`, `packages/` and `README.md` for job, jobs, work item, work items, milestone, lane, epic and story as the name of a level.

- `spec/schemas/records/pool.rule.v1.schema.json`, description of `subject`: the example key "lane-limit" became "hunt-limit", since the design calls the same rule a Hunt limit and the key names a level of Hive's.

Found and kept, as the task allows: the GitHub Actions `jobs` endpoint in the design's Claude GitHub section and its observation table; "lane" for the fleet's level in the spec README's version sentence ("no lane of stage 2 has started") and in the import plan ("a lane that splits a file", "If a lane finds a case", "the lane says so"), which describe building Hive. No package README, package declaration or the root README named a level in the old words.

## The contract version

- `spec/README.md`: the version moved from 1.6 to 1.7, with the clause appended to the version paragraph: it names the Wave as the fourth level and states the schedule rule in the Pool contract, is a minor change that adds no member, no operation and no code, asks nothing of built code so the library's contract version constant does not move, and keeps every record and declaration valid under 1.6 valid under 1.7 except a Target that carried a schedule, which the design never allowed and no build has written. The constant in the contract library stays at 1.5, as the task fixes.

No point needed a file outside the list, and nothing under `contract/` changed.

done claude/hive-level-names-k164d9
