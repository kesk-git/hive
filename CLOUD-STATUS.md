# Cloud status

## Model

The environment of this session forbids writing a model identifier into an artifact pushed to a public repository, so the identifiers were withheld. The two sources named by the task, the sentence of the system prompt that names the model and the `model` and `effort_level` values of `session_context` as the `get_session` tool returned them, are answered yes or no instead, and both sources agree with each other.

model equals the one launched with, claude-fable-5-1: yes

effort equals xhigh: yes

The model that served the latest turn, as `external_metadata.last_served_model` reports it, equals the one launched with: yes.

## Changes

Branch: `claude/cloud-session-placement-5276ui`, which the environment made from the launching branch `fm/hive-design-cloud-recipe`.

The five rules, each once in `docs/design.md`, in the section that owns it:

1. **The launch passes the class as explicit model and effort.** Section "Claude in the Void", step 1 of the implementation sequence: the model and effort of the effective profile `hive-policy resolve` emitted, passed in the launch's argument list through the flags the installed CLI's probe confirms; a launch that names no model is refused, never left to the vendor's default; the class travels with the launch and never as a repository setting; Opus, Sonnet or Fable at any effort; a member of another vendor is refused by admission's contradiction check of harness and place, which the Pool contract already states. Acceptance: the `test-dispatch` proof paragraph that follows step 5 of the sequence (its first two clauses), and the clause added to the "Execution place per Hunt" row of "Implementation evidence still required" for what only a real session establishes.
2. **The prompt requires the session's own record.** Section "Render and deliver the Imperative", item 4, Definition of done: the completion evidence of a Hunt placed in a Claude cloud session includes the session's record, in a status file committed on its output branch, of its model and effort as the session itself reports them, the model sentence of its system prompt quoted exactly and the model and effort level its session tool returns. Acceptance: the clause added to the "Complete dispatch without a committed Imperative" row of "Implementation evidence still required".
3. **Collection verifies the collected commits against that record.** Section "Claude in the Void", step 4: each commit bound to the launched session by its session-binding trailer, the recorded model and effort compared with the class, the comparison recorded in the collection receipt; a difference, or a missing record, reported as a finding and never a clean collection; the session's record is the evidence of the observed model, which the execution record keeps apart from the requested one. Acceptance: the proof paragraph's clauses on a head collected clean, a head whose record differs and a head with no record.
4. **A cloud result is never the proof a check bound to the operator's machine gives.** Section "Synapse on the operator's machine, with both reviewers", first paragraph: Synapse and every check bound to the operator's machine run there over the collected head, whatever the session reported, and the session's report is task data for the gate, never a receipt. Acceptance: the clause added to the "Synapse on the operator's machine" row of "Implementation evidence still required".
5. **Commit trailers.** Section "Claude in the Void", the closing paragraph on committed repository instructions: a project whose Hunts may run in a Claude cloud session drops the co-author trailer by `includeCoAuthoredBy` false in its committed `.claude/settings.json`, which travels with the source where Hive's per-launch settings file does not; the session-binding trailer stays because collection binds the cloud branch to its session by it. Acceptance: the proof paragraph's last clause.

Where the task says the Mac, the design says the operator's machine, and the rules are written in those words.

Contract change, version **1.11**, a minor change:

- `spec/pool.md`, "The execution place": a new paragraph, "A generation placed in a Claude cloud session", records against the place the Pool defines what stage 4 does with a generation whose requested place is `claude-cloud-session`, as the design states it, and says that no Pool record changes for it.
- `spec/versions/1.11.md`: new, records the change and why it is minor.
- `spec/README.md`: the version line, 1.10 to 1.11.
- No schema file changed: `claude-cloud-session` keeps its name and its meaning in `records/pool.item.v1`, so its description stays true. The library's contract version constant does not move.

Contract gaps, stated in the design only: the collection receipt and the execution record's observed model are records of `dispatch` at stage 4, and the Synapse receipts are `synapse`'s at stage 5, none yet defined under `spec/`; the session's status file, the commit trailers and the repository setting are no Hive record. No new record kind, package, operation or dependency was added for them.

