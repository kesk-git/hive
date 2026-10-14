package legacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kesk-git/hive/packages/pool/internal/item"
)

// The acceptance test of the legacy grammar (spec/pool.md, Legacy import and
// export): a backlog whose lines end in CRLF and whose last line has no line
// end parses and renders back byte for byte, and the import record names the
// translation with the licence it keeps.

// TestRoundTripKeepsCRLFAndNoFinalNewline parses the fixture and renders it
// again; the bytes must be the ones read, on every platform.
func TestRoundTripKeepsCRLFAndNoFinalNewline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", "crlf-no-final-newline.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(src, []byte("\r\n")) || bytes.HasSuffix(src, []byte("\n")) {
		t.Fatal("the fixture must hold CRLF line ends and no final newline")
	}
	b, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Render(); !bytes.Equal(got, src) {
		t.Fatalf("the rendering differs from the source\n got %q\nwant %q", got, src)
	}
}

// TestImportRecordNamesTheTranslation reads the record of the import and the
// licence text it keeps.
func TestImportRecordNamesTheTranslation(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "imports", "tasks-axi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		ImportID string `json:"import_id"`
		Kind     string `json:"kind"`
		Owner    string `json:"owner"`
		Upstream struct {
			Revision string `json:"revision"`
			Tag      string `json:"tag"`
		} `json:"upstream"`
		Licence struct {
			SPDX      string   `json:"spdx"`
			Copyright []string `json:"copyright"`
			Files     []struct {
				Path   string `json:"path"`
				Digest string `json:"digest"`
				Bytes  int    `json:"bytes"`
			} `json:"files"`
		} `json:"licence"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ImportID != "tasks-axi" || rec.Kind != "translation" || rec.Owner != "pool" {
		t.Fatalf("the record is %q, %q, %q; want tasks-axi, translation, pool", rec.ImportID, rec.Kind, rec.Owner)
	}
	if rec.Upstream.Revision != "9401ff899c0d1d8ae6b4fd8727b9025abda2032c" || rec.Upstream.Tag != "tasks-axi-v0.2.6" {
		t.Fatalf("the record names upstream %q at %q", rec.Upstream.Revision, rec.Upstream.Tag)
	}
	if rec.Licence.SPDX != "MIT" || len(rec.Licence.Copyright) != 1 || rec.Licence.Copyright[0] != "Copyright (c) 2026 Kun Chen" {
		t.Fatalf("the record's licence is %q with notices %q", rec.Licence.SPDX, rec.Licence.Copyright)
	}
	if len(rec.Licence.Files) != 1 || rec.Licence.Files[0].Path != "packages/pool/upstream/tasks-axi/LICENSE" {
		t.Fatalf("the record keeps %+v", rec.Licence.Files)
	}
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rec.Licence.Files[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(text)
	if len(text) != 1065 || rec.Licence.Files[0].Bytes != 1065 ||
		rec.Licence.Files[0].Digest != "sha256:"+hex.EncodeToString(sum[:]) ||
		rec.Licence.Files[0].Digest != "sha256:945016bd37e1ba7211622ef60ee1d23ab727896ba7710edd21e8fbe983863969" {
		t.Fatalf("the kept licence is %d bytes with digest %x", len(text), sum)
	}
}

// ---------------------------------------------------------------------------
// Translated tests. Upstream: tasks-axi 0.2.6 at revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
//
// test/backends/markdown-grammar.test.ts, describe "markdown grammar". The
// upstream's two sample backlogs are not copied: backlog.md and
// checkbox-in-flight.md under testdata/legacy re-express them (FIXTURE and
// FIRSTMATE_FIXTURE of its test/helpers.ts) with invented identifiers, and the
// third sample, MULTI_REASON_FIXTURE, is written out in its test. Hosts,
// identifiers and dates are Hive's; the behaviour tested is the upstream's.
//
// "byte-exact round-trip":
//   - "render(parse(src)) === src on the corpus fixture": TestCorpusFixtureRoundTrips
//   - "round-trips CRLF fixtures while still recognizing tasks": TestCRLFCorpusRoundTripsAndStillParses
//   - "round-trips an empty file": TestRoundTripsSmallFiles
//   - "round-trips a file with no trailing newline": TestRoundTripsSmallFiles
//   - "round-trips a file that is only a heading": TestRoundTripsSmallFiles
//   - "render(parse(src)) === src on the real backlog": not carried; it reads
//     a file of the upstream author's machine and is skipped without it.
//
// "parse":
//   - "recognizes the three sections and their states": TestParseRecognizesSectionStates
//   - "recognizes slug ids and leaves odd/annotated lines free-form": TestParseRecognizesSlugIDs
//   - "extracts repo, blocked-by deps, and a leading-word kind": TestParseExtractsRepoDepsAndKind
//   - "reads `(local + repo: X)` as repo X": TestParseReadsLocalPlusRepo
//   - "captures only the trailing date tag, leaving mid-sentence ones in prose": TestParseCapturesOnlyTheTrailingDate
//   - "extracts a trailing priority tag": TestParseExtractsPriority
//   - "extracts structured hold tags from the trailing tag region": TestParseExtractsHold
//   - "reads indented continuation lines as the body": TestParseReadsContinuationLines
//   - "keeps internal blank lines inside a multi-paragraph body": TestParseKeepsInternalBlanks
//   - "treats whitespace-only lines as blank body separators": TestParseTreatsWhitespaceAsBlank
//   - "treats indented pseudo-headings as body, not section boundaries": TestParseIndentedHeadingsAreBody
//   - "keeps a trailing blank in the item block raw while structured body strips it": TestParseKeepsTrailingBlankRaw
//   - "leaves single-line items without a body unchanged": TestParseSingleLineHasNoBody
//   - "round-trips a done bullet with a Forgejo pull URL byte-exactly": TestParseForgejoDoneBullet
//   - "derives typed links from prose", "derives a Forgejo pulls URL as the
//     single pr link" and "keeps non-canonical PR-ish URLs as doc links, never
//     pr": in internal/item/links_test.go, where deriveLinks lives here.
//
// "canonical render":
//   - "orders tags as deps, repo, kind, date": TestRenderOrdersTags
//   - "omits the kind tag when the prose already leads with the kind word": TestRenderOmitsKindTagForLeadingWord
//   - "renders a done task with the link-derived closure verb": TestRenderDoneUsesClosureVerb
//   - "renders the body as indented continuation lines": TestRenderBodyAsContinuationLines
//   - "renders blank body paragraphs as blank lines, not two spaces": TestRenderBlankBodyLines
//   - "dirty re-render preserves multi-paragraph bodies including internal blanks": TestDirtyRenderKeepsParagraphs
//   - "renders structured holds as canonical human-readable tags": TestRenderHold
//   - "normalization is idempotent: normalize(normalize(x)) === normalize(x)": TestNormalizationIsIdempotent
//
// "firstmate format interop" (the name the upstream gives the group):
//   - "render(parse(src)) === src on the firstmate-shaped fixture": TestInteropFixtureRoundTrips
//   - "parses the `- [ ] <id>` checkbox in-flight form firstmate uses": TestInteropCheckboxInFlight
//   - "still parses the legacy `- **<id>**` in-flight form": TestInteropBoldInFlight
//   - "normalizes in-flight items to `- [ ]`, never `- **id**`": TestInteropNormalizesToCheckbox
//   - "parses `blocked-by: <id> - <reason>`, keeping both id and reason": TestInteropParsesDepReason
//   - "still parses a bare `blocked-by: <id>` (no reason)": TestParseExtractsRepoDepsAndKind
//   - "renders a blocked-by dep with its free-text reason": TestInteropRendersDepReason
//   - "round-trips the blocked-by reason through a normalize cycle": TestInteropReasonSurvivesNormalize
//   - "parses multiple reason-bearing blockers without folding the later edge": TestInteropMultipleReasons
//   - "does not derive task links from dependency reason text": TestInteropNoLinksFromReason
//
// "leadingKind":
//   - "maps the firstmate leading words": TestLeadingKind
//
// The cases below TestLeadingKind are Hive's own: the grammar on bytes the
// upstream never saw, and the boundaries of the tail patterns.
// ---------------------------------------------------------------------------

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustParse(t *testing.T, src string) *Backlog {
	t.Helper()
	b, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func render(b *Backlog) string { return string(b.Render()) }

func markAllDirty(b *Backlog) {
	for _, s := range b.Sections {
		for _, e := range s.Entries {
			if e.Task != nil {
				e.Dirty = true
			}
		}
	}
}

func toCRLF(src string) string {
	return regexp.MustCompile(`\r?\n`).ReplaceAllString(src, "\r\n")
}

func taskByID(t *testing.T, b *Backlog, id string) *Task {
	t.Helper()
	task := b.Find(id)
	if task == nil {
		t.Fatalf("no task %q", id)
	}
	return task
}

func ids(b *Backlog) []string {
	var out []string
	for _, t := range b.Tasks() {
		out = append(out, t.ID)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func assertRoundTrip(t *testing.T, src string) *Backlog {
	t.Helper()
	b := mustParse(t, src)
	if got := render(b); got != src {
		t.Fatalf("the rendering differs from the source\n got %q\nwant %q", got, src)
	}
	return b
}

// ---- byte-exact round trip ----

func TestCorpusFixtureRoundTrips(t *testing.T) {
	assertRoundTrip(t, readFixture(t, "backlog.md"))
}

func TestCRLFCorpusRoundTripsAndStillParses(t *testing.T) {
	src := toCRLF(readFixture(t, "backlog.md"))
	b := assertRoundTrip(t, src)
	got := ids(b)
	for _, id := range []string{"key-cleanup", "cache-core-t4"} {
		if !contains(got, id) {
			t.Errorf("ids %v miss %q", got, id)
		}
	}
	if body := taskByID(t, b, "notes-multi-w8").Body; strings.Contains(body, "\r") {
		t.Errorf("body %q keeps a carriage return", body)
	}
}

func TestRoundTripsSmallFiles(t *testing.T) {
	for name, src := range map[string]string{
		"empty file":          "",
		"no trailing newline": "# Backlog\n\n## Queued\n- [ ] a-task - do a thing",
		"only a heading":      "# Backlog\n",
	} {
		t.Run(name, func(t *testing.T) { assertRoundTrip(t, src) })
	}
}

// ---- parse ----

func TestParseRecognizesSectionStates(t *testing.T) {
	b := mustParse(t, readFixture(t, "backlog.md"))
	var got []State
	for _, s := range b.Sections {
		got = append(got, s.State)
	}
	if want := []State{StateInFlight, StateQueued, StateDone}; !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

func TestParseRecognizesSlugIDs(t *testing.T) {
	got := ids(mustParse(t, readFixture(t, "backlog.md")))
	for _, id := range []string{"keeps-gadget-h7", "cache-adopt", "survey-scout-d4"} {
		if !contains(got, id) {
			t.Errorf("ids %v miss %q", got, id)
		}
	}
	// "launch-gate (GATED) - ..." and "PR #41 (outsider) - ..." are free-form.
	for _, id := range []string{"launch-gate", "PR"} {
		if contains(got, id) {
			t.Errorf("ids %v hold %q, a free-form line", got, id)
		}
	}
}

func TestParseExtractsRepoDepsAndKind(t *testing.T) {
	b := mustParse(t, readFixture(t, "backlog.md"))
	task := taskByID(t, b, "cache-adopt")
	if task.Repo != "acme" {
		t.Errorf("repo = %q, want acme", task.Repo)
	}
	// A bare edge, with no reason.
	if want := []Dep{{Type: "blocked-by", ID: "cache-core-t4"}}; !reflect.DeepEqual(task.Deps, want) {
		t.Errorf("deps = %+v, want %+v", task.Deps, want)
	}
	if got := taskByID(t, b, "keeps-gadget-h7").Kind; got != "secondmate" {
		t.Errorf("kind of the persistent entry = %q, want secondmate", got)
	}
}

func TestParseReadsLocalPlusRepo(t *testing.T) {
	b := mustParse(t, readFixture(t, "backlog.md"))
	if got := taskByID(t, b, "promote-check").Repo; got != "widget" {
		t.Errorf("repo = %q, want widget", got)
	}
}

func TestParseCapturesOnlyTheTrailingDate(t *testing.T) {
	task := taskByID(t, mustParse(t, readFixture(t, "backlog.md")), "survey-scout-d4")
	if task.Closed != "2026-10-02" {
		t.Errorf("closed = %q, want 2026-10-02", task.Closed)
	}
	if task.Kind != "scout" {
		t.Errorf("kind = %q, want scout", task.Kind)
	}
	// The mid-sentence "(reported 2026-10-02)" stays in the prose title.
	if !strings.Contains(task.Title, "report.md (reported 2026-10-02):") {
		t.Errorf("title %q lost its mid-sentence tag", task.Title)
	}
}

func TestParseExtractsPriority(t *testing.T) {
	b := mustParse(t, "# Backlog\n\n## Queued\n- [ ] prio-q1 - important work (priority: 2) (since 2026-07-01)\n")
	task := b.Tasks()[0]
	if task.Title != "important work" || task.Priority == nil || *task.Priority != 2 || task.Created != "2026-07-01" {
		t.Fatalf("task = %+v", task)
	}
}

func TestParseExtractsHold(t *testing.T) {
	b := mustParse(t, "# Backlog\n\n## Queued\n- [ ] held-q1 - wait for launch (repo: app) (hold: load clears) (hold-kind: load) (hold-until: 2999-01-01)\n")
	task := b.Tasks()[0]
	if task.Title != "wait for launch" || task.Repo != "app" {
		t.Errorf("title %q, repo %q", task.Title, task.Repo)
	}
	if want := (&Hold{Reason: "load clears", Kind: "load", Until: "2999-01-01"}); !reflect.DeepEqual(task.Hold, want) {
		t.Errorf("hold = %+v, want %+v", task.Hold, want)
	}
}

func TestParseReadsContinuationLines(t *testing.T) {
	body := taskByID(t, mustParse(t, readFixture(t, "backlog.md")), "notes-multi-w8").Body
	for _, want := range []string{"Follow-up note added later", "Second continuation line"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q misses %q", body, want)
		}
	}
}

func TestParseKeepsInternalBlanks(t *testing.T) {
	src := strings.Join([]string{
		"# Backlog",
		"",
		"## Queued",
		"- [ ] multi-para - multi-paragraph body (repo: alpha)",
		"  First paragraph line.",
		"",
		"  Second paragraph after a blank.",
		"  ## Intent",
		"",
		"  Indented heading then blank then more.",
		"  final line",
		"- [ ] after-multi - subsequent item (repo: alpha)",
		"  after body",
		"",
		"## Done",
		"",
	}, "\n")
	b := assertRoundTrip(t, src)
	wantBody := strings.Join([]string{
		"First paragraph line.",
		"",
		"Second paragraph after a blank.",
		"## Intent",
		"",
		"Indented heading then blank then more.",
		"final line",
	}, "\n")
	if got := taskByID(t, b, "multi-para").Body; got != wantBody {
		t.Errorf("body = %q, want %q", got, wantBody)
	}
	if got := taskByID(t, b, "after-multi").Body; got != "after body" {
		t.Errorf("body = %q, want %q", got, "after body")
	}
}

func TestParseTreatsWhitespaceAsBlank(t *testing.T) {
	src := strings.Join([]string{
		"## Queued",
		"- [ ] whitespace-body - preserves paragraphs (repo: alpha)",
		"  First paragraph.",
		" \t ",
		"  Second paragraph.",
		"   ",
		"## Done",
		"",
	}, "\n")
	b := assertRoundTrip(t, src)
	if got := b.Tasks()[0].Body; got != "First paragraph.\n\nSecond paragraph." {
		t.Errorf("body = %q", got)
	}
	markAllDirty(b)
	want := strings.Join([]string{
		"## Queued",
		"- [ ] whitespace-body - preserves paragraphs (repo: alpha)",
		"  First paragraph.",
		"",
		"  Second paragraph.",
		"## Done",
		"",
	}, "\n")
	if got := render(b); got != want {
		t.Errorf("dirty rendering\n got %q\nwant %q", got, want)
	}
}

func TestParseIndentedHeadingsAreBody(t *testing.T) {
	src := strings.Join([]string{
		"## Queued",
		"- [ ] intent-trap - harness default work (repo: widget)",
		"  Context for the supervisor.",
		"  ## Intent",
		"  Deliver the full spec, not the title alone.",
		"  ## Acceptance",
		"  - body survives parse",
		"- [ ] next-item - after the trap (repo: widget)",
		"",
		"## Done",
		"",
	}, "\n")
	b := assertRoundTrip(t, src)
	var headers []string
	for _, s := range b.Sections {
		headers = append(headers, s.HeaderLine)
	}
	if want := []string{"## Queued", "## Done"}; !reflect.DeepEqual(headers, want) {
		t.Errorf("headers = %v, want %v", headers, want)
	}
	body := taskByID(t, b, "intent-trap").Body
	for _, want := range []string{"## Intent", "## Acceptance", "Deliver the full spec, not the title alone."} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q misses %q", body, want)
		}
	}
}

func TestParseKeepsTrailingBlankRaw(t *testing.T) {
	src := strings.Join([]string{
		"## Queued",
		"- [ ] section-tail - body ends at section (repo: alpha)",
		"  last queued body",
		"  ## Intent",
		"  still body until column-0 section",
		"",
		"## Done",
		"- [x] old-task - shipped (merged 2026-07-01)",
		"",
	}, "\n")
	b := assertRoundTrip(t, src)
	var entry *Entry
	for _, e := range b.Sections[0].Entries {
		if e.Task != nil && e.Task.ID == "section-tail" {
			entry = e
		}
	}
	if entry == nil {
		t.Fatal("no entry section-tail")
	}
	// Block membership (Lines) includes the trailing blank before ## Done.
	if entry.Lines[len(entry.Lines)-1] != "" || !contains(entry.Lines, "  ## Intent") {
		t.Errorf("lines = %q", entry.Lines)
	}
	// The structured body keeps internal content; the separator is not body.
	if want := "last queued body\n## Intent\nstill body until column-0 section"; entry.Task.Body != want {
		t.Errorf("body = %q, want %q", entry.Task.Body, want)
	}
}

func TestParseSingleLineHasNoBody(t *testing.T) {
	src := "## Queued\n- [ ] plain-q1 - just a title (repo: alpha)\n\n## Done\n"
	b := assertRoundTrip(t, src)
	task := b.Tasks()[0]
	if task.ID != "plain-q1" || task.Body != "" {
		t.Errorf("task = %+v", task)
	}
	// The trailing blank before ## Done stays in the item block, not as body.
	if want := []string{"- [ ] plain-q1 - just a title (repo: alpha)", ""}; !reflect.DeepEqual(b.Sections[0].Entries[0].Lines, want) {
		t.Errorf("lines = %q, want %q", b.Sections[0].Entries[0].Lines, want)
	}
}

func TestParseForgejoDoneBullet(t *testing.T) {
	src := "## Queued\n\n## Done\n- [x] fj-done-q1 - merged https://forge.example.org/eve/widget/pulls/39 (merged 2026-08-07)\n"
	b := assertRoundTrip(t, src)
	want := []item.Link{{Kind: item.LinkPR, URL: "https://forge.example.org/eve/widget/pulls/39"}}
	if got := b.Tasks()[0].Links; !reflect.DeepEqual(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
}

// ---- canonical render ----

func TestRenderOrdersTags(t *testing.T) {
	p := 2
	task := &Task{
		ID: "x-q1", Title: "do a thing", State: StateQueued, Kind: "ship", Repo: "acme",
		Priority: &p, Deps: []Dep{{Type: "blocked-by", ID: "y-t1"}}, Created: "2026-07-01",
	}
	want := "do a thing blocked-by: y-t1 (repo: acme) (kind: ship) (priority: 2) (since 2026-07-01)"
	if got := BuildProse(task); got != want {
		t.Errorf("prose = %q, want %q", got, want)
	}
}

func TestRenderOmitsKindTagForLeadingWord(t *testing.T) {
	task := &Task{ID: "x-q1", Title: "SHIP a thing", State: StateQueued, Kind: "ship"}
	if got := BuildProse(task); got != "SHIP a thing" {
		t.Errorf("prose = %q", got)
	}
}

func TestRenderDoneUsesClosureVerb(t *testing.T) {
	task := &Task{
		ID: "x-q1", Title: "shipped it https://github.com/o/r/pull/9", State: StateDone,
		Links:  []item.Link{{Kind: item.LinkPR, URL: "https://github.com/o/r/pull/9"}},
		Closed: "2026-07-01",
	}
	want := []string{"- [x] x-q1 - shipped it https://github.com/o/r/pull/9 (merged 2026-07-01)"}
	if got := RenderTaskLines(task); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	// The verb follows the links: a report is "reported", neither is "done".
	task.Links = []item.Link{{Kind: item.LinkReport, URL: "data/x/report.md"}}
	if got := BuildProse(task); !strings.HasSuffix(got, "(reported 2026-07-01)") {
		t.Errorf("prose = %q", got)
	}
	task.Links = nil
	if got := BuildProse(task); !strings.HasSuffix(got, "(done 2026-07-01)") {
		t.Errorf("prose = %q", got)
	}
}

func TestRenderBodyAsContinuationLines(t *testing.T) {
	task := &Task{ID: "x-q1", Title: "title", State: StateQueued, Body: "line one\nline two"}
	want := []string{"- [ ] x-q1 - title", "  line one", "  line two"}
	if got := RenderTaskLines(task); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestRenderBlankBodyLines(t *testing.T) {
	task := &Task{ID: "x-q1", Title: "title", State: StateQueued, Body: "line one\n\nline two\n## Intent\n\nfinal"}
	want := []string{"- [ ] x-q1 - title", "  line one", "", "  line two", "  ## Intent", "", "  final"}
	if got := RenderTaskLines(task); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestDirtyRenderKeepsParagraphs(t *testing.T) {
	// No trailing blank after the last body line: a dirty re-render rebuilds
	// from the structured body, and the section separators live only in Lines.
	src := strings.Join([]string{
		"## Queued",
		"- [ ] multi-para - multi-paragraph body (repo: alpha)",
		"  First paragraph line.",
		"",
		"  Second paragraph after a blank.",
		"  ## Intent",
		"",
		"  final line",
		"## Done",
		"",
	}, "\n")
	b := mustParse(t, src)
	markAllDirty(b)
	if got := render(b); got != src {
		t.Errorf("dirty rendering\n got %q\nwant %q", got, src)
	}
}

func TestRenderHold(t *testing.T) {
	task := &Task{
		ID: "held-q1", Title: "wait for launch", State: StateQueued,
		Hold: &Hold{Reason: "load clears", Kind: "load", Until: "2999-01-01"},
	}
	want := []string{"- [ ] held-q1 - wait for launch (hold: load clears) (hold-kind: load) (hold-until: 2999-01-01)"}
	if got := RenderTaskLines(task); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestNormalizationIsIdempotent(t *testing.T) {
	b1 := mustParse(t, readFixture(t, "backlog.md"))
	markAllDirty(b1)
	once := render(b1)
	b2 := mustParse(t, once)
	markAllDirty(b2)
	if twice := render(b2); twice != once {
		t.Errorf("normalizing twice differs from once\n once %q\ntwice %q", once, twice)
	}
}

// ---- the other forms the legacy file has ----

func TestInteropFixtureRoundTrips(t *testing.T) {
	assertRoundTrip(t, readFixture(t, "checkbox-in-flight.md"))
}

func TestInteropCheckboxInFlight(t *testing.T) {
	task := taskByID(t, mustParse(t, readFixture(t, "checkbox-in-flight.md")), "fix-signin-k3")
	if task.State != StateInFlight {
		t.Errorf("state = %q, want in_flight", task.State)
	}
}

func TestInteropBoldInFlight(t *testing.T) {
	task := taskByID(t, mustParse(t, readFixture(t, "backlog.md")), "keeps-gadget-h7")
	if task.State != StateInFlight {
		t.Errorf("state = %q, want in_flight", task.State)
	}
}

func TestInteropNormalizesToCheckbox(t *testing.T) {
	b := mustParse(t, readFixture(t, "backlog.md")) // bold in-flight entries
	markAllDirty(b)
	out := render(b)
	if !regexp.MustCompile(`(?s)## In flight.*- \[ \] keeps-gadget-h7`).MatchString(out) {
		t.Errorf("the in-flight entry is not a checkbox:\n%s", out)
	}
	if strings.Contains(out, "**keeps-gadget-h7**") {
		t.Errorf("the bold form is still there:\n%s", out)
	}
}

