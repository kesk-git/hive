# Hive design

Design baseline: 2026-10-10. This is an implementation contract, not a claim that the components below already exist.

Product scope follows the Hive Mind's 2026-10-10 brainstorm rulings and subsequent instruction to repair Codex Void support. The dated Claude and Codex measurements establish the capability evidence; the original firstmate supplies the inherited operational contracts.

Hive specializes firstmate for Claude Code and Codex. It retains their existing local capabilities and adds execution in the Void. The Hive and its Hive Fleets ordinarily run as Claude Code sessions on the Mac; local Codex supervision remains an inherited capability. Swarms and Lictors can run locally or in the Void. Synapse validates their work, and Assimilation stays on the Mac.

The latest Mind's word takes precedence over the earlier minimal-Mac sketch. Local Swarms, worktrees, terminal runtimes, the local review pipeline, supervision hooks, and a local test machine remain available. Tooling specific to other agent harnesses is removed; shared tools needed by Claude and Codex remain. This design does not alter the existing firstmate installation.

The subsequent ruling requires making Codex work in the Void. Its intended path is submit-and-collect for both Swarms and Astra reviews. The measured gaps are open issues under active repair, not a decision to abandon that path. An implementation must report those gaps honestly until repaired; local Codex remains usable throughout.

## Vocabulary and placement

| Name | Meaning and responsibility |
| --- | --- |
| Hive | The product. **The Hive** is its primary supervisor and the Hive Mind's point of contact. |
| Hive Mind | The operator; supplies intent, scope, decisions, and Assimilation authority. |
| Hive Fleet | A persistent project supervisor, named after its project, with an isolated private home and a charter. |
| Swarm | A worker completing an authorized change through its chosen delivery path. |
| Lictor | An investigator producing evidence, a diagnosis, or a design report; research alone does not authorize implementation. |
| Imperative | The complete dispatch instructions for one assignment. |
| Pool | The durable work queue owned by the Hive or a Hive Fleet. |
| Mind's word | An operator decision, recorded against a held Pool item. |
| Shadow in the Warp | Supervision; called **the Shadow** below. |
| Synapse | The review and validation gate. |
| Harvest | A pull request carrying a proposed change. |
| Assimilation | Authorized merging of a Harvest or landing of a local-only change. |
| Void | Cloud execution. Model names remain their real names. |
| Genestealer | The research role developing Hive; it does not create another level of runtime authority. |

```mermaid
flowchart TD
    Mind[Hive Mind] --> Hive[The Hive · Mac]
    Hive --> Fleet[Hive Fleet · Mac]
    Hive <--> Private[Private homes · Pool, Imperatives, decisions, journal]
    Fleet <--> Private
    Fleet --> Local[Local Claude or Codex · isolated worktree]
    Fleet --> Claude[Claude in the Void · launch and steer]
    Fleet --> Codex[Codex in the Void · submit and collect · repair required]
    Local --> Harvest[Harvest · GitHub]
    Claude --> Harvest
    Codex --> Collection[Mac collection · verified result]
    Collection --> Harvest
    Harvest --> Synapse[Synapse · Actions, Claude review, Astra review]
    Synapse --> Assimilation[Assimilation · authorized Mac operation]
    Shadow[Shadow · Mac polling] --> Private
    Shadow --> Local
    Shadow --> Claude
    Shadow --> Codex
    Shadow --> Harvest
    Shadow --> Synapse
```

GitHub delivers events to its Actions and vendor integrations. It does not deliver webhooks into the Mac supervisor session. The Shadow therefore polls and wakes the appropriate supervisor. No always-running supervisor or waiting loop is placed in a Void session.

## What Hive keeps from firstmate

The inheritance baseline is the original [firstmate operator contract](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/AGENTS.md), [architecture](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/architecture.md), and [configuration](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/configuration.md). This snapshot makes the comparison reproducible; implementation must review later upstream changes before porting them.

### Operator contract and hard rules

1. The Hive coordinates work; Swarms change projects. Hive Fleets apply the same separation within their charters. Guarded initialization, synchronization, updates, and authorized Assimilation keep their narrow upstream exceptions. A concrete instruction from the Hive Mind can authorize a specific project operation, but cannot be broadened into standing authority.
2. Assimilation requires the Hive Mind's explicit word, or an explicitly granted project posture permitting autonomous Assimilation of green, in-scope work. A worker, reviewer, passing check, or completed Void session cannot grant that authority.
3. Preserve unlanded work. A dirty worktree, uncertain ownership, an uncollected result, or an unproved remote copy blocks cleanup. A Lictor's scratch copy can be retired only after its report is safely collected and its decision inventory recorded.
4. Swarms and Lictors report to their dispatching supervisor. Hive Fleets relay outcomes through their registered parent channel. Direct intervention by the Hive Mind remains authoritative and is reconciled into the durable record.
5. Report outcomes faithfully: distinguish an accepted launch, observed activity, published change, passing Synapse, and completed Assimilation. Missing evidence is unknown, not success.

The Hive presents concrete outcomes and the decisions needed from the Hive Mind, including a Harvest's full URL when relevant. An idle Fleet does not invent audits or improvement work. Research is dispatched separately only when requested or when uncertainty could change whether or what to build; bounded research can remain within an already authorized implementation task.

### Retained capabilities

