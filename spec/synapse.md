# Synapse workflow contract

Owner: `synapse`. This file carries the behavioural obligations and acceptance cases of the design's [local gate](../docs/design.md#synapse-on-the-operators-machine-with-both-reviewers), for stage 5's plugin route and the later native route. It adds no executable or record schema; stage 5 defines those interfaces. `test-synapse` proves the cases against recorded executors, with route qualification separately proving the installed plugin's capabilities.

## Review rounds and rulings

The review and fix loop has one fix writer. Review rounds are uncapped. From round 3, review covers the delta since the last reviewed head and any production-correctness or security defect anywhere; coverage, wording, simplification and documentation findings become Hunt notes. An approve with zero blockers ends the review loop unless the fix touched production code, which requires review at its new head. Required checks and the other readiness obligations still stand. Fix-turn budgets and escalation can hold or park work but cannot make a defect pass or impose a review-round cap.

A ruling is the finding plus one sentence. A fixer may skip a finding it cannot place with confidence, must say so in its turn, and leaves it unresolved rather than claiming it fixed. Ask-user findings still require the complete owning-supervisor disposition; neither uncertainty nor a declined finding supplies missing review evidence.

The [test contract](test-targets.md#acceptance-proof-and-pre-checks) owns acceptance-test-first, scenario coverage and proof-of-record placement. No workflow rule has a freeze window; the Hive Mind may amend it whenever it says.

Prove a third-round review finds a production defect outside the delta while filing coverage and wording as notes; a zero-blocker approval ends after a non-production fix but a production fix receives new-head review; further required rounds remain possible beyond the escalation signal; and one-sentence rulings and explicit uncertain skips keep their original findings and unresolved dispositions.