func TestInteropParsesDepReason(t *testing.T) {
	task := taskByID(t, mustParse(t, readFixture(t, "checkbox-in-flight.md")), "add-checks-q7")
	if task.Title != "one line" {
		t.Errorf("title = %q", task.Title)
	}
	want := []Dep{{Type: "blocked-by", ID: "fix-signin-k3", Reason: "waits on the sign-in refactor"}}
	if !reflect.DeepEqual(task.Deps, want) {
		t.Errorf("deps = %+v, want %+v", task.Deps, want)
	}
}

func TestInteropRendersDepReason(t *testing.T) {
	task := &Task{
		ID: "add-checks-q7", Title: "one line", State: StateQueued, Repo: "app",
		Deps: []Dep{{Type: "blocked-by", ID: "fix-signin-k3", Reason: "waits on the sign-in refactor"}},
	}
	// An edge with a reason renders last, after the parentheticals.
	want := "one line (repo: app) blocked-by: fix-signin-k3 - waits on the sign-in refactor"
	if got := BuildProse(task); got != want {
		t.Errorf("prose = %q, want %q", got, want)
	}
}

func TestInteropReasonSurvivesNormalize(t *testing.T) {
	b := mustParse(t, readFixture(t, "checkbox-in-flight.md"))
	markAllDirty(b)
	once := render(b)
	if !strings.Contains(once, "blocked-by: fix-signin-k3 - waits on the sign-in refactor") {
		t.Fatalf("the reason is gone:\n%s", once)
	}
	want := Dep{Type: "blocked-by", ID: "fix-signin-k3", Reason: "waits on the sign-in refactor"}
	if got := taskByID(t, mustParse(t, once), "add-checks-q7").Deps[0]; got != want {
		t.Errorf("dep = %+v, want %+v", got, want)
	}
}

