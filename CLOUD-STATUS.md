# Cloud status: secret-handling contract revision

Branch: `claude/secret-handling-contract-4qx7lc`, created from `fm/hive-design-secrets-in-keychain`. Documentation only: `docs/design.md`, `REVISION-NOTES.md` and this file. `README.md` checked in both rounds and unchanged; nothing under `spec/` edited.

## Round 1

1. Read `docs/design.md` in full and the contracts under `spec/`.
2. Added the section "Secrets: Hive keeps none" to `docs/design.md`, pushed.
3. Replaced every restatement elsewhere in the document with a reference to that section, pushed.
4. Wrote `REVISION-NOTES.md`; pushed.
5. `make check` exit 0; `make test-contract` exit 0.

## Round 2

1. Fixed the untrue and contradictory sentences: the intent carries whole bodies, so the owner's no-value check runs before the intent is written; one contract for the value type (encodings fail, print form is one fixed redaction); a Spoor's four fields beyond what every record carries, with its name defined as its identifier and prefix `spoor_`; who validates (the owner, with the schema package; the store unchanged); the UI session's own authentication material reconciled with the Overlay paragraph; the keychain as the macOS default.
2. Added the missing contract content: Tendril small and audited, with Prove cases; the four command-contract rows rewritten to refuse-whole-or-keep-exactly; the presence check retrieves no value; the self-kept Spoor for the three tools' own logins; all six dependencies in the passes-nothing paragraph; the Windows and Linux question as one Mind's word with each condition's status and code; who raises and records the incident.
3. Added the references still missing: Synapse findings and fix rounds, the Shadow's observations, `ui` card answers, submitted rounds and the Overlay, the legacy import in Fork and adapt and stage 2, and the two vocabulary rows.
4. Updated `REVISION-NOTES.md`: the spec list completed, `secrets-detection` split into recognizer and detector, `secrets-ui-session` added, section log corrected.
5. Checks on the final state: `make check` exit 0; `make test-contract` exit 0 (`contract/clock`, `contract/ids`, `contract/schema`, `contract/store` ok; `spec` has no test files). Go toolchain `go1.27.0 linux/amd64`, as go.mod requires.

## Round 3

1. The evidence row now says the presence check finds the item by its attributes and retrieves no value; no other sentence outside the section described the check as resolving.
2. "Where the parts enter the plan" says once when the no-value check, the value type, a possible recognizer and the `spoor_` prefix each enter; stage 2's text agrees.
3. The `dispatch` to `word` call is named beside `synapse` to `dispatch`, with the graph acyclic; the notes list the `spec/packages.md` row and the declaration it touches.
4. Model transcripts have their true reason in "What Hive holds".
5. The findings sentence and the Shadow sentence are whole either way: recognized value refused whole, unrecognized kept exactly as a found copy.
6. Checks on the final state: `make check` exit 0; `make test-contract` exit 0.

done claude/secret-handling-contract-4qx7lc round 3
