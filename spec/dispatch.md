# Dispatch and supervision contract

Owner: `dispatch`, with observation by `shadow` and durable work identity in `pool`. This file carries the design's prospective stage 4 behaviour and acceptance cases. Its record and operation schemas enter with that stage; these requirements add no new authority or executor.

## Dispatch matrix

The matrix is one plain-JSON record of the home, written only through a policy request with Accepted and Applied receipts. The resolver reads it at dispatch and at each fixer's launch; workers receive a resolved profile and never read the matrix. Its entries hold:

- Hive and Fleet supervision profiles, distinct from worker classes.
- Classes 0–7, each with eligible members per harness, placement preferences naming supported backends or policy, and an optional Lictor member override.
- A default profile and selection data: provider weights, gauge comparisons, conditional member substitutions, floors with comparison operators, and tie rules. Each selection rule carries scope, provenance and expiry. Temporary vendor preferences remain temporary rule records.
- A gate entry with configured reviewer roles resolved for class 2, validation steps, a fixer, its explicit step-up member and `after_round`, fix-turn budgets, check bounds and escalation signals. Roles add no dispatch class. The class-1 step-up stays in the fixer's harness and vendor.
- Per profile: harness, real model name accepted by its CLI, effort, capabilities and selection rationale with source and date. A harness-to-vendor table supports vendor-relative comparisons.

The matrix supports any number of vendors. A class's first and second member may name the same vendor. The gate's reviewer roles are whatever its effective entry names, with the same vendor allowed; no counterpart or reviewer-diversity rule constrains that choice. Route qualification checks the reviewer results the effective configured gate requires against what the installed plugin actually supplies, never an independent fixed-count assumption. Adapter facts and operator scope rulings remain named; policy chooses roles and classes through the matrix.

`test-dispatch` proves supervision resolves the respective entries, a changed member is used at the next launch without changing a running generation, the Lictor override affects the intended role only, floor equality and ties follow data, a cross-vendor step-up is refused, and same-vendor first and second members and reviewer roles are accepted. It also proves a matrix with more than two vendors resolves by the same rules. Route qualification probes each real model name and effort before launch. `test-synapse` proves missing or substituted reviewer results cannot satisfy the effective gate entry.
