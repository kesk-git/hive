package legacy

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kesk-git/hive/packages/pool/internal/item"
)

// Translated tests. Upstream: tasks-axi 0.2.6 at revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
//
// test/commands/maintain.test.ts, describe "maintenance commands", the cases
// that exercise archive lookup and normalization. The command line of the
// upstream, its flags, its output and its configuration are not carried, and
// Hive prunes nothing, so a prune is not performed here: the test writes the
// archive block as the upstream's prune appends it (pruneBlock) and reads that
// back, which is the part Hive keeps.
//
//   - "prune trims Done to the keep count and archives the surplus": TestArchiveHoldsThePrunedSurplus.
//     The surplus of a Done section beyond the entries kept is archived under
//     an `## Archived` header.
//   - "prune emits a machine-readable result with --json": the same test; the
//     ids and the count that result carries are the entries of the block.
//   - "prune rejects unknown flags before pruning" (the archive stays empty):
//     TestEmptyArchiveHoldsNothing.
//   - "render normalizes the file and reports the count" and "render emits a
//     machine-readable result with --json": TestNormalizeCountsTheTasks. The
//     count is the number of tasks with an identifier; free-form lines are
//     left untouched.
//
// Not carried, because they test the command line: "prune rejects a negative
// keep", "prune rejects a negative configured keep", "prune rejects an invalid
// state", "render rejects extra arguments before rendering".

// pruneBlock is the block the upstream's prune appends to an archive file: a
// blank line, the header with the date, the surplus entries' exact lines and a
// final line end.
func pruneBlock(stamp string, entries []*Entry) string {
	var lines []string
	for _, e := range entries {
		lines = append(lines, e.Lines...)
	}
	return "\n## Archived " + stamp + "\n" + strings.Join(lines, "\n") + "\n"
}