| Capability | Hive contract |
| --- | --- |
| Local Claude and Codex | Preserve verified launch, steering, interruption, exit, resume, recovery, model/effort configuration, permission modes, and tool restrictions. Harness choice and runtime placement are separate axes. |
| Isolated worktrees and terminal runtimes | Preserve pooled or runtime-owned disposable copies and visible sessions, including existing verified terminal backends. Neither a Void addition nor a resource-saving preference removes a local runtime. |
| Local Synapse | Retain no-mistakes and its ownership of review, fixes, tests, documentation, lint, publication, and CI when selected. A worker drives its gates; it does not edit around an active run. |
| Delivery choices | Preserve validated delivery, direct Harvest delivery, and local-only delivery. Their upstream command values are `no-mistakes`, `direct-PR`, and `local-only`. Merge authority is an independent choice. |
| Local test machines | Preserve a Fleet's required native machine or guest and its capacity controls. macOS execution requires a macOS runner or local macOS machine; a Linux Void session cannot establish macOS behavior. |
| Persistent Fleets | Preserve charters, isolated homes, scope routing, correlated parent replies, guarded provisioning and recovery. Existing remote-home capability remains optional; the chosen deployment starts with supervisors on the Mac. |
| Supervision and continuity | Preserve durable inboxes, status events, wake acknowledgments, current-state reconciliation, turn-end guards, away/quiet operation, stow, and guarded restart/update behavior for Claude and Codex. |
| Project and operational tooling | Keep project registration, branch/base selection, forge bindings, Pool backends, usage-aware admission, optional operator communication integrations, and guarded synchronization where they support Claude/Codex work. Optional features stay opt-in; this GitHub Void design does not silently remove other local delivery capabilities. |

Local-only work stays under the Hive's local landing authority. A direct Harvest does not acquire an additional independent review pipeline merely because it uses Claude or Codex. The complete GitHub Synapse contract below is the validated Void delivery path; choosing the faster path or local-only path must be explicit in the Imperative. Existing repository-required checks still apply.

## Components and private state

Hive remains a set of instructions, skills, and small local scripts around existing tools. It needs no new hosted control service. The `hive-*` command names below are **proposed script interfaces**, not installed commands or vendor flags. Their upstream owners are named so an implementation can port behavior without reproducing it in several places.

Each supervisor has an explicitly selected private home outside the public project repository. The following are logical stores within that home, not machine-specific locations. Use atomic record replacement, append-only event journals, and home/task locks. Missing or malformed authoritative records stop the affected mutation; a cached display cannot authorize it.

| Component and command interface | State and location | Failure behavior |
| --- | --- | --- |
| Session owner: `hive-session-start` | Private home lock, startup digest, schema version, charter, inherited preferences, registered direct reports. Ports `fm-session-start.sh`. | Lock refusal leaves the session read-only. Deferred network checks remain unverified until their results arrive. |
| Pool: `hive-pool add/read/update` | Private durable work items: task identity, project, scope, dependencies, delivery path, dispatch class, artifact links, holds and dates. Ports the home-scoped `fm-tasks-axi.sh` interface. | Failed dispatch/finish transitions cannot be reported as successful. Capacity deferral leaves the item queued. |
| Mind's word: `hive-word hold/answer/complete/verify` | Private held task, reason, origin, decision provenance, answer, and release/completion state. Ports `fm-captain-hold.sh`. | An unanswered or unreadable hold prevents dependent dispatch, Assimilation, and closing that question. |
| Imperative author: `hive-imperative create/revise/render` | Private canonical text, revision and digest, plus exactly what was delivered for each generation. Ports `fm-brief.sh` and its intent/completion helpers. | Empty sections, unresolved placeholders, or incompatible delivery settings refuse dispatch. |
| Dispatcher: `hive-dispatch` | Private configuration and task execution record, launch intent and receipt, runtime ownership, admission/capacity reservation. Ports `fm-spawn.sh` and verified Claude/Codex adapters. | Preserve uncertain launches for reconciliation; never blindly create a second session after a timeout. |
| Steering and control: `hive-send`, `hive-control` | Private message outbox/inbox, acknowledgments, revisions, lifecycle operation receipts. Ports `fm-send.sh` and `fm-control.sh`. | Delivery acceptance is distinct from acknowledgment. Unsupported lifecycle actions refuse; a chat message is not an interrupt command. |
| Shadow: `hive-shadow`, `hive-state` | Private observations, poll cursors, next-due times, wake queue, handled sequences, owner generation and liveness. Ports `fm-watch.sh`, `fm-crew-state.sh`, and wake helpers. | Unknown remote state stays unknown; failures neither erase evidence nor trigger destructive recovery. |
| Synapse: `hive-synapse start/collect/respond` | Private run ledger, review packets, finding decisions, bound head/base SHAs, fix rounds and executor; public sanitized checks/reviews on GitHub. | Incomplete or stale evidence keeps the gate pending or failed. Only the selected executor owns fixes. |
| Assimilation: `hive-assimilate`, `hive-assimilate-local` | Private authority receipt, verified Harvest identity/head, required evidence, merge result and completion receipt. Ports `fm-pr-merge.sh` and `fm-merge-local.sh`. | Refuse on missing authority or unmet guards; verify actual forge state after submission. |
| Continuity: `hive-stow`, `hive-recover`, `hive-sync`, `hive-retire` | Private curated memory, recovery checkpoint, pending transitions and cleanup receipts; recorded worktree ownership. Ports stow, sync, control and teardown contracts. | Preserve records and copies on failed collection, synchronization or cleanup; never reset/discard to make recovery convenient. |