func TestInteropMultipleReasons(t *testing.T) {
	src := strings.Join([]string{
		"# Backlog",
		"",
		"## In flight",
		"- [ ] blocker-b - second blocker",
		"",
		"## Queued",
		"- [ ] target-q1 - work (repo: app) blocked-by: blocker-a - first blocker done blocked-by: blocker-b - waits on second blocker",
		"",
		"## Done",
		"- [x] blocker-a - first blocker done",
		"",
	}, "\n")
	b := mustParse(t, src)
	task := taskByID(t, b, "target-q1")
	if task.Title != "work" {
		t.Errorf("title = %q, want work", task.Title)
	}
	want := []Dep{
		{Type: "blocked-by", ID: "blocker-a", Reason: "first blocker done"},
		{Type: "blocked-by", ID: "blocker-b", Reason: "waits on second blocker"},
	}
	if !reflect.DeepEqual(task.Deps, want) {
		t.Fatalf("deps = %+v, want %+v", task.Deps, want)
	}
	markAllDirty(b)
	once := render(b)
	normalized := mustParse(t, once)
	if got := taskByID(t, normalized, "target-q1").Deps; !reflect.DeepEqual(got, want) {
		t.Errorf("deps after a normalize = %+v, want %+v", got, want)
	}
	markAllDirty(normalized)
	if again := render(normalized); again != once {
		t.Errorf("second normalize differs\n once %q\nagain %q", once, again)
	}
}

