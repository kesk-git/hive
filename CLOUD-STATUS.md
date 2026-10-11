# Cloud status: the hierarchy mockup review in the design and the Pool contract

Branch: `claude/codex-cloud-rename-f1r8i6`, opened from `fm/hive-design-mockup-review`.

## What changed

One design revision and one minor contract change, version 1.8.

- The Codex cloud place is named Codex Cloud everywhere, and the record value is `codex-cloud`. The Void on its own is unchanged, including the phrase "Codex in the Void" that explains the value.
- `docs/design.md` gains the nine sentences the review settled.
- `spec/pool.md` states three settled points as sentences of the contract and drops their rows from Left undecided.
- `spec/schemas/records/pool.item.v1.schema.json` renames the enumeration value and its description.
- `packages/pool/hive-package.json` renames the place in its test scope text.
- `spec/README.md` raises the contract version to 1.8 with a clause in the style of the ones before it.

## Where each of the nine design sentences was applied

All in `docs/design.md`.

1. The Wave row of the level table under Vocabulary and placement, its Shown as cell: Waves are also the Pool's group headers in roadmap order, and never appear on the map.
2. The Pool row of the views table under Home and navigation: the grouping by Wave, the four columns Not started, Hunting, Confirming and Captured with each Prey's Hunts beneath it, the Waiting on you strip at the top, and the collapse of a column a Subsector has nothing in, appended to the sentence on columns emptied by the filter.
3. The Waiting on you row of the views table: the same decisions also appear in the Pool as held items among the waiting decisions, and an imported hold with no question is not among them.
4. The Task inspector row of the views table: it holds for a Hunt, which also shows its Prey and Target above it; selecting a Target opens its Prey and selecting a Prey opens its Hunts; the place is labelled Mac, Claude cloud session, By policy or Codex Cloud (parked), with the requested place beside the actual one.
5. The Swarm row of the views table: the dispatch-matrix editor's placement column reads in the same four labels.
6. The Fleet overview row of the views table: Subsector rows carry their count of Targets beside their next due entry.
7. The Decision cards section, after the sentence that work held on a gate, an order, a lift condition or a fold shows as blocked: the imported hold with no question shows as a hold on its item, held with no question yet, and becomes a decision card only once a question is raised.
8. The Targets, Prey and Hunts row of the Record model table, its Presentation cell: a Target's Prey are counted by column in the screen's words, for example 2 captured, or 1 confirming and 1 not started, not as landed of drawn.
9. The first paragraph of Definitions, standing rules and generated knowledge, after the sentence on the Target's passage: building reads Hunting, a Prey awaiting its validation reads Confirming, landed reads Captured, as display words only, with the record states and the derived facts keeping their names.

No sentence about the map changed.

## Where the three settled rows of the Pool contract went

All in `spec/pool.md`, each row removed from Left undecided and its answer stated once where its subject is defined.

- Whether a command offers a short form at a terminal: the One writer bullet of The store, which names the commands. No command offers a short form at a terminal; the request document is the one form.
- Where a decision item is shown: the opening paragraph of Items, which defines the decision item. It is shown in the Pool as a held item among the waiting decisions.
- The labels of the placement values: a sentence after the placement values table of The execution place. The UI labels them Mac, Claude cloud session, By policy and Codex Cloud (parked).

## Notes

- The version clause for 1.8 names the renamed value as "the parked placement value, Codex in the Void", not by its former spelling, because the done condition requires that a search for the former spelling over the repository finds nothing.
- `spec/decisions.md` keeps one sentence in the cost paragraph of a stage 1 decision saying a command may offer a short form that builds the same request. That file is outside the files this change may touch and the sentence is a stage 1 decision's cost note, not the Pool contract, so it was left as it is. It is noted here in case the next revision wants it aligned.
- Nothing under `contract/` and no code file named the Codex place; the search found it only in the design, the Pool contract, the item schema and the Pool package's test scope text.
- The library's contract version constant stays at 1.5, as the clause says.
- The machine's Go fetched the toolchain the module requires without difficulty.

## What the commands printed

`make check`:

```
go vet ./...
```

exit 0.

`make test-contract`:

```
go test ./contract/... ./spec/...
ok  	github.com/kesk-git/hive/contract/canon	0.015s
ok  	github.com/kesk-git/hive/contract/cli	0.597s
ok  	github.com/kesk-git/hive/contract/clock	(cached)
ok  	github.com/kesk-git/hive/contract/envelope	0.089s
ok  	github.com/kesk-git/hive/contract/ids	0.012s
ok  	github.com/kesk-git/hive/contract/journal	0.416s
ok  	github.com/kesk-git/hive/contract/schema	8.819s
ok  	github.com/kesk-git/hive/contract/store	5.387s
?   	github.com/kesk-git/hive/spec	[no test files]
```

exit 0.

The case-insensitive search over the repository, excluding `.git`, for the former name in its three spellings and with any non-word characters between the two words, printed nothing, on single lines and across line breaks alike.

done claude/codex-cloud-rename-f1r8i6