func TestArchiveHoldsThePrunedSurplus(t *testing.T) {
	b := mustParse(t, readFixture(t, "backlog.md"))
	var done *Section
	for _, s := range b.Sections {
		if s.State == StateDone {
			done = s
		}
	}
	var tasks []*Entry
	for _, e := range done.Entries {
		if e.Task != nil {
			tasks = append(tasks, e)
		}
	}
	const keep = 2
	surplus := tasks[keep:]
	if len(surplus) == 0 {
		t.Fatal("the fixture's Done section must hold more than the entries kept")
	}

	text := pruneBlock("2026-10-07", surplus)
	if !strings.Contains(text, "## Archived") {
		t.Fatal("the archive block has no `## Archived` header")
	}
	a, err := ParseArchive([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Render(); !bytes.Equal(got, []byte(text)) {
		t.Fatalf("the archive does not render as it was read\n got %q\nwant %q", got, text)
	}
	if len(a.Backlog.Sections) != 1 || a.Backlog.Sections[0].Stamp() != "2026-10-07" {
		t.Fatalf("archive blocks = %+v, want one stamped 2026-10-07", a.Backlog.Sections)
	}
	// The ids the prune reported are the ones that resolve, each with the lines
	// it was archived with; the entries kept are not in the archive.
	for _, e := range surplus {
		got, ok := a.Find(e.Task.ID)
		if !ok {
			t.Fatalf("pruned %q does not resolve", e.Task.ID)
		}
		if strings.Join(got.Lines, "\n") != strings.Join(e.Lines, "\n") {
			t.Errorf("%q archived as %q, want %q", e.Task.ID, got.Lines, e.Lines)
		}
		if got.Task.State != StateDone || got.Task.Title != e.Task.Title || got.Stamp != "2026-10-07" {
			t.Errorf("%q read back as %+v stamped %q", e.Task.ID, got.Task, got.Stamp)
		}
	}
	for _, e := range tasks[:keep] {
		if _, ok := a.Find(e.Task.ID); ok {
			t.Errorf("%q was kept and must not be in the archive", e.Task.ID)
		}
	}
}

func TestEmptyArchiveHoldsNothing(t *testing.T) {
	a, err := ParseArchive(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Render()) != 0 {
		t.Fatalf("an empty archive renders %q", a.Render())
	}
	if _, ok := a.Find("anything"); ok {
		t.Fatal("an empty archive resolved an identifier")
	}
}

func TestNormalizeCountsTheTasks(t *testing.T) {
	src := readFixture(t, "backlog.md")
	b := mustParse(t, src)
	want := len(b.Tasks())
	if want == 0 {
		t.Fatal("the fixture holds no task")
	}
	if got := b.Normalize(); got != want {
		t.Fatalf("Normalize() = %d, want %d", got, want)
	}
	// Free-form lines come out as they were: every line of the source that is
	// not part of a task is still in the rendering.
	out := string(b.Render())
	for _, s := range mustParse(t, src).Sections {
		for _, e := range s.Entries {
			if e.Task != nil {
				continue
			}
			for _, l := range e.Lines {
				if l != "" && !strings.Contains(out, l+"\n") {
					t.Errorf("free-form line %q is not in the normalized file", l)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Beyond the upstream's cases: how Hive reads the archive files.
// ---------------------------------------------------------------------------

// TestArchiveFixtureReadsEveryBulletForm holds the reading of an archived
// entry's state: the legacy form is silent on it and an archive has no
// section, so it is read from the entry's own bullet, as the upstream wrote it,
// and no entry is taken to be done for sitting in an archive.
func TestArchiveFixtureReadsEveryBulletForm(t *testing.T) {
	src := readFixture(t, "done-archive.md")
	a, err := ParseArchive([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(a.Render()); got != src {
		t.Fatalf("the archive does not render as it was read\n got %q\nwant %q", got, src)
	}
	var stamps []string
	for _, s := range a.Backlog.Sections {
		stamps = append(stamps, s.Stamp())
		if s.State != "" {
			t.Errorf("block %q has state %q, want none", s.HeaderLine, s.State)
		}
	}
	if strings.Join(stamps, ",") != "2026-10-06,2026-10-09" {
		t.Fatalf("stamps = %v", stamps)
	}
	for id, want := range map[string]State{
		"scouted-old-b2": StateDone,
		"bare-old-c3":    StateDone,
		"flight-old-d4":  StateInFlight,
		"queued-old-e5":  StateQueued,
	} {
		got, ok := a.Find(id)
		if !ok || got.Task.State != want {
			t.Errorf("Find(%q) = %+v, %v; want state %q", id, got.Task, ok, want)
		}
	}
	b, _ := a.Find("scouted-old-b2")
	if b.Task.Body != "Continuation line kept with its entry. (reported 2026-09-02)" || b.Task.Kind != "scout" {
		t.Errorf("scouted-old-b2 read as body %q, kind %q", b.Task.Body, b.Task.Kind)
	}
	c, _ := a.Find("bare-old-c3")
	if c.Task.Closed != "" {
		t.Errorf("bare-old-c3 has closing date %q, want none", c.Task.Closed)
	}
}

func TestArchiveFindsTheLatestBlock(t *testing.T) {
	a, err := ParseArchive([]byte(readFixture(t, "done-archive.md")))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := a.Find("shipped-old-a1")
	if !ok || got.Stamp != "2026-10-09" || got.Task.Closed != "2026-10-08" {
		t.Fatalf("Find(shipped-old-a1) = %+v stamped %q, %v; want the block of 2026-10-09", got.Task, got.Stamp, ok)
	}
}

func TestLookupPrefersTheLiveFileThenTheArchives(t *testing.T) {
	live := mustParse(t, readFixture(t, "backlog.md"))
	archive, _ := ParseArchive([]byte(readFixture(t, "done-archive.md")))
	other, _ := ParseArchive([]byte("\n## Archived 2026-10-01\n- [x] only-in-other - an older prune (merged 2026-09-30)\n"))

	if f, ok := Lookup("cache-core-t4", live, archive); !ok || f.Archived {
		t.Fatalf("a live task resolved as %+v, %v", f, ok)
	}
	f, ok := Lookup("shipped-old-a1", live, archive)
	if !ok || !f.Archived || f.Stamp != "2026-10-09" {
		t.Fatalf("a pruned task resolved as %+v, %v", f, ok)
	}
	if f, ok := Lookup("only-in-other", live, archive, other); !ok || !f.Archived || f.Task.Closed != "2026-09-30" {
		t.Fatalf("a task of the second archive resolved as %+v, %v", f, ok)
	}
	if _, ok := Lookup("never-existed", live, archive, other, nil); ok {
		t.Fatal("an unknown identifier resolved")
	}
	// A task pruned upstream still resolves with no live file at all.
	if _, ok := Lookup("bare-old-c3", nil, archive); !ok {
		t.Fatal("a pruned task did not resolve without a live file")
	}
}

func TestArchiveKeepsCRLFAndNoFinalNewline(t *testing.T) {
	src := strings.ReplaceAll(readFixture(t, "done-archive.md"), "\n", "\r\n")
	src = strings.TrimSuffix(src, "\r\n")
	a, err := ParseArchive([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(a.Render()); got != src {
		t.Fatalf("the archive does not render as it was read\n got %q\nwant %q", got, src)
	}
	if _, ok := a.Find("queued-old-e5"); !ok {
		t.Fatal("an entry of a CRLF archive did not resolve")
	}
	if f, _ := a.Find("scouted-old-b2"); strings.Contains(f.Task.Body, "\r") {
		t.Fatalf("body %q keeps a carriage return", f.Task.Body)
	}
}

// ---------------------------------------------------------------------------
// Hive's own cases: the normalization an entry gets before it is rendered
// canonically, from the upstream's source and not from any of its tests.
// ---------------------------------------------------------------------------

func wantInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Errorf("%s: error = %v, want a validation error", what, err)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if got, err := NormalizeTitle("  plain title \u00a0"); err != nil || got != "plain title" {
		t.Errorf("NormalizeTitle = %q, %v", got, err)
	}
	for what, title := range map[string]string{
		"two lines":     "a\nb",
		"carriage ret.": "a\rb",
		"line sep.":     "a\u2028b",
		"paragraph sep": "a\u2029b",
		"empty":         "",
		"blank":         " \t ",
		"ends in a tag": "work (repo: app)",
		"ends in a dep": "work blocked-by: other-1",
		"ends in date":  "work (since 2026-01-01)",
		"open repo tag": "review (repo: old",
		"open kind tag": "review (kind: old",
		"open + repo":   "review (old + repo:",
		"open edge":     "work parent: other-1 -",
	} {
		_, err := NormalizeTitle(title)
		wantInvalid(t, what, err)
	}
	// A tag in the middle is prose.
	if got, err := NormalizeTitle("work (repo: app) goes on"); err != nil || got != "work (repo: app) goes on" {
		t.Errorf("NormalizeTitle = %q, %v", got, err)
	}
}

func TestNormalizeTagValue(t *testing.T) {
	if got, err := NormalizeTagValue("  ship ", "kind"); err != nil || got != "ship" {
		t.Errorf("NormalizeTagValue = %q, %v", got, err)
	}
	if got, err := NormalizeTagValue("   ", "repo"); err != nil || got != "" {
		t.Errorf("a blank value = %q, %v; want none", got, err)
	}
	for _, v := range []string{"a(b", "a)b", "a\nb", "a\u2028b", "a\u2029b"} {
		_, err := NormalizeTagValue(v, "repo")
		wantInvalid(t, v, err)
	}
	// A value that a re-parse would read as an edge or as another tag.
	for _, field := range []string{"kind", "repo"} {
		for _, v := range []string{"blocked-by: b - waiting", "x parent: b - y", "x discovered-from:b - y", "a + repo: b"} {
			_, err := NormalizeTagValue(v, field)
			wantInvalid(t, field+" "+v, err)
		}
	}
	// A marker that starts no edge is plain text.
	if got, err := NormalizeTagValue("after blocked-by: b", "repo"); err != nil || got != "after blocked-by: b" {
		t.Errorf("NormalizeTagValue = %q, %v", got, err)
	}
}

func TestNormalizeTypedLink(t *testing.T) {
	good := []item.Link{
		{Kind: item.LinkPR, URL: "https://github.com/o/r/pull/9"},
		{Kind: item.LinkReport, URL: "data/x/report.md"},
		{Kind: item.LinkDoc, URL: "https://example.org/a"},
	}
	for _, l := range good {
		if got, err := NormalizeTypedLink(l); err != nil || got != l {
			t.Errorf("NormalizeTypedLink(%v) = %v, %v", l, got, err)
		}
	}
	// Padding is trimmed from a report path and a URL, never from a pull request.
	if got, err := NormalizeTypedLink(item.Link{Kind: item.LinkDoc, URL: "  https://example.org/a "}); err != nil || got.URL != "https://example.org/a" {
		t.Errorf("padded doc link = %v, %v", got, err)
	}
	for what, l := range map[string]item.Link{
		"padded pr":     {Kind: item.LinkPR, URL: " https://github.com/o/r/pull/9"},
		"issue as pr":   {Kind: item.LinkPR, URL: "https://github.com/o/r/issues/9"},
		"url as report": {Kind: item.LinkReport, URL: "https://example.org/a"},
		"path as doc":   {Kind: item.LinkDoc, URL: "data/x/report.md"},
		"empty":         {Kind: item.LinkDoc, URL: " "},
		"two lines":     {Kind: item.LinkDoc, URL: "https://example.org/a\nb"},
		"pr as a doc":   {Kind: item.LinkDoc, URL: "https://github.com/o/r/pull/9"},
	} {
		_, err := NormalizeTypedLink(l)
		wantInvalid(t, what, err)
	}
}

func TestNormalizePriorityDateAndHold(t *testing.T) {
	for _, p := range []int{0, 2, 4} {
		if got, err := NormalizePriority(p); err != nil || got != p {
			t.Errorf("NormalizePriority(%d) = %d, %v", p, got, err)
		}
	}
	for _, p := range []int{-1, 5} {
		_, err := NormalizePriority(p)
		wantInvalid(t, "priority", err)
	}
	if got, err := NormalizeDate("2026-10-14", "created date"); err != nil || got != "2026-10-14" {
		t.Errorf("NormalizeDate = %q, %v", got, err)
	}
	for _, d := range []string{"", "2026-1-14", "26-10-14", "2026-10-14 ", "2026/10/14"} {
		_, err := NormalizeDate(d, "created date")
		wantInvalid(t, d, err)
	}

	h, err := NormalizeHold(Hold{Reason: "  waiting ", Kind: "captain", Until: "2026-12-01"})
	if err != nil || h != (Hold{Reason: "waiting", Kind: "captain", Until: "2026-12-01"}) {
		t.Errorf("NormalizeHold = %+v, %v", h, err)
	}
	for what, h := range map[string]Hold{
		"empty reason": {Reason: "  "},
		"parenthesis":  {Reason: "a (b)"},
		"two lines":    {Reason: "a\nb"},
		"line sep.":    {Reason: "a\u2028b"},
		"edge":         {Reason: "blocked-by: b - waiting"},
		"other tag":    {Reason: "a + repo: b"},
		"unknown kind": {Reason: "a", Kind: "later"},
		"bad date":     {Reason: "a", Until: "tomorrow"},
	} {
		_, err := NormalizeHold(h)
		wantInvalid(t, what, err)
	}
}

// What normalization accepts reads back unchanged from the canonical prose,
// however close each field comes to an edge or a tag.
func TestNormalizedFieldsReadBack(t *testing.T) {
	must := func(v string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	title := must(NormalizeTitle("review (repo: old) after parent: other-1 -x"))
	kind := must(NormalizeTagValue("x parent:y", "kind"))
	repo := must(NormalizeTagValue("after blocked-by: b", "repo"))
	hold, err := NormalizeHold(Hold{Reason: "waiting on blocked-by: b -", Kind: "load"})
	if err != nil {
		t.Fatal(err)
	}
	deps := []Dep{{Type: "parent", ID: "up-1"}, {Type: "blocked-by", ID: "other-2", Reason: "why (repo: no)"}}
	task := &Task{ID: "a", Title: title, State: StateQueued, Kind: kind, Repo: repo, Hold: &hold, Deps: deps}
	prose := BuildProse(task)
	got := ExtractTags(prose)
	if got.Title != title || got.Kind != kind || got.Repo != repo || got.Hold == nil || *got.Hold != hold || !reflect.DeepEqual(got.Deps, deps) {
		t.Errorf("%q reads back as %+v", prose, got)
	}
}

func TestNormalizeDep(t *testing.T) {
	got, err := NormalizeDep("me-1", Dep{Type: "blocked-by", ID: "other-2", Reason: "  because  "})
	if err != nil || got != (Dep{Type: "blocked-by", ID: "other-2", Reason: "because"}) {
		t.Errorf("NormalizeDep = %+v, %v", got, err)
	}
	if got, err := NormalizeDep("me-1", Dep{Type: "parent", ID: "other-2", Reason: "   "}); err != nil || got.Reason != "" {
		t.Errorf("a blank reason = %+v, %v; want none", got, err)
	}
	for what, d := range map[string]Dep{
		"itself":         {Type: "blocked-by", ID: "me-1"},
		"bad id":         {Type: "blocked-by", ID: "not an id"},
		"two lines":      {Type: "blocked-by", ID: "o", Reason: "a\nb"},
		"line sep.":      {Type: "blocked-by", ID: "o", Reason: "a\u2028b"},
		"para. sep.":     {Type: "blocked-by", ID: "o", Reason: "a\u2029b"},
		"unknown type":   {Type: "blocks", ID: "o"},
		"no type":        {ID: "o"},
		"marker":         {Type: "blocked-by", ID: "o", Reason: "x blocked-by: y"},
		"leading marker": {Type: "blocked-by", ID: "o", Reason: "parent: y"},
		"marker at tab":  {Type: "blocked-by", ID: "o", Reason: "x\tdiscovered-from: y"},
	} {
		_, err := NormalizeDep("me-1", d)
		wantInvalid(t, what, err)
	}
	// A marker without the colon and a space is not an edge.
	if _, err := NormalizeDep("me-1", Dep{Type: "blocked-by", ID: "o", Reason: "blocked-by-nothing"}); err != nil {
		t.Errorf("a word that only starts like a marker was refused: %v", err)
	}
}

func TestAppendTitleLink(t *testing.T) {
	pr := item.Link{Kind: item.LinkPR, URL: "https://github.com/o/r/pull/9"}
	got, err := AppendTitleLink("work", pr)
	if err != nil || got != "work https://github.com/o/r/pull/9" {
		t.Fatalf("AppendTitleLink = %q, %v", got, err)
	}
	// A link the title already holds is not added again.
	if again, err := AppendTitleLink(got, pr); err != nil || again != got {
		t.Errorf("AppendTitleLink = %q, %v; want %q", again, err, got)
	}
	_, err = AppendTitleLink("work", item.Link{Kind: item.LinkPR, URL: "https://github.com/o/r/issues/9"})
	wantInvalid(t, "issue as pr", err)
}

func TestEnsureSectionsBuildsAnEmptyBacklog(t *testing.T) {
	b := mustParse(t, "")
	if n := b.Normalize(); n != 0 {
		t.Fatalf("Normalize() = %d on an empty file", n)
	}
	want := "# Backlog\n\n## In flight\n## Queued\n## Done\n"
	if got := render(b); got != want {
		t.Fatalf("rendering = %q, want %q", got, want)
	}
	// Sections that are there stay where they are; the missing ones follow.
	b = mustParse(t, "## Done\n- [x] a-1 - one (merged 2026-01-01)\n")
	b.EnsureSections()
	if got, want := render(b), "## Done\n- [x] a-1 - one (merged 2026-01-01)\n## In flight\n## Queued\n"; got != want {
		t.Fatalf("rendering = %q, want %q", got, want)
	}
}