The minimum execution record carries `task_id`, `home_id`, `generation`, `kind` (Swarm or Lictor), project/repository identity, base branch and source SHA, expected output branch, Imperative revision/digest, class, concrete harness/model/effort, placement, delivery mode, Synapse executor, authority reference, and timestamps. A local record adds the owned worktree and runtime endpoint; a Void record adds vendor, environment if applicable, session/task ID and URL, observed output branch, and collection receipt. Record requested and observed model separately; absence of observed model evidence cannot be filled from the request.

Execution, delivery, and decision state are separate. Execution can be planned, launching, running, stopped, failed, or unknown. Delivery can be unpublished, collected, Harvest-open, in-Synapse, ready, assimilated, or retired. Holds can coexist with any of those. An event saying `done` does not imply Assimilation, and a later `working` event does not close a keyed decision.

### The Pool and the Mind's word

Pool items describe work, never persistent agents. A Fleet's routed work belongs in its Pool; the parent keeps a handoff receipt rather than a second competing task. Dispatch reserves capacity and transitions a queued item together with recording the launch attempt. If submission becomes uncertain, retain the reservation until reconciliation proves what happened.

A decision is an ordinary Pool item held for the Hive Mind. Prefer holding the work it gates; create a separate question only when it has its own scope. Record the question, consequence, options, recommendation, and any explicit revisit date privately. `hive-word answer` records the actual answer and either releases the work or completes the question. Replays of the same answer are idempotent; conflicting answers require reconciliation. A deferral date is not approval.

Before completing a Lictor report, the supervisor inventories every remaining operator decision and records the held items, or explicitly records that there are none. Finishing the report and closing those decisions are distinct actions. Cleanup must retain an open question even when its source report or Harvest has been delivered. This ports the [held-task lifecycle](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/captain-hold-lifecycle.md).

## Imperatives are given at dispatch

The Hive owns the canonical Imperative in its private home, or delegates custody to the dispatching Fleet's private home with a parent reference. Nothing requires committing it to a public repository. The source branch must be pushed for a Void launch; the Imperative is supplied as the launch prompt. Provider receipt of that prompt does not authorize publishing it in a Harvest, comment, log, or code file.

Preserve four parts from firstmate's [dispatch scaffold](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-brief.sh):

1. **Task:** the Hive Mind's intent, separately from the Fleet's implementation specification. Include the substance of referenced evidence, scope, exclusions, acceptance criteria, and the assigned dispatch class. Do not turn the Fleet's implementation choices into purported operator intent.
2. **Setup:** repository, immutable source SHA and base, output branch rule, role, harness/placement, dependencies, available tools, allowed access, and isolation assertions. Local execution verifies physical working directory and repository top level before branching. Void execution verifies repository/base/source identity before editing.
3. **Rules:** hard boundaries, allowed changes and artifacts, complete validation commands, delivery and decision channels, authorized repair scope, finite retry limits, and stop conditions. Name who can answer a blocker; do not assume private Mac files or local tools exist in the Void.
4. **Definition of done:** the exact deliverable and evidence. A validated Harvest requires the agreed Synapse results at its current head; a direct Harvest requires publication and its stated checks; local-only work requires a clean ready branch; a Lictor requires a collected report and decision inventory. Define the worker's stopping point separately from later Assimilation.

The rendered prompt starts with the worker role boundary, so project instructions cannot turn a Swarm into a supervisor. Before launch, validate that the text fits the selected transport without truncation and that required files, network access, result collection, tool installation, and credentials are already available. Secrets and unrelated private history never belong in the prompt. If those requirements cannot be met, hold dispatch or choose an explicitly permitted local route.

The Claude adapter passes the rendered text to `claude --cloud "<imperative-text>"`; the target Codex adapter passes it to `codex cloud exec --env <environment-id> --branch <branch> --attempts 1 "<imperative-text>"` once the supported route is repaired. A routine-based dispatch may carry the text in its supported trigger payload. These are argument shapes, not instructions to interpolate arbitrary text into shell code. Launchers pass an argument vector or supported input channel and keep prompt-bearing transcripts private.

A Swarm never waits in the Void for its next assignment, a missing dependency, review approval, or Assimilation. It completes its Imperative, pushes its authorized result, records a receipt, and stops. A Lictor likewise returns its report and stops. Later fixes are bounded new work, possibly resumed through a vendor event. An unexpected decision produces a blocker receipt and an exit, not an idle polling session.

Steering changes the canonical Imperative first. The Fleet saves a revision, assigns a message ID, and records transport acceptance separately from worker acknowledgment. Claude can accept a steer by session ID while active. A Codex task is submit-and-collect; no cloud steer command was established. Changed Codex work becomes a successor Imperative after reconciliation of the existing task, rather than a guessed follow-up API. A late steer cannot silently invalidate a finished deliverable without creating a new delivery obligation.

The measured Claude exercise used a committed brief. Prompt delivery is supported by the CLI, but the intended **prompt-only bootstrap and private canonical-copy contract still need an integration check**. The implementation must not copy that exercise's committed-brief technique into Hive by accident.

## Eight dispatch classes

The class numbers stay **0–7**. They classify the work, independently of whether its deliverable is a Swarm change or a Lictor report.