func TestInteropNoLinksFromReason(t *testing.T) {
	src := "# Backlog\n\n## Queued\n- [ ] target-q1 - work blocked-by: blocker-a - see https://example.com/doc\n"
	task := taskByID(t, mustParse(t, src), "target-q1")
	if task.Title != "work" || len(task.Links) != 0 {
		t.Errorf("title %q, links %v", task.Title, task.Links)
	}
	want := []Dep{{Type: "blocked-by", ID: "blocker-a", Reason: "see https://example.com/doc"}}
	if !reflect.DeepEqual(task.Deps, want) {
		t.Errorf("deps = %+v, want %+v", task.Deps, want)
	}
}

func TestLeadingKind(t *testing.T) {
	for title, want := range map[string]string{
		"SHIP a thing":                  "ship",
		"SCOUT - report":                "scout",
		"DOCS-ONLY change":              "docs",
		"PERSISTENT SECONDMATE owns it": "secondmate",
		"just prose":                    "",
		// A word boundary ends the word.
		"SHIPPING it":  "",
		"SHIP_IT":      "",
		"SHIP":         "ship",
		"SHIP.":        "ship",
		"ship a thing": "",
	} {
		if got := LeadingKind(title); got != want {
			t.Errorf("LeadingKind(%q) = %q, want %q", title, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Hive's own cases
// ---------------------------------------------------------------------------

// A file with every tag the legacy file has, a header suffix, an unrecognized
// section and a recognized header with trailing white space.
func TestEveryTagFixture(t *testing.T) {
	src := readFixture(t, "every-tag.md")
	b := assertRoundTrip(t, src)
	if !regexp.MustCompile(`## Queued  \n`).MatchString(src) {
		t.Fatal("the fixture must keep the white space after a header")
	}
	var states []State
	for _, s := range b.Sections {
		states = append(states, s.State)
	}
	if want := []State{StateInFlight, StateQueued, "", StateDone}; !reflect.DeepEqual(states, want) {
		t.Fatalf("section states = %q, want %q", states, want)
	}
	// An unrecognized section passes every line through: no task in it.
	for _, e := range b.Sections[2].Entries {
		if e.Task != nil {
			t.Errorf("the unrecognized section holds the task %q", e.Task.ID)
		}
	}

	b2 := taskByID(t, b, "tagged-b2")
	p := 4
	wantB2 := Task{
		ID: "tagged-b2", State: StateQueued, Kind: "docs", Repo: "app", Priority: &p, Created: "2026-10-03",
		Title: "define the gauge schema https://github.com/acme/app/pull/77 data/tagged-b2/report.md https://example.org/spec",
		Body:  "Body line one.\n\nBody line after a blank.",
		Hold:  &Hold{Reason: "waiting for the review slot", Kind: "parked", Until: "2026-12-01"},
		Links: []item.Link{
			{Kind: item.LinkPR, URL: "https://github.com/acme/app/pull/77"},
			{Kind: item.LinkReport, URL: "data/tagged-b2/report.md"},
			{Kind: item.LinkDoc, URL: "https://example.org/spec"},
		},
	}
	if !reflect.DeepEqual(*b2, wantB2) {
		t.Errorf("tagged-b2 = %+v\nwant %+v", *b2, wantB2)
	}

	wantC3 := []Dep{
		{Type: "blocked-by", ID: "tagged-b2"},
		{Type: "parent", ID: "tagged-a1", Reason: "the umbrella"},
		{Type: "discovered-from", ID: "tagged-d4", Reason: "found while testing"},
	}
	if got := taskByID(t, b, "tagged-c3").Deps; !reflect.DeepEqual(got, wantC3) {
		t.Errorf("tagged-c3 deps = %+v, want %+v", got, wantC3)
	}
	// A captain hold keeps the legacy word; the import maps it.
	if got := taskByID(t, b, "tagged-e5").Hold; !reflect.DeepEqual(got, &Hold{Reason: "awaiting the word", Kind: "captain", Until: "2026-11-01"}) {
		t.Errorf("tagged-e5 hold = %+v", got)
	}
	for id, want := range map[string]string{"tagged-d4": "2026-10-04", "tagged-f6": "2026-10-04", "tagged-g7": "", "tagged-h8": "2026-10-05"} {
		if got := taskByID(t, b, id).Closed; got != want {
			t.Errorf("%s closed = %q, want %q", id, got, want)
		}
	}
	// The same file normalized parses back to the same fields.
	norm := mustParse(t, src)
	markAllDirty(norm)
	again := mustParse(t, render(norm))
	for _, task := range b.Tasks() {
		if got := taskByID(t, again, task.ID); !reflect.DeepEqual(got, task) {
			t.Errorf("%s changed by a normalize\n got %+v\nwant %+v", task.ID, got, task)
		}
	}
}

// Whatever the bytes, nothing is lost by parse and render: a lone carriage
// return, a carriage return inside a line, white space the grammar counts,
// bytes that are not UTF-8, a header with nothing after it, a file of line
// ends only.
func TestRoundTripKeepsAnyBytes(t *testing.T) {
	for name, src := range map[string]string{
		"lone line feed":          "\n",
		"line ends only":          "\n\n\r\n\n",
		"carriage return only":    "\r",
		"old mac line ends":       "# Backlog\r## Queued\r- [ ] a - b\r",
		"mixed line ends":         "## Queued\r\n- [ ] a - b\n  body\r\n\n## Done\r\n- [x] c - d (merged 2026-01-01)",
		"cr inside a line":        "## Queued\n- [ ] a - b\rc (repo: x)\n",
		"bare header":             "##\n## \n##\t\n",
		"not utf-8":               "## Queued\n- [ ] a - \xff\xfe broken \xc3 (repo: \x80)\n  \xf0 body\n",
		"byte order mark":         "\xef\xbb\xbf# Backlog\n## Queued\n- [ ] a - b\n",
		"unicode space in a tag":  "## Queued\n- [ ] a - b (repo: x) \n",
		"line separator":          "## Queued\n- [ ] a - b c\n- [ ] d - e (repo: y )\n",
		"entry before any header": "- [ ] a - b\n## Queued\n",
		"bold outside in flight":  "## Queued\n- **a** - b\n## Done\n- [ ] c - d\n",
		"trailing spaces":         "## Queued   \n- [ ] a - b   \n   \n",
	} {
		t.Run(name, func(t *testing.T) {
			b := assertRoundTrip(t, src)
			// And a normalize never fails or loses an identifier.
			var before []string
			before = append(before, ids(b)...)
			markAllDirty(b)
			if after := ids(mustParse(t, render(b))); !reflect.DeepEqual(before, after) && len(before) != 0 {
				t.Errorf("identifiers %v became %v", before, after)
			}
		})
	}
}

func TestSectionHeaders(t *testing.T) {
	for header, want := range map[string]State{
		"## In flight":             StateInFlight,
		"## in flight":             StateInFlight,
		"##  IN FLIGHT  ":          StateInFlight,
		"## Queued":                StateQueued,
		"## QUEUED (next up)":      "",
		"## Done":                  StateDone,
		"## Done (10 most recent)": StateDone,
		"##\tdone":                 StateDone,
		"## DONE - kept by hand":   StateDone,
		"## Doneish":               StateDone,
		"## Later":                 "",
		"## In  flight":            "",
		"## İn flight":             "",
		"##":                       "",
	} {
		b := mustParse(t, header+"\n")
		var got State
		if len(b.Sections) == 1 {
			got = b.Sections[0].State
		} else if header != "##" {
			t.Errorf("%q is not a header", header)
		}
		if got != want {
			t.Errorf("state of %q = %q, want %q", header, got, want)
		}
	}
	// A header with a carriage return is the same header.
	if got := mustParse(t, "## Queued\r\n").Sections[0].State; got != StateQueued {
		t.Errorf("state of a CRLF header = %q", got)
	}
}

func TestBulletFormsBySection(t *testing.T) {
	b := mustParse(t, strings.Join([]string{
		"## In flight",
		"- **bold-a** - one",
		"- [ ] box-b - two",
		"- [x] ticked-c - not here",
		"## Queued",
		"- **bold-d** - not here",
		"- [ ] box-e - three",
		"## Done",
		"- [ ] box-f - not here",
		"- [x] ticked-g - four",
		"- [X] big-x-h - not here",
		"-  [ ] spaced-i - not here",
		"- [ ] no-dash-j  not here",
	}, "\n"))
	if want := []string{"bold-a", "box-b", "box-e", "ticked-g"}; !reflect.DeepEqual(ids(b), want) {
		t.Errorf("ids = %v, want %v", ids(b), want)
	}
}

// The tail patterns have edges that are easy to move: they are anchored at the
// end of the prose only, a marker inside a word still counts, and white space
// around a reason is not part of it.
func TestTailEdges(t *testing.T) {
	cases := []struct {
		rest  string
		title string
		deps  []Dep
	}{
		{"work blocked-by: a-1", "work", []Dep{{"blocked-by", "a-1", ""}}},
		{"work   blocked-by:   a-1   ", "work", []Dep{{"blocked-by", "a-1", ""}}},
		{"work blocked-by: a-1 - because  ", "work", []Dep{{"blocked-by", "a-1", "because"}}},
		{"work blocked-by: a-1 -  spaced out  ", "work", []Dep{{"blocked-by", "a-1", "spaced out"}}},
		{"work blocked-by: a-1 - ", "work blocked-by: a-1 -", nil},
		// With two white space characters after the dash the reason is the
		// first of them, as the upstream's pattern reads it.
		{"work blocked-by: a-1 -  ", "work", []Dep{{"blocked-by", "a-1", " "}}},
		{"foo-parent: bar", "foo-", []Dep{{"parent", "bar", ""}}},
		{"work blocked-by: a b", "work blocked-by: a b", nil},
		{"work blocked-by: a-1 then more", "work blocked-by: a-1 then more", nil},
		{"work blocked-by: parent: x", "work blocked-by:", []Dep{{"parent", "x", ""}}},
		{"work discovered-from: d_3 - found it", "work", []Dep{{"discovered-from", "d_3", "found it"}}},
		{"work blocked-by: a - r1 parent: b - r2", "work", []Dep{{"blocked-by", "a", "r1"}, {"parent", "b", "r2"}}},
		{"work (repo: x) (since 2026-01-02) blocked-by: a", "work", []Dep{{"blocked-by", "a", ""}}},
	}
	for _, c := range cases {
		got := ExtractTags(c.rest)
		if got.Title != c.title || !reflect.DeepEqual(got.Deps, c.deps) {
			t.Errorf("ExtractTags(%q) = title %q deps %+v, want title %q deps %+v", c.rest, got.Title, got.Deps, c.title, c.deps)
		}
	}

	// Tags only count at the end, and the first of a repeated tag wins.
	got := ExtractTags("a (repo: one) b (repo: two) (repo: three)")
	if got.Repo != "three" || got.Title != "a (repo: one) b" {
		t.Errorf("repeated repo: title %q, repo %q", got.Title, got.Repo)
	}
	// Priority is one digit from 0 to 4.
	for rest, want := range map[string]*int{"a (priority: 4)": ptr(4), "a (priority: 0)": ptr(0), "a (priority: 5)": nil, "a (priority: 10)": nil} {
		if p := ExtractTags(rest).Priority; !reflect.DeepEqual(p, want) {
			t.Errorf("priority of %q = %v, want %v", rest, p, want)
		}
	}
	// An unknown hold kind is not a tag and stays in the prose.
	if got := ExtractTags("a (hold: r) (hold-kind: bogus)"); got.Hold != nil || got.Title != "a (hold: r) (hold-kind: bogus)" {
		t.Errorf("unknown hold kind: %+v", got)
	}
	// A blank kind tag is a tag all the same, and blocks the leading word.
	if got := ExtractTags("SHIP a (kind:  )"); got.Kind != "" || got.Title != "SHIP a" {
		t.Errorf("blank kind tag: %+v", got)
	}
	if got := ExtractTags("SHIP a"); got.Kind != "ship" {
		t.Errorf("leading kind: %+v", got)
	}
}

func ptr(n int) *int { return &n }

// Normalizing never changes how an entry reads back, whichever tags it holds.
func TestParseOfCanonicalRenderIsStable(t *testing.T) {
	p := 3
	for _, task := range []*Task{
		{ID: "a-1", Title: "plain", State: StateQueued},
		{ID: "a-2", Title: "SCOUT survey https://example.org/x", State: StateDone, Kind: "scout", Closed: "2026-01-02"},
		{ID: "a-3", Title: "tagged", State: StateInFlight, Repo: "r", Kind: "docs", Priority: &p, Created: "2026-01-01",
			Hold: &Hold{Reason: "why", Kind: "future", Until: "2027-01-01"},
			Deps: []Dep{{Type: "blocked-by", ID: "b"}, {Type: "parent", ID: "c", Reason: "umbrella"}, {Type: "discovered-from", ID: "d", Reason: "while testing"}},
			Body: "one\n\ntwo"},
	} {
		src := strings.Join(RenderTaskLines(task), "\n") + "\n"
		src = map[State]string{StateQueued: "## Queued\n", StateInFlight: "## In flight\n", StateDone: "## Done\n"}[task.State] + src
		got := taskByID(t, mustParse(t, src), task.ID)
		task.Links = got.Links // links are derived from the title
		if !reflect.DeepEqual(got, task) {
			t.Errorf("%s reads back as\n%+v\nwant\n%+v", task.ID, got, task)
		}
	}
}

func TestParseNeverFails(t *testing.T) {
	if _, err := Parse([]byte("\x00\x01 binary \xff")); err != nil {
		t.Fatalf("Parse of arbitrary bytes failed: %v", err)
	}
}
