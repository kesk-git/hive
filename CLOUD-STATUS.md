# Cloud status: platform answers design revision

This environment forbids writing a model identifier into a repository, so the model lines take the yes-or-no form, with the identifier left out.

- model equals the one launched with: yes
- effort equals xhigh: yes

## Changed, by decision

1. **Local.** `docs/design.md`, `ui` → Home and navigation, the Task inspector row (contract sentence and `test-ui` case); The Swarm row points to it. `spec/pool.md`, The execution place.
2. **Hive's own thin terminal host.** `docs/design.md`, Platforms → The terminal host (replaces The terminal runtime), with its `test-dispatch` case; plan stage 4 names its first lane, stage 3 qualifies against it. tmux leaves the core: six become five across Tooling, the inventory, Kept executors and the evidence table; `spec/distribution.md`, the two schema enums, the `dispatch` and `shadow` declarations and the fixtures.
3. **Enrolment with a redacted record.** `docs/design.md`, Secrets → How a secret is enrolled, and what Hive checks, with `test-contract` and `test-dispatch` cases; Tendril's bounds and the command-contract table follow. `spec/test-targets.md`.
4. **go-keyring.** `docs/design.md`, Secrets → Which store on which platform, with its `test-dispatch` case; `spec/distribution.md`, The toolchain; `spec/imports.md`, `spec/packages.md`.
5. **Claude Code's login file.** `docs/design.md`, Secrets → How a value reaches exactly one command: the named exception, told apart by the file's presence and owner-only access, with its `test-dispatch` case; the conditions table gains its row.
6. **Hive's own app.** `docs/design.md`, `ui` → Hive's own app (new), with the measured probe and its acceptance case; Review → Serving and Overlay follow; plan stage 6 names the app's first lane. `spec/distribution.md` (no listener), `spec/review.md`, `spec/decisions.md` decisions 4 and 15.
7. **purego.** `docs/design.md`, Platforms → Build, test and distribution, with the standard-library rule's two named exceptions, the accepted system parts and a `test-distribution` case; `spec/distribution.md`, The artefact and The toolchain.
8. **Lifetime and notifications.** `docs/design.md`, Platforms → Credentials, files and resident processes → The app's lifetime and notifications, with its acceptance case; `spec/distribution.md`, Resident processes and listeners.

The Open choices table and every pointer to Q1, Q2, Q3, Q4 and A1 are gone from the design and the specification; the Assumptions replaced rows say what replaced each. The contract change is version 1.10 (`spec/versions/1.10.md`, `spec/README.md`), amended in place as a minor change by the precedent of 1.5 and 1.7. `README.md` names five core dependencies.

## Checks

- `make check`: pass (exit 0).
- `make test-contract`: pass (exit 0), run uncached.

done fm/hive-design-platform-answers