| Class | Scope |
| --- | --- |
| 0 — Option weighing and feasibility | Compare plausible approaches, establish feasibility, and recommend a direction before the Hive Mind chooses. Produce evidence and tradeoffs, not speculative implementation. |
| 1 — Architecture and specification | Define boundaries, behavior, interfaces, invariants, and a buildable specification. Carry consequential design reasoning into implementation where that reasoning still determines the work. |
| 2 — Adversarial review | Challenge a proposed design or change for correctness, missing requirements, regressions, and failure cases. Findings are a review deliverable; they do not confer permission to implement or Assimilate. |
| 3 — Partially specified build | Implement an agreed outcome whose behavior or scenarios are known but whose internal structure still needs substantial definition. Preserve the reasoning requirements of an architectural change rather than treating a written outline as a mechanical recipe. |
| 4 — Specified build | Implement a bounded feature or fix whose behavior, approach, and acceptance conditions are sufficiently settled. Remaining uncertainty is ordinary implementation detail, not an unresolved product or architecture choice. |
| 5 — Structured research | Gather and organize existing evidence: inventories, interface maps, source comparisons, reproducible captures, and evidence tables. Escalate to the relevant reasoning class if the work turns into feasibility, architecture, or diagnosis. |
| 6 — Diagnosis | Reproduce a failure, distinguish competing causes, and establish a supported explanation and repair direction. An investigation request alone does not authorize shipping that repair. |
| 7 — Mechanical work | Perform narrow, readily checked edits such as a specified rename, typo correction, or deterministic formatting change. Hidden behavioral decisions require reclassification. |

The private dispatch configuration maps each class to eligible Claude/Codex profiles and placement preferences. Each profile includes harness, real model name, effort, supported capabilities, and selection rationale. Separate entries configure the Hive, Fleet supervision, Synapse reviewers, validation steps, and fixers; those roles do not create extra dispatch classes.

The Fleet resolves the class and concrete profile at every intake. An explicit task instruction wins, followed by the configured matching rule, configured default, then the supported static default. Inspect actual model support and available usage evidence; record uncertainty rather than inventing precision. A malformed configuration, unsupported required capability, or unusable authentication stops dispatch. Never silently change vendor, model, permission mode, or placement to bypass a refusal. Where local fallback is preauthorized, record the fallback and its reason in the execution record.

## Runtime adapters: what the Void adds

Each adapter implements `probe`, `launch`, `observe`, `collect`, and a capability declaration for `steer`, `interrupt`, `resume`, and `stop`. These are Hive interfaces, not assumed vendor commands. Results distinguish accepted, running, finished, failed, unknown, and unsupported; they carry source evidence and the bound task generation. Completion always requires collection and delivery checks above the adapter.

### Local Claude and Codex

The local adapter ports firstmate's runtime/harness separation. `hive-dispatch` acquires an isolated worktree from its configured provider, records ownership, launches `claude` or `codex` with the resolved profile through the verified terminal backend, and supplies the private Imperative. It records the runtime endpoint, worktree, branch and session identity before accepting worker status.

`hive-send` writes a durable steering message and rings the recorded endpoint. `hive-control` performs verified lifecycle operations; relaunch preserves the same worktree and work. `hive-state` reconciles the current no-mistakes run, semantic harness activity, inbox acknowledgment and status journal. It does not infer a live run from a stale status tail. The local adapter retains existing native test-machine routing and capacity reservations.

No worker may administer a shared pipeline daemon or another task's worktree. A missing endpoint is not proof that work was lost or that a second endpoint is safe. A pipeline client timeout first prompts inspection and reattachment; a confirmed daemon failure is a supervisor-owned blocker. Preserve Codex compaction and Claude handover/recovery behavior through their respective harness adapters rather than imposing one vendor's lifecycle on the other.

### Claude in the Void

**Measured on 2026-10-10:** a launch from a pushed branch was accepted in about 14.5 seconds; a steer was accepted in about 1.3 seconds and its pushed acknowledgment became visible on the Mac within 43 seconds. The same session opened a Harvest through GitHub REST, Auto-fix repaired both a failed check and a review comment, all three native OS checks passed, and Assimilation occurred on the Mac. This establishes a small end-to-end delivery path, not desktop-platform parity or the complete Synapse design.

The implementation sequence is:

1. Persist the launch attempt, expected repository/branch/source and prompt digest. Run `claude --cloud "<imperative-text>"` with an interactive terminal attached: the measured launch refused piped stdout. Capture the returned session ID and URL privately without treating CLI exit as task completion.
2. Bind the session's actual output branch after validating its receipt against the launch record. Claude may choose a branch name different from the source branch; never adopt an arbitrary new branch solely because it appeared recently.
3. For steering, run `claude -p "<revision-text>" --cloud <session-id> --output-format json`. An `ok` response proves queue acceptance. Require a matching message acknowledgment or observed result before calling the steer applied.
4. Collect the pushed head and sanitized delivery receipt. Create the Harvest through the verified repository-scoped REST shape, `gh-axi api POST repos/<repository>/pulls --input <payload> --full`, after checking for an existing Harvest for that branch. The measured cloud proxy rejected GraphQL-dependent creation; its REST path worked.
5. Finish the assignment and stop. Native Auto-fix may wake a bounded repair when enabled; the Mac owns its registration, expiry and final unsubscribe confirmation.

