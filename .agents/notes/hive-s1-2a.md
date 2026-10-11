# Task notes: hive-s1-2a

Durable project-intrinsic knowledge this task produced.
The fold routes each fact below into the destination document and then retires this file.
Keep the bar high: knowledge useful to almost every future session, and a pointer to the authoritative file or command rather than detail the code already shows.

## Destination

AGENTS.md

## Notes


- The schema validator is `contract/schema`: `Embedded()` loads and checks every file under `spec/schemas/` (profile, closed objects, `$id` derived from the path, references, reference cycles), and `Validate(ref, bytes)` decodes strictly (whole numbers, unique member names, no lone surrogates) before checking. A schema edit that breaks the profile fails `make test-contract` at once; `Keywords()` is compared with the list in `spec/records.md`, so change both together.
- A `pattern` is held to the *syntax* common to RE2 and ECMAScript, not to identical meaning. `\s` and `.` are in that syntax but differ between Go and a browser (Unicode whitespace and line terminators); `import-record.v1.schema.json` uses `[^\s]`. Keep patterns ASCII-only in their intent, or state the narrower class explicitly.
