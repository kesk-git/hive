# Backlog

## In flight
- **keeps-gadget-h7** - PERSISTENT SECONDMATE (kind=secondmate, home ./gadget, harness demo). Owns the gadget end to end, including its release cycle and the store review watch. An idle pane is healthy; it reports by status writes. (since 2026-10-02)
- **cache-adopt-l5** - SHIP (acme, widget): adopt the durable cache in start-up - take a home with `widget get --cache` and give it back on retirement. (since 2026-10-02). **pipeline running.**
- Release domain (owned by keeps-gadget-h7): 2.0.4 IN REVIEW; next release held. Full detail in the supervisor's home.
- (status) Mobile ladder: 0.0.4 confirmed good on a device 2026-10-01. More rough edges to flag later.

## Queued
- [ ] cache-adopt - adopt the durable cache in the start-up path blocked-by: cache-core-t4 (repo: acme)
- [ ] promote-check - staged promotion of widget v2.3.1 (a candidate; cut as a prerelease via #308 merge 2026-10-01 - carries the route fix). REMAINING: validate, then flip to latest. (local + repo: widget)
- [ ] key-cleanup - port the post-upload key pruning to the release workflow. Keep the newest 2, never touch the signing keys. (repo: monorepo)
- [ ] launch-gate (GATED) - the full launch checklist is the SINGLE SOURCE OF TRUTH at data/launch.md. Discrete tasks spawn from there.
- [ ] (later roadmap, data/plan.md) Phase 6 hardening remainder, concurrent bots.

## Done (10 most recent)
- [x] survey-scout-d4 - SCOUT - data/survey-scout-d4/report.md (reported 2026-10-02): assessment of a backlog tool. VERDICT BUILD-FIRST: own repo, a markdown backend owning backlog.md in place. (reported 2026-10-02)
- [x] cache-core-t4 - SHIP MERGED https://github.com/acme/widget/pull/35 (squash, 2026-10-02): durable work-tree cache with state kept apart from live processes. (merged 2026-10-02)
- [x] PR #41 (outsider) - SHIP MERGED https://github.com/acme/widget/pull/41 (squash, 2026-10-02): teardown treats work as landed when HEAD is on any remote. Reviewed MERGE AS-IS, contributor thanked. (merged 2026-10-02)
- [x] route-fix-k4 - SHIP (widget, PR https://github.com/acme/widget/pull/306, merged by hand, released v2.2.0): clean-slate route fix. Supersedes outside change #296 (closed with a link). Unblocks the fork flow. (merged 2026-10-01)
- [x] notes-multi-w8 - SCOUT - data/notes-multi-w8/report.md: isolated end-to-end run of the feature.
  Follow-up note added later: the harness environment was the cause, not the feature.
  Second continuation line for good measure. (reported 2026-10-02)