The measurement does **not** establish a headless native session-status command. `observe` must use a supported session-state surface when one is available and otherwise return native state as unknown, while separately reading authenticated GitHub progress. Do not invent `claude cloud status`, scrape undocumented endpoints, or send model messages merely to ask whether a session is alive. Optional `claude --teleport <session-id>` is a recovery/inspection path; it is not Assimilation.

The remote image did not contain `gh-axi`, and redirected Actions logs were inaccessible through its proxy. Environment preparation must verify the needed tools and REST access before dispatch. For failures that require inaccessible logs, the Mac obtains and sanitizes the relevant output for a new repair Imperative. Committed repository instructions can travel with the source, but private Mac settings and installed plugins must not be assumed to travel. [Claude cloud documentation](https://code.claude.com/docs/en/claude-code-on-the-web) describes the supported launch and recovery surface.

### Codex in the Void: target contract and active repair

The design requires Codex Swarms and Astra reviews to be submitted from the Mac and collected headlessly. This is a first-class adapter alongside Claude, with an explicit readiness state. The implementation cannot mark it ready merely because the web product supports tasks.

**Measured on 2026-10-10 with stable CLI 0.162.1:** an environment was created and published in the web application, but the CLI environment picker exposed none and `codex cloud list --json` showed none of the web tasks. A complete-prompt `codex cloud exec` failed before creating a task with an environment-availability error. No Codex Void result, Harvest, automatic repair, or Astra review was produced by that measurement.

Version-matched source inspection found three independent problems:

| Open repair issue | Consequence and required evidence |
| --- | --- |
| Environment and task visibility | The CLI uses the older task/environment API family. A generation mismatch is a leading explanation; the exact backend/account cause was not proved. Demonstrate supported discovery of the intended environment and task under a verified matching context, then an accepted submission returning a stable task ID. Guessing another environment ID cannot bypass the measured empty inventory. |
| Model selection | The inspected cloud submission payload carries no model-selection field. A generic `-c model=...` setting is not evidence that Astra was selected. Demonstrate supported per-task model selection and independent result metadata confirming the requested model. |
| Final-answer collection | Headless `status`, `list`, `diff`, and `apply` do not expose the final natural-language answer. Add or integrate a supported headless result route that exports the complete review text, including an explicit no-findings verdict. An empty diff or a diff-statistics summary is not a review result. |

These findings are grounded in the versioned [cloud command implementation](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks/src/lib.rs), [environment resolution](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks/src/env_detect.rs), and [submission client](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks-client/src/http.rs). They describe the inspected release and measured context, not universal impossibility of Codex cloud.

The adapter's intended operations, using the legacy CLI shape where still supported, are:

| Operation | Command surface and state contract |
| --- | --- |
| Submit | `codex cloud exec --env <environment-id> --branch <branch> --attempts 1 "<imperative-text>"`; persist request and receipt. The repaired route must also honor the configured model and effort where supported. Do not invent a model flag for this command. |
| Observe | `codex cloud list --env <environment-id> --json --limit 20`, continuing with `--cursor` until the bound ID is found or pagination ends. Keep a complete response privately. `codex cloud status <task-id>` is diagnostic: in the inspected source it exits zero only for ready, so nonzero alone cannot distinguish pending from failure. |
| Collect code | `codex cloud diff <task-id>` and, when appropriate, `codex cloud apply <task-id>` into the task's isolated collection worktree. A local collection Swarm validates repository/base/head, selected attempt, patch application, changed scope and tests, then commits, pushes and opens a Harvest through the ordinary publication path; the Fleet only coordinates. These collection operations were not exercised successfully in the measurement. |
| Collect review | Retrieve the full final text through the repaired supported result surface, bind it to repository/base/head, actual model and task ID, and store it in the private Synapse ledger. Publish only sanitized findings and an attestation. Diff collection cannot substitute. |
| Change or stop | Advertise only supported, measured controls. No cloud steer-by-ID command or generic HTTP trigger was established; unsupported controls return an explicit refusal. Reconcile a task before submitting successor work. |

For a route that returns a diff instead of publishing a branch, the Void Swarm finishes by making its result durably collectable and stops; the local collection Swarm fulfills the push obligation. Record both executions under the same delivery task with separate generations and receipts. Verify the provider's result-retention behavior before enabling this route, and collect before its retention deadline. If the repaired adapter supports direct branch publication, collect that branch instead; both paths require the same source and result checks.

The inspected task mapping is pending/ready/applied/error; failed and cancelled map to error. Unknown upstream states mapped to pending in that release, so deadlines and source diagnostics remain necessary. Hive treats an absent task, truncated page, unreadable response, or unrecognized state as unknown. Ready means available for collection, not passing Synapse. [The CLI reference](https://developers.openai.com/codex/cli/reference/) documents the experimental command surface; it does not establish compatibility with the measured web environment.

Codex Void readiness requires a controlled replay proving discovery, prompt-only dispatch, source binding, required model selection, polling through terminal success and failure, complete result collection, and a Swarm change passing three-OS Synapse through Mac Assimilation. Astra review readiness additionally requires a review-only task with retrievable findings and an explicit no-findings case at a known head. Prove restart recovery and duplicate-submission prevention as well. Store versioned evidence privately and enable the specific verified capabilities in dispatch configuration; do not enable every capability from one successful code task.

Until those repairs are proved, Codex Void dispatch reports the concrete blocker. A Fleet can use local Codex Swarms and local Astra reviews under its configured authority while the repair proceeds. That is an operational fallback, not completion of the required Codex Void work. No unsupported scraping, hidden API, automatic account changes, or model substitution is part of the design.

## The Shadow in the Warp

The Shadow is a lightweight Mac process, one owner per private home. It performs bounded reads without model calls, persists observations, and wakes a supervisor only for actionable changes. Claude's existing Stop-hook supervision ownership and Codex's existing checkpoint protocol remain available. The Hive observes its Fleets' routed summaries; each Fleet observes its own children. No process sweeps another home's endpoint namespace.

The cadence below is a configurable starting contract. Local timings retain upstream defaults; the faster remote timings are proposed Hive defaults, not measured service guarantees.

| Source | Reads | Cadence and interpretation |
| --- | --- | --- |
| Local Swarms and Lictors | Task journal and inbox acknowledgment; owned endpoint's semantic busy/liveness result; attributed no-mistakes run via `no-mistakes axi status`; recorded worktree activity only for a suspected stall. | Local tick every 15 seconds. A status append wakes promptly; a quiet display alone does not prove a stopped worker. |
| Claude Void sessions | Bound session-state surface if supported; worker receipt sequence, branch head and Harvest evidence through `gh-axi api`. Native status remains unknown where no supported reader exists. | Every 60 seconds while launch, work or collection is pending; 300 seconds during a declared external wait. Accepted steering is not an acknowledgment. |
| Codex Void tasks | Paginated JSON task list, bound task ID/environment, structured state and collection receipt. | Every 60 seconds when the repaired adapter is enabled. With readiness blocked, no repeated submission attempts; only a scheduled capability recheck or new repair evidence changes readiness. |
| Harvest arrivals | `gh-axi api repos/<repository>/pulls` plus branch identity; paginate all relevant results and reconcile both open and recently closed Harvests. | Every 60 seconds while a dispatched task has no recorded Harvest; otherwise every 300 seconds for the registered repository. Correlate by recorded repository/branch/generation, not title alone. |
| Checks and reviews | Harvest head/base, `commits/<sha>/check-runs`, combined commit statuses, Actions run results, review records and unresolved threads through `gh-axi api`; supported Mac GraphQL where REST is insufficient. | Every 60 seconds during Synapse; 300 seconds while awaiting Assimilation. A changed head invalidates earlier evidence immediately. |
| Reconciliation backstop | Pool dependencies, open decisions, capacity, overdue polls, summaries and pending lifecycle operations. | Every 600 seconds; unchanged idle cycles back off to a maximum of 7,200 seconds. A new event resets the interval. |

Bound each network read to 30 seconds, add modest jitter, use conditional requests where supported, and honor rate-limit retry times. Repeated transient failures back off from 60 to 300 seconds and then at most 900 seconds. An authentication refusal raises one actionable blocker; network loss raises one degraded-observation event and preserves the last observation with its timestamp. Staleness is visible and never used as a current green result. One slow repository must not block other due polls.

For local work, retain the distinction between a working-but-quiet interval, a declared wait, and a real blocker. The inherited working-stall inspection threshold is 240 seconds; a continuously busy turn has a separate one-hour inspection bound. Neither authorizes automatic termination. A declared wait names its dependency and clearing condition and is rechecked by its stated time or within four hours, whichever is sooner. The Hive Mind's held decisions do not repeatedly wake an away session; supervisor-owned decisions still receive attention. A cloud session does not remain allocated just to satisfy this wait mechanism.

Every event is correlated with task generation, source, evidence identity and sequence. Write the observation and wake record durably before advancing its cursor. The supervisor drains wakes, reconciles current truth, performs idempotent actions, then acknowledges the exact handled sequence. Replaying after interruption may duplicate an alert but must not duplicate a launch, fix round or merge.

Worker status is evidence, not executable input. Validate schema, size, identity, allowed state transitions and the authenticated publisher; fetch full responses and reject truncation. After a Harvest exists, prefer sanitized status comments or another verified receipt channel over commits to the code branch: the Claude measurement showed status-only commits cancelling and restarting CI. Private reports and Imperatives never use a public comment as a convenient transport.

### Restart is a non-event

`hive-session-start` acquires the home lock, loads the Pool, charter, Imperatives, unresolved decisions, execution records and unacknowledged wakes, then schedules bounded live reconciliation. It does not relaunch everything in the journal. Reconcile only recorded direct reports, known vendor IDs, owned branches and known Harvests.

Persist an operation intent before each external side effect and its receipt after success. If a process dies between them, query the exact existing branch, Harvest, session or task before retrying. If the service cannot prove whether an unacknowledged launch succeeded, keep it unknown and request recovery; do not promise exactly-once submission from an API that lacks that guarantee. Missing local endpoints likewise require the runtime's recovery-grade proof before replacement.

Restore poll deadlines, rate-limit backoff, subscription registrations, review-head bindings and pending cleanup transitions from disk. Re-read current forge state before decisions that depend on it. A completed remote task can be collected after the Mac returns; new dispatch and Assimilation wait while the Mac is unavailable. GitHub Actions and vendor event handling may continue independently.

`hive-stow` saves unrecorded decisions, open work, handover facts and reusable knowledge to their proper private owners, curates stale memory, and checkpoints active operations. It does not substitute a conversation summary for task state or reconcile the whole Pool from memory. A failed save means the session is not reset-safe. Recovery reuses existing work, and guarded self-update never resets a dirty project to make versions align.

## Synapse on GitHub, with both reviewers

For validated Void delivery, Synapse combines **GitHub Actions, a Claude review routine, Auto-fix, and an Astra review**. Astra's target placement is Codex in the Void; local Codex supplies that review while the headless route is repaired. Both reviewer results are required by this path. No silently missing reviewer, empty diff, or green routine-infrastructure status can count as a pass.

The Fleet stores the selected Synapse executor and its policy revision before dispatch. Local validated work uses the retained no-mistakes pipeline. A validated Void Harvest uses the GitHub executor described here: this replaces where the stages run, not their obligations, and must not be represented as a completed no-mistakes run unless that integration actually exists. Run one fix owner at a time; never let local no-mistakes and cloud Auto-fix concurrently rewrite the same branch.

| Local pipeline obligation | GitHub Synapse replacement |
| --- | --- |
| Intent and scope | Fleet supplies a review packet with the applicable acceptance criteria, authorized scope and source/head identities. The canonical Imperative remains private. |
| Fresh base and rebase | Read the live base and mergeability; commission a bounded repair for conflicts or an out-of-date base as required by project policy. Auto-fix is not assumed to detect base-advance conflicts. |
| Review | Claude routine and Astra review, each bound to the same repository, base and current head, with explicit findings or a no-findings verdict. |
| Fix rounds | Auto-fix or a newly dispatched Swarm executes approved mechanical corrections; decisions about intent return to the Fleet. The Synapse ledger owns retry count and which actor may write. |
| Tests and build | GitHub Actions executes the project's agreed checks natively on Linux, Windows and macOS. Linux cross-compilation does not establish the other platforms' behavior. |
| Documentation and lint | Explicit documentation review plus deterministic formatting/lint commands in Actions; omissions are findings, not a presumed pass. |
| Publication and CI readiness | Verified Harvest identity, exact head, complete check set and reviewer attestations; an aggregate result suitable for branch protection. |

### Actions and review contracts

Actions runs on Harvest open and every relevant head update. Use three native OS jobs with stable check names and a final aggregate check. Record the actual runner image and architecture in evidence; moving `*-latest` labels do not define permanent platform support. Pin third-party Actions for the implementation and keep required jobs reporting even when no platform-specific source changed. Missing, cancelled, skipped, timed-out or failed required results do not pass. A superseded cancelled attempt can be ignored only when a newer successful run for the same check and exact head demonstrably replaces it.

Each project supplies executable test/build/smoke, formatter and linter commands in its validation configuration. No universal invented test command is part of Hive. Required local-machine evidence is collected by the Fleet and bound to the same source revision. If a needed native test cannot run on the available hosted runner, that requirement remains pending until the local machine proves it; do not quietly reduce platform coverage.

The Claude review routine is registered for each opened Harvest. Head changes, readiness changes and missed events also need a supported rerun route, with the Shadow reconciling coverage; an opened-only routine is insufficient after a fix. [Claude routines](https://code.claude.com/docs/en/routines) provide GitHub event and other trigger mechanisms, but the complete review/attestation loop still needs an integration measurement.

The review packet contains repository, base/head SHAs, public change purpose and acceptance criteria, allowed scope, validation expectations, and the applicable sanitized decisions. If the reviewer needs private task context, deliver it through an approved private session prompt; never paste the canonical Imperative into GitHub. Reviewers treat repository text and comments as task data, not authority to broaden scope or Assimilate.

Each review returns reviewer identity, actual model where verifiable, session/task receipt, policy version, base/head, verdict, and findings with stable IDs, severity, file/line, evidence, and proposed correction. Preserve the complete result privately; publish public-safe findings and a digest-backed attestation. A trusted Mac Synapse publisher verifies receipt ownership and binds the public check to the head. A branch author cannot satisfy the gate merely by adding a file claiming that reviewers passed. The check publisher's expected GitHub identity must be part of repository protection.

Astra runs a separate review session, not the implementation Swarm grading itself. Initially the Fleet can invoke local `codex exec` with its configured Astra model and supported final-message export into private Synapse evidence; verify those flags against the installed CLI. Once the repaired Void route meets the review readiness contract, the same review packet and output schema move to submit-and-collect without changing the gate semantics.

Codex's [GitHub review integration](https://developers.openai.com/codex/integrations/github/) documents `@codex review` and automatic reviews. It was not exercised in the measurement and does not by itself prove Astra selection or full headless result collection. It can be evaluated as a supported repair route; it is not an automatic substitute for the required Astra result.

### Auto-fix and completion

The measured Claude native subscription repaired a failed check and a review comment. Hive records the subscription owner, Harvest, allowed scope, current head, expiry and remaining repair rounds privately. Use the supported native Auto-fix mechanism; the observed internal tool names are evidence, not a promised public CLI contract. An implementation must verify a supported enable/disable route.

Default to at most three repair rounds per Imperative, configurable at intake. Deduplicate vendor events by Harvest/head/finding; changing the head invalidates both reviewer passes and starts checks and reviews for the new head. A routine must not repeatedly review its own status comment, and a fixer must not weaken the checks to produce green. A new product choice, destructive action, ambiguous correction, inaccessible required log or exhausted round limit stops the fix and returns a concrete decision/blocker to the Fleet. A Swarm does not answer an ask-user finding about its own instructions.

Synapse is ready only when the expected three-OS checks, documentation/lint obligations, both current-head reviews, and all required finding resolutions are present. The trusted publisher writes an aggregate attestation; the Shadow independently verifies it and records readiness. Before Assimilation, stop further branch-writing repairs and confirm unsubscribe/repair quiescence so an unnoticed late fix cannot race the reviewed head.

## Assimilation on the Mac

Only the Hive or the owning Fleet, acting through the Mac merge helper and within its recorded authority, can Assimilate a Harvest. A Swarm, Lictor, reviewer or Void subscription never merges. Local-only landing remains the Hive's guarded local operation.

`hive-assimilate <task-id> <harvest-url>` ports the checks and head binding of [the upstream merge helper](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-pr-merge.sh) and adds the GitHub Synapse evidence contract. It must:

1. Validate the task generation, owning home and full forge URL; confirm the repository and target base match the Imperative. Acquire the task/authority lock and refuse an unresolved Mind's word hold.
2. Verify explicit Assimilation authority, or the Hive Mind's recorded autonomous posture for this project and scope. Away authority is checked at the moment of action; quiet mode does not imply away authority. Security-sensitive, destructive or irreversible changes retain their explicit-decision boundary.
3. Read the live Harvest: open, not draft, current head, current base, mergeable and conflict-free. A temporarily unknown mergeability result allows a bounded reread, not an optimistic merge.
4. Read required checks from branch protection and active rulesets, including required producer identities. Verify every required result exists at the current head, all unwaived current checks are green, and validated delivery's two reviewer attestations and blocking-finding resolutions match that head and the required base. An unreadable requirements source blocks the operation.
5. Confirm collection/publication of the delivered commit and quiescence of any fix owner. Bind the merge request to the verified SHA, using the forge's supported expected-head guard corresponding to `--match-head-commit`. If the head or required evidence changes, stop and revalidate.
6. Read the forge back and record the actual merge SHA and time before claiming Assimilation. A queued request is pending, not landed; an unreadable response is unknown. Retain evidence for safe replay and cleanup.

The inherited attended exception for an explicitly named failing or missing check remains narrow: the Hive Mind must identify the concrete merge and check to waive. Record that exception and still enforce every other guard; neither autonomous posture nor away mode supplies a waiver. No worker can use administrative flags to skip Synapse. Repository identity, required-source readability and head binding are not inferred from a waiver.

`hive-assimilate-local <task-id>` ports the [local landing helper](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-merge-local.sh): verified local-only mode, recorded authority, released holds, clean owned branch and destination, and a fast-forward only. Divergence returns to a Swarm for reconciliation; it does not authorize a reset.

Cloud publication in the measurement used credentials that were not proven unable to merge. Production enablement therefore needs a demonstrated permissions/ruleset arrangement that permits Swarms to publish branches and Harvests while reserving protected-base updates and Assimilation to the authorized Mac route. Required check producers must resist author spoofing. This is an implementation prerequisite, not a security property already established by the experiment.

After confirmed Assimilation, guarded synchronization refreshes the project copy. Cleanup verifies that all work is safely reachable or otherwise proven landed, confirms task/slot ownership, preserves unresolved decisions, and records the Pool transition before retiring execution records. A merge does not justify deleting uncommitted files or a sibling's copy.

## Implementation evidence still required

The later rulings resolve the earlier disagreements about dropping local tooling and committing Imperatives: local capabilities remain, and canonical Imperatives are private dispatch material. Codex Void repair is required. No unresolved product choice is silently settled by treating one vendor as a permanent local-only path.

The following are open implementation issues. Keep each in the owning Pool with evidence and a completion condition:

| Issue | Evidence needed to close it |
| --- | --- |
| Codex Void repair | Supported environment/task discovery, model selection and complete final-text export; the Swarm and Astra replay described above. The underlying visibility cause remains open until demonstrated. |
| Claude session observation and private report collection | A supported native state reader, or an explicitly limited observation contract, plus durable private collection of a Lictor report without public leakage. Keep a Lictor local when its required private output cannot yet be collected from the Void. |
| Complete dispatch without a committed Imperative | Exercise both supported adapters with only a prompt-supplied Imperative, required tools already available, and no dependency on the Mac's private files. |
| GitHub Synapse | Exercise the Claude review routine, local then Void Astra collection, trusted attestation publication, both reviewers after a head change, bounded Auto-fix, and missing/stale-check rejection. The Claude delivery measurement alone did not prove this whole gate. |
| Result and log access | Prove authenticated receipts, full payload handling, private result retention and a usable path for platform-specific failure logs. |
| Assimilation enforcement | Prove the protected-base permission boundary, required check provenance, exact-head race refusal, current authority checks and verified post-merge result. |
| Recovery and resource use | Kill and restart the Mac supervisor at launch, collection, review, merge and cleanup boundaries; recover without duplicated work or lost decisions. Show that ordinary polling uses no model turns and respects configured machine capacity. |

Implement inherited local behavior first, then the shared execution records and adapter contract, then Claude and repaired Codex Void delivery, then the complete GitHub Synapse and Assimilation integration. Preserve working local routes throughout. This document specifies the target; deployment, vendor-account changes, and changes to the existing firstmate operation remain separate work.
