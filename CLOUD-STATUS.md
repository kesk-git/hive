# Cloud status: split the contract version history

## What changed

- `spec/versions/1.0.md` through `spec/versions/1.8.md`: one file per contract version, each opening with the heading `# Version <n>` and holding that version's clause, moved from the version paragraph of `spec/README.md`.
- `spec/versions/README.md`: two sentences, one saying each file records one contract version and what it changed, one saying the current version is the one `spec/README.md` states.
- `spec/README.md`: the version paragraph is now the sentence `Contract version: **1.8**.` and one sentence pointing to `versions/`. The last sentence of "Changing a contract" gains the rule that a change raises the version stated there and adds its own file under `versions/`, named for the version it introduces, and that a change rebased behind another that took its number renames only its own file and that one line.
- No other file changed. No link anywhere in the repository pointed to the version paragraph by an anchor (searched for `README.md#` and for anchors naming a version); the links to `spec/README.md` from the root README and the seven package READMEs carry no anchor and still resolve.

Two mechanical adjustments were made to the moved text, and nothing else:

- The clause for 1.0 ended in the semicolon that joined it to 1.1; it now ends in a period, and the 1.1 clause opens with `Version 1.1, later the same day` instead of the bare `1.1, later the same day`, as decided.
- The clauses move one directory down, so their three relative links were shifted up one directory (`records.md` and `pool.md` to `../records.md` and `../pool.md`; `../docs/design.md` to `../../docs/design.md`). The link text is unchanged.

This changes no contract, so the version stays 1.8 and no `1.9.md` is added.

## Verification of the move

A script read `spec/README.md` at the launching commit (fcf3dde), took the version paragraph after `Contract version: **1.8**. `, and compared it with the nine files reassembled in order after undoing the two adjustments above (the period back to a semicolon joining 1.0 and 1.1, `Version 1.1` back to `1.1`, and the link paths back one directory). The reassembled text was byte-identical to the paragraph at the launching commit. The same script confirmed that no `Version 1.<n> ` clause opener remains in `spec/README.md` and that each clause's opening appears in exactly one file under `spec/versions/`.

## Checks

`spec/embed.go` embeds only `schemas` (`//go:embed schemas`), so `spec/versions/` is not embedded; no check said otherwise.

`make check` printed:

```
go vet ./...
```

and exited 0 (the gofmt listing was empty).

`make test-contract` printed:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.023s
ok  	github.com/kesk-git/hive/contract/cli	0.858s
ok  	github.com/kesk-git/hive/contract/clock	0.004s
ok  	github.com/kesk-git/hive/contract/envelope	0.042s
ok  	github.com/kesk-git/hive/contract/ids	0.007s
ok  	github.com/kesk-git/hive/contract/journal	0.496s
ok  	github.com/kesk-git/hive/contract/schema	9.198s
ok  	github.com/kesk-git/hive/contract/store	5.452s
?   	github.com/kesk-git/hive/spec	[no test files]
```

and exited 0. The module's Go 1.27 toolchain was fetched without trouble. No other target was run.

done claude/split-contract-versions-62apkb