Unchanged, as the task states: no cloud environment is created or selected for a project; no repository-wide model pin; the four placement labels Local, Claude cloud session, By policy and Codex Cloud (parked); Codex Cloud stays parked; this repository's `.claude/settings.json`; `spec/versions/1.9.md`, which the task's file list named by a stale line.

Files touched: `docs/design.md`, `spec/pool.md`, `spec/README.md`, `spec/versions/1.11.md` and this file.

## PR description

Hive's design states five rules for a Hunt placed in a Claude cloud session, as one design revision, and the Pool contract records against the place it defines what the design says is done with that place. A cloud session runs the model it is launched with, nothing readable on the operator's machine names that model afterwards, and what the session reports of its own checks is not a check of the operator's machine. The rules make the class travel with the launch, make the session record what it ran, and make collection and the gate treat that record as evidence to verify and never as proof.

The five rules, each in the section of `docs/design.md` that owns it:

1. **The launch passes the class** (Claude in the Void, step 1 of the implementation sequence). The launch names the model and the effort of the effective profile resolved for the Hunt's class, beside the Imperative, through the flags the installed CLI's probe confirms. A launch that names no model is refused, because the session would run a default model the class did not choose. Every Claude member of a class is honoured this way, Opus, Sonnet or Fable at any effort; a member of another vendor cannot be placed in a Claude cloud session, which admission already refuses as a contradiction of harness and place.
2. **The prompt requires the session's own record** (Render and deliver the Imperative, Definition of done). The session records, in a status file committed on its output branch, its own model and effort as it reports them: the model sentence of its system prompt and the model and effort level its session tool returns.
3. **Collection verifies the collected commits against that record** (Claude in the Void, step 4). Each commit is bound to the launched session by its session-binding trailer, and the recorded model and effort are compared with the class. A Hunt whose record differs from its class, or has no record, is not collected as clean; the difference is a finding. The session's own record is the evidence of the model it ran, and the commits corroborate it.
4. **A cloud result is never the proof a check bound to the operator's machine gives** (Synapse on the operator's machine, with both reviewers). Synapse and every check bound to the operator's machine run there over the collected head, whatever the session reported.
5. **Commit trailers** (Claude in the Void, the paragraph on committed repository instructions). A project whose Hunts may run in a Claude cloud session drops the co-author trailer by its repository setting, `includeCoAuthoredBy` false in its `.claude/settings.json`; the session-binding trailer stays, because collection binds the cloud branch to its session by it.

Acceptance cases: a `test-dispatch` proof paragraph follows the implementation sequence of Claude in the Void, and three rows of the Implementation evidence still required table name what a real session must establish: Execution place per Hunt, Complete dispatch without a committed Imperative, and Synapse on the operator's machine.

Contract: version 1.11, a minor change. `spec/pool.md`, The execution place, gains the paragraph "A generation placed in a Claude cloud session"; `spec/versions/1.11.md` records it; `spec/README.md` states the version. No schema changes and no new record kind, operation, package or dependency; the library's contract version constant does not move. The collection receipt, the execution record and the Synapse receipts are stage 4's and stage 5's records with no contract yet, and the status file, the commit trailers and the repository setting are no Hive record, so those parts of the rules stand in the design alone.

Unchanged: no cloud environment is created or selected for a project; no repository-wide model pin, which would fix one model per branch while the class varies per Hunt; the four placement labels Local, Claude cloud session, By policy and Codex Cloud (parked); Codex Cloud stays parked; this repository's `.claude/settings.json` already sets `includeCoAuthoredBy` false.

## Checks

Run from the repository root after the design and contract edits, and again after this file was written:

- `make check`: passed. The formatter reported no unformatted file and `go vet ./...` reported nothing.
- `make test-contract`: passed. `go test ./contract/... ./spec/...` reported ok for canon, cli, clock, envelope, ids, journal, schema and store; `spec` has no test files, as before.

done claude/cloud-session-placement-5276ui
