package legacy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/legacy"
)

// The acceptance test of the import that writes and of the export from a
// migration (spec/pool.md, Legacy import and export). It drives the executable
// the default registry describes, in process, over a home of its own: the two
// upstream fixtures re-expressed as Hive fixtures, a file with every tag the
// table names and a file with carriage returns, a header suffix and an
// unrecognized section are each imported for real, writing one migration, its
// items, holds and resolved dependencies with their receipts, and never the
// source file; the refusals the section states write nothing; and the export
// from each migration equals its source, until an item is modified.

// imported is a fixture imported for real and what the home then holds.
type imported struct {
	h    *home
	path string
	data []byte
	when time.Time
	resp *envelope.Response
	body map[string]any
	// migration is the migration record as stored.
	migration map[string]any
	// items are the items of the migration by legacy identifier, and holds
	// its holds by the legacy identifier of the item they are on.
	items map[string]map[string]any
	holds map[string]map[string]any
}

// records are the records of the kind under pool/records, by identity.
func (h *home) records(kind string) map[string]map[string]any {
	h.t.Helper()
	dir := filepath.Join(h.dir, "pool", "records", kind)
	names, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n.Name()))
		if err != nil {
			h.t.Fatal(err)
		}
		v, err := canon.Parse(data)
		if err != nil {
			h.t.Fatalf("%s: %v", n.Name(), err)
		}
		out[strings.TrimSuffix(n.Name(), ".json")] = v.(map[string]any)
	}
	return out
}

// liveImport imports the file for real.
func (h *home) liveImport(path, zone string, extra map[string]any) *envelope.Response {
	h.t.Helper()
	body := map[string]any{"path": path, "zone": zone, "dry_run": false}
	for k, v := range extra {
		body[k] = v
	}
	return h.do("import-legacy", body)
}

// importFixture imports the fixture in the zone and reads back what the home
// holds, after checking that the source file was not written.
func (h *home) importFixture(fixture, zone string) *imported {
	h.t.Helper()
	path, data, when := source(h.t, fixture)
	return h.importPath(path, data, when, zone)
}

func (h *home) importPath(path string, data []byte, when time.Time, zone string) *imported {
	h.t.Helper()
	resp := h.liveImport(path, zone, map[string]any{"expected_digest": canon.FileDigest(data)})
	body := ok(h.t, resp)
	unchanged(h.t, path, data, when)
	im := &imported{h: h, path: path, data: data, when: when, resp: resp, body: body, items: map[string]map[string]any{}, holds: map[string]map[string]any{}}
	migrations := h.records("migration")
	id, _ := body["migration"].(map[string]any)["migration_id"].(string)
	if im.migration = migrations[id]; im.migration == nil {
		h.t.Fatalf("the migration %q is not stored; the home holds %d", id, len(migrations))
	}
	byTask := map[string]string{}
	for taskID, rec := range h.records("item") {
		if legacyMember(rec, "migration_id") == id {
			im.items[legacyMember(rec, "legacy_id")] = rec
			byTask[taskID] = legacyMember(rec, "legacy_id")
		}
	}
	for _, rec := range h.records("hold") {
		if legacyID, ok := byTask[rec["task_id"].(string)]; ok {
			im.holds[legacyID] = rec
		}
	}
	return im
}

func legacyMember(rec map[string]any, key string) string {
	m, _ := rec["legacy"].(map[string]any)
	s, _ := m[key].(string)
	return s
}

func (im *imported) taskID(legacyID string) string {
	im.h.t.Helper()
	rec := im.items[legacyID]
	if rec == nil {
		im.h.t.Fatalf("no item was made of %s; the import made %v", legacyID, mapKeys(im.items))
	}
	return rec["task_id"].(string)
}

func mapKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// entries are the migration's entries by legacy identifier; a repeated
// identifier keeps its first.
func (im *imported) entry(legacyID string) map[string]any {
	im.h.t.Helper()
	for _, v := range im.migration["entries"].([]any) {
		if e := v.(map[string]any); e["legacy_id"] == legacyID {
			return e
		}
	}
	im.h.t.Fatalf("the migration holds no entry %s", legacyID)
	return nil
}

// receiptKinds counts the receipts of the response by kind.
func (im *imported) receiptKinds() map[string]int {
	out := map[string]int{}
	for _, r := range im.resp.Receipts {
		out[r.Kind]++
	}
	return out
}

// checkWritten holds the import to what the section says it writes: one
// migration holding the source text and every entry's exact lines, one item
// per entry that is an item with its legacy identifier kept and no level and
// no parent, a hold for each entry with hold tags, and the receipts of each
// record.
func (im *imported) checkWritten(wantItems, wantHolds int) {
	t := im.h.t
	t.Helper()
	if n := len(im.h.records("migration")); n != 1 {
		t.Errorf("the home holds %d migrations", n)
	}
	src, _ := im.migration["source"].(map[string]any)
	if src["text"] != string(im.data) || src["digest"] != canon.FileDigest(im.data) || src["bytes"] != int64(len(im.data)) || src["path"] != im.path {
		t.Errorf("the migration's source is %v", src)
	}
	if len(im.items) != wantItems || len(im.holds) != wantHolds {
		t.Errorf("the import made %d items and %d holds; want %d and %d", len(im.items), len(im.holds), wantItems, wantHolds)
	}
	if n := len(im.h.records("item")); n != wantItems {
		t.Errorf("the home holds %d items", n)
	}
	if n := len(im.h.records("hold")); n != wantHolds {
		t.Errorf("the home holds %d holds", n)
	}
	want := map[string]int{"pool.legacy.imported": 1, "pool.item.added": wantItems}
	if wantHolds > 0 {
		want["pool.hold.placed"] = wantHolds
	}
	if got := im.receiptKinds(); !equalCounts(got, want) {
		t.Errorf("the receipts are %v; want %v", got, want)
	}
	migrationID := im.migration["migration_id"]
	for legacyID, rec := range im.items {
		if legacyMember(rec, "legacy_id") != legacyID || legacyMember(rec, "migration_id") != migrationID {
			t.Errorf("%s: legacy member %v", legacyID, rec["legacy"])
		}
		if _, has := rec["level"]; has {
			t.Errorf("%s: an imported item takes no level from its title, and has %v", legacyID, rec["level"])
		}
		e := im.entry(legacyID)
		if e["disposition"] != "item" || e["task_id"] != rec["task_id"] {
			t.Errorf("%s: the migration's entry is %v", legacyID, e)
		}
		if _, has := rec["parent_id"]; has {
			t.Errorf("%s: an imported item has a parent", legacyID)
		}
	}
	for legacyID, hold := range im.holds {
		if e := im.entry(legacyID); e["hold_id"] != hold["hold_id"] {
			t.Errorf("%s: the migration's entry names the hold %v; the hold is %v", legacyID, e["hold_id"], hold["hold_id"])
		}
		if hold["state"] != "active" || hold["task_id"] != im.items[legacyID]["task_id"] {
			t.Errorf("%s: the hold is %v", legacyID, hold)
		}
	}
	// The response names what was written.
	if got, _ := im.body["items"].([]any); len(got) != wantItems {
		t.Errorf("the response lists %d items", len(got))
	}
	if got, _ := im.body["holds"].([]any); len(got) != wantHolds {
		t.Errorf("the response lists %d holds", len(got))
	}
	if r, _ := im.body["report"].(map[string]any); r == nil || r["round_trip_exact"] != true {
		t.Errorf("the report is %v", im.body["report"])
	}
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// dep is the edge of the item to the item of the legacy identifier.
func (im *imported) dep(from, typ, to string) map[string]any {
	im.h.t.Helper()
	for _, v := range im.items[from]["deps"].([]any) {
		if d := v.(map[string]any); d["type"] == typ && d["task_id"] == im.taskID(to) {
			return d
		}
	}
	im.h.t.Fatalf("%s has no %s edge to %s resolved to %s: %v", from, typ, to, im.taskID(to), im.items[from]["deps"])
	return nil
}

// The two upstream fixtures, re-expressed as Hive fixtures, are imported for
// real: one migration, an item for every entry but the persistent supervisor,
// the dependencies resolved to the identities the same import minted, and the
// source file left as it was.
func TestLiveImportOfTheTwoUpstreamFixtures(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("backlog.md", "UTC")
	im.checkWritten(8, 0)
	if e := im.entry("keeps-gadget-h7"); e["disposition"] != "excluded" || e["reason"] == "" {
		t.Errorf("the persistent supervisor entry is %v", e)
	} else if _, has := e["task_id"]; has {
		t.Error("an excluded entry has an item")
	}
	if _, has := im.items["keeps-gadget-h7"]; has {
		t.Error("an item was made of the persistent supervisor")
	}
	for legacyID, state := range map[string]string{
		"cache-adopt-l5": "in-flight", "cache-adopt": "queued", "promote-check": "queued", "key-cleanup": "queued",
		"survey-scout-d4": "done", "cache-core-t4": "done", "route-fix-k4": "done", "notes-multi-w8": "done",
	} {
		rec := im.items[legacyID]
		if rec == nil || rec["state"] != state {
			t.Errorf("%s: %v; want state %s", legacyID, rec, state)
			continue
		}
		if state == "done" && (rec["completed"] != true || (rec["closed_at"] == nil && rec["closed_at_unknown"] != true)) {
			t.Errorf("%s: a done entry is imported completed with its closing time: %v", legacyID, rec)
		}
	}
	// The leading kind word and the repo tag are kept as written; nothing is
	// inferred into the deliverable or the class.
	rec := im.items["cache-adopt-l5"]
	if legacyMember(rec, "kind") != "ship" {
		t.Errorf("cache-adopt-l5: legacy %v", rec["legacy"])
	}
	for _, name := range []string{"deliverable", "class"} {
		if _, has := rec[name]; has {
			t.Errorf("cache-adopt-l5: %s is inferred", name)
		}
	}
	if got := legacyMember(im.items["cache-adopt"], "repo"); got != "acme" {
		t.Errorf("cache-adopt: repo %q", got)
	}
	// An edge to an entry of the same file is resolved to the identity the
	// import minted for it.
	im.dep("cache-adopt", "blocked-by", "cache-core-t4")
	// The body of an entry is its continuation lines, kept as they are: a tag
	// on one of them is prose, and the entry has no closing date.
	if body := im.items["notes-multi-w8"]["body"]; body != "Follow-up note added later: the harness environment was the cause, not the feature.\nSecond continuation line for good measure. (reported 2026-10-02)" {
		t.Errorf("notes-multi-w8: body %q", body)
	}
	if im.items["notes-multi-w8"]["closed_at_unknown"] != true || im.items["cache-core-t4"]["closed_at"] != "2026-10-02T00:00:00.000Z" {
		t.Errorf("the closing dates are %v and %v", im.items["notes-multi-w8"], im.items["cache-core-t4"]["closed_at"])
	}

	// The second fixture is a second migration into the same home.
	im2 := h.importFixture("checkbox-in-flight.md", "UTC")
	if n := len(h.records("migration")); n != 2 {
		t.Errorf("the home holds %d migrations", n)
	}
	if len(im2.items) != 3 || len(im2.holds) != 0 {
		t.Errorf("the second import made %d items and %d holds", len(im2.items), len(im2.holds))
	}
	d := im2.dep("add-checks-q7", "blocked-by", "fix-signin-k3")
	if d["reason"] != "waits on the sign-in refactor" {
		t.Errorf("the edge's reason is %v", d["reason"])
	}
	if im2.items["fix-signin-k3"]["state"] != "in-flight" || im2.items["old-done-z1"]["state"] != "done" {
		t.Error("the sections are the states")
	}
	if got := im2.items["old-done-z1"]["links"].([]any); len(got) != 1 || got[0].(map[string]any)["kind"] != "harvest" {
		t.Errorf("old-done-z1: links %v", got)
	}
}

// A file with every tag the table names: the hold kinds with their dates, the
// priority, the dates read in the zone of the import, the links, the three
// kinds of edge with their reasons, a done entry with no closing date, and a
// section that is not one of the three.
func TestLiveImportOfAFileWithEveryTag(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("every-tag.md", "Europe/Helsinki")
	im.checkWritten(8, 2)
	if _, has := im.items["looks-like-an-entry"]; has {
		t.Error("an entry of an unrecognized section became an item")
	}
	a1, b2 := im.items["tagged-a1"], im.items["tagged-b2"]
	if a1["state"] != "in-flight" || a1["priority"] != int64(1) || legacyMember(a1, "kind") != "ship" || legacyMember(a1, "repo") != "app" {
		t.Errorf("tagged-a1: %v", a1)
	}
	// 2026-10-03 at the start of the day in Helsinki, in summer time.
	if a1["created_at"] != "2026-10-02T21:00:00.000Z" {
		t.Errorf("tagged-a1: created_at %v", a1["created_at"])
	}
	if d := im.dep("tagged-a1", "blocked-by", "tagged-b2"); d["reason"] != "needs the gauge schema" {
		t.Errorf("tagged-a1: %v", d)
	}
	if b2["priority"] != int64(4) || legacyMember(b2, "kind") != "docs" {
		t.Errorf("tagged-b2: %v", b2)
	}
	kinds := map[string]string{}
	for _, v := range b2["links"].([]any) {
		l := v.(map[string]any)
		target, _ := l["url"].(string)
		if target == "" {
			target, _ = l["path"].(string)
		}
		kinds[l["kind"].(string)] = target
	}
	if kinds["harvest"] != "https://github.com/acme/app/pull/77" || kinds["report"] != "data/tagged-b2/report.md" || kinds["doc"] != "https://example.org/spec" {
		t.Errorf("tagged-b2: links %v", b2["links"])
	}
	if b2["body"] != "Body line one.\n\nBody line after a blank." {
		t.Errorf("tagged-b2: body %q", b2["body"])
	}
	// The hold of an entry: its kind, its reason and its date kept as an
	// instant, a zone and the date as written.
	hold := im.holds["tagged-b2"]
	until, _ := hold["until"].(map[string]any)
	if hold["kind"] != "parked" || hold["reason"] != "waiting for the review slot" ||
		until["at"] != "2026-11-30T22:00:00.000Z" || until["zone"] != "Europe/Helsinki" || until["date"] != "2026-12-01" {
		t.Errorf("tagged-b2: hold %v", hold)
	}
	word := im.holds["tagged-e5"]
	until, _ = word["until"].(map[string]any)
	if word["kind"] != "word" || word["reason"] != "awaiting the word" || until["date"] != "2026-11-01" || until["at"] != "2026-10-31T22:00:00.000Z" {
		t.Errorf("tagged-e5: hold %v", word)
	}
	// The three kinds of edge, each resolved, with their reasons.
	c3 := im.items["tagged-c3"]
	if len(c3["deps"].([]any)) != 3 {
		t.Errorf("tagged-c3: deps %v", c3["deps"])
	}
	im.dep("tagged-c3", "blocked-by", "tagged-b2")
	if d := im.dep("tagged-c3", "parent", "tagged-a1"); d["reason"] != "the umbrella" {
		t.Errorf("tagged-c3: %v", d)
	}
	if d := im.dep("tagged-c3", "discovered-from", "tagged-d4"); d["reason"] != "found while testing" {
		t.Errorf("tagged-c3: %v", d)
	}
	// The closing date of a done entry is the start of its day in the zone of
	// the import; one with none is completed with its time unknown and no time
	// invented for it.
	for legacyID, closed := range map[string]string{"tagged-d4": "2026-10-03T21:00:00.000Z", "tagged-f6": "2026-10-03T21:00:00.000Z", "tagged-h8": "2026-10-04T21:00:00.000Z"} {
		rec := im.items[legacyID]
		if rec["state"] != "done" || rec["completed"] != true || rec["closed_at"] != closed {
			t.Errorf("%s: %v", legacyID, rec)
		}
	}
	g7 := im.items["tagged-g7"]
	if g7["state"] != "done" || g7["completed"] != true || g7["closed_at_unknown"] != true {
		t.Errorf("tagged-g7: %v", g7)
	}
	if _, has := g7["closed_at"]; has {
		t.Errorf("tagged-g7: a time was invented: %v", g7["closed_at"])
	}
	if got := im.migration["zone"]; got != "Europe/Helsinki" {
		t.Errorf("the migration's zone is %v", got)
	}
}

// A file with carriage returns, a header suffix, an unrecognized section and no
// final newline is imported with its source kept exactly.
func TestLiveImportOfCarriageReturnsASuffixAndAnUnrecognizedSection(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("crlf-suffix-unrecognized.md", "UTC")
	im.checkWritten(3, 0)
	if !strings.Contains(im.migration["source"].(map[string]any)["text"].(string), "\r\n") {
		t.Error("the migration lost the carriage returns")
	}
	pre := im.migration["preamble"].([]any)
	if len(pre) != 4 || pre[0] != "# Backlog\r" || pre[1] != "\r" {
		t.Errorf("the preamble is %q", pre)
	}
	var headers, states []string
	for _, v := range im.migration["sections"].([]any) {
		s := v.(map[string]any)
		headers = append(headers, strings.TrimSuffix(s["header"].(string), "\r"))
		states = append(states, s["state"].(string))
	}
	if !slices.Equal(headers, []string{"## In flight", "## Queued", "## Ideas for later", "## Done (10 most recent)"}) ||
		!slices.Equal(states, []string{"in-flight", "queued", "none", "done"}) {
		t.Errorf("the sections are %v %v", headers, states)
	}
	if _, has := im.items["idea-c3"]; has {
		t.Error("an entry of an unrecognized section became an item")
	}
	if e := im.entry("trail-b2"); e["first_line"] != int64(10) || len(e["lines"].([]any)) != 3 || !strings.HasSuffix(e["lines"].([]any)[0].(string), "\r") {
		t.Errorf("trail-b2: %v", e)
	}
	// Every line without an identifier is kept, by its line number.
	free := map[int64]string{}
	for _, v := range im.migration["free_lines"].([]any) {
		l := v.(map[string]any)
		free[l["line"].(int64)] = l["text"].(string)
	}
	if free[7] != "- Loose note between entries.\r" {
		t.Errorf("the free lines are %v", free)
	}
}

// A file that does not render back byte for byte is refused with
// pool.legacy_not_round_trip and writes nothing.
func TestLiveImportOfAFileThatDoesNotRoundTripIsRefused(t *testing.T) {
	h := newHome(t)
	// A byte that is not UTF-8 cannot be kept in the migration's text, so the
	// parsed file cannot be rendered back from what is stored.
	path, data, when := sourceBytes(t, []byte("# Backlog\n\n## Queued\n- [ ] enc-a1 - caf\xe9 au lait\n"))
	before := snapshot(t, h.dir)
	resp := h.liveImport(path, "UTC", nil)
	refused(t, resp, envelope.Refused, "pool.legacy_not_round_trip")
	if len(resp.Receipts) != 0 {
		t.Errorf("receipts %v", resp.Receipts)
	}
	if after := snapshot(t, h.dir); !equal(before, after) {
		t.Errorf("a refused import wrote in the home: before %v, after %v", before, after)
	}
	unchanged(t, path, data, when)
}

// expected_digest makes the caller import what it inspected: another digest is
// refused as a conflict and writes nothing, and the right one imports.
func TestLiveImportRefusesAFileOtherThanTheOneInspected(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "checkbox-in-flight.md")
	before := snapshot(t, h.dir)
	resp := h.liveImport(path, "UTC", map[string]any{"expected_digest": canon.FileDigest([]byte("another file"))})
	refused(t, resp, envelope.Conflict, envelope.CodeRevisionConflict)
	if after := snapshot(t, h.dir); !equal(before, after) {
		t.Errorf("a refused import wrote in the home: before %v, after %v", before, after)
	}
	unchanged(t, path, data, when)
	ok(t, h.liveImport(path, "UTC", map[string]any{"expected_digest": canon.FileDigest(data)}))
	unchanged(t, path, data, when)
}

// A second import of the same source digest into the same home is
// already_exists, and changes nothing.
func TestLiveImportOfTheSameDigestTwiceIsAlreadyExists(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "backlog.md")
	ok(t, h.liveImport(path, "UTC", nil))
	before := snapshot(t, h.dir)
	resp := h.liveImport(path, "UTC", nil)
	refused(t, resp, envelope.Conflict, envelope.CodeAlreadyExists)
	if len(resp.Receipts) != 0 {
		t.Errorf("receipts %v", resp.Receipts)
	}
	if after := snapshot(t, h.dir); !equal(before, after) {
		t.Errorf("a refused import wrote in the home: before %v, after %v", before, after)
	}
	// The same bytes under another path are the same source.
	other := filepath.Join(t.TempDir(), "copy.md")
	if err := os.WriteFile(other, data, 0o600); err != nil {
		t.Fatal(err)
	}
	refused(t, h.liveImport(other, "UTC", nil), envelope.Conflict, envelope.CodeAlreadyExists)
	if n := len(h.records("migration")); n != 1 {
		t.Errorf("the home holds %d migrations", n)
	}
	unchanged(t, path, data, when)
}

// A file holding a string of a credential shape, wherever in it, is refused
// whole with nothing written: no migration, item, hold, receipt, intent or
// temporary file, and the refusal never repeats the string.
func TestLiveImportOfAFileHoldingACredentialShapeWritesNothing(t *testing.T) {
	for name, text := range map[string]string{
		"in a body line":  "# Backlog\n\n## Queued\n- [ ] cred-a1 - fine\n  the token is " + credential + " here\n\n## Done\n",
		"in a title":      "# Backlog\n\n## Queued\n- [ ] cred-b2 - rotate " + credential + " today (repo: x)\n\n## Done\n",
		"in the preamble": "# Backlog " + credential + "\n\n## Queued\n- [ ] cred-c3 - fine\n\n## Done\n",
		"in a free line":  "# Backlog\n\n## Queued\n- [ ] cred-d4 - fine\n\n## Notes\nremember " + credential + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			path, data, when := sourceBytes(t, []byte(text))
			before := snapshot(t, h.dir)
			resp := h.liveImport(path, "UTC", nil)
			refused(t, resp, envelope.Invalid, envelope.CodeSecretShapeRecognized)
			if strings.Contains(resp.Error.Message, credential) || strings.Contains(fmt.Sprint(resp.Error.Detail), credential) {
				t.Errorf("the refusal repeats the credential: %+v", resp.Error)
			}
			if len(resp.Receipts) != 0 {
				t.Errorf("receipts %v", resp.Receipts)
			}
			if after := snapshot(t, h.dir); !equal(before, after) {
				t.Errorf("a refused import wrote in the home: before %v, after %v", before, after)
			}
			unchanged(t, path, data, when)
		})
	}
}

// exportMigration is the body of an export from the migration.
func (h *home) exportMigration(id string, extra map[string]any) map[string]any {
	h.t.Helper()
	body := map[string]any{"migration_id": id, "zone": "UTC"}
	for k, v := range extra {
		body[k] = v
	}
	return ok(h.t, h.do("export-legacy", body))
}

// The export from each migration equals its source with exact true, each item
// emitted from its exact original lines.
func TestExportFromAMigrationOfAnUnmodifiedImportEqualsItsSource(t *testing.T) {
	for _, fixture := range []string{"backlog.md", "checkbox-in-flight.md", "every-tag.md", "crlf-suffix-unrecognized.md", "crlf-no-final-newline.md", "done-archive.md"} {
		t.Run(fixture, func(t *testing.T) {
			h := newHome(t)
			im := h.importFixture(fixture, "UTC")
			before := snapshot(t, h.dir)
			out := h.exportMigration(im.migration["migration_id"].(string), nil)
			if after := snapshot(t, h.dir); !equal(before, after) {
				t.Errorf("an export wrote in the home")
			}
			if out["text"] != string(im.data) || out["exact"] != true {
				t.Errorf("the export differs from its source (exact %v)\n got %q\nwant %q", out["exact"], out["text"], im.data)
			}
			if out["digest"] != canon.FileDigest(im.data) || out["bytes"] != int64(len(im.data)) {
				t.Errorf("the export names digest %v and %v bytes", out["digest"], out["bytes"])
			}
			listed := out["items"].([]any)
			if len(listed) != len(im.items) {
				t.Errorf("the export lists %d items; the import made %d", len(listed), len(im.items))
			}
			for _, v := range listed {
				row := v.(map[string]any)
				if row["form"] != "verbatim" || row["legacy_id"] == nil || im.items[row["legacy_id"].(string)]["task_id"] != row["task_id"] {
					t.Errorf("the export lists %v", row)
				}
			}
		})
	}
}

// sectionOf is the header of the section of the exported text that holds the
// entry, and whether it holds it.
func sectionOf(t testing.TB, text, legacyID string) string {
	t.Helper()
	b, err := legacy.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range b.Sections {
		for _, e := range s.Entries {
			if e.Task != nil && e.Task.ID == legacyID {
				return strings.TrimSuffix(s.HeaderLine, "\r")
			}
		}
	}
	return ""
}

// An item modified since its import is re-rendered canonically in the section
// of its present state, and the items around it are still emitted from their
// original lines: a transition moves an item to the end of the first section
// that maps to its new state, a change that leaves the state in place, and a
// hold released since the import, re-render it where it stands.
func TestExportFromAMigrationRendersAModifiedItemInTheSectionOfItsState(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("backlog.md", "UTC")
	id := im.migration["migration_id"].(string)
	adopt, clean, promote := im.taskID("cache-adopt"), im.taskID("key-cleanup"), im.taskID("promote-check")
	ok(t, h.do("transition", map[string]any{"task_id": adopt, "to": "in-flight"}))
	ok(t, h.do("update", map[string]any{"task_id": clean, "patch": map[string]any{"priority": int64(3)}}))
	ok(t, h.do("transition", map[string]any{"task_id": promote, "to": "done", "at": "2026-10-10T12:00:00.000Z"}))

	out := h.exportMigration(id, nil)
	text := out["text"].(string)
	if out["exact"] != false || text == string(im.data) {
		t.Fatal("the export of a modified import equals its source")
	}
	forms := map[string]string{}
	for _, v := range out["items"].([]any) {
		row := v.(map[string]any)
		forms[row["legacy_id"].(string)] = row["form"].(string)
	}
	for legacyID, form := range forms {
		want := "verbatim"
		if legacyID == "cache-adopt" || legacyID == "key-cleanup" || legacyID == "promote-check" {
			want = "rendered"
		}
		if form != want {
			t.Errorf("%s was emitted %s; want %s", legacyID, form, want)
		}
	}
	for legacyID, want := range map[string]string{
		"cache-adopt": "## In flight", "promote-check": "## Done (10 most recent)", "key-cleanup": "## Queued",
		"cache-adopt-l5": "## In flight", "cache-core-t4": "## Done (10 most recent)",
	} {
		if got := sectionOf(t, text, legacyID); got != want {
			t.Errorf("%s is under %q; want %q", legacyID, got, want)
		}
	}
	// The item that stays is rendered where it stands, with its new priority;
	// every line of an item that was not modified is still there.
	if !strings.Contains(text, "- [ ] key-cleanup - port the post-upload key pruning to the release workflow. Keep the newest 2, never touch the signing keys. (repo: monorepo) (priority: 3) (since ") {
		t.Errorf("key-cleanup is not rendered with its priority in place:\n%s", text)
	}
	for _, line := range strings.Split(string(im.data), "\n") {
		if strings.Contains(line, "cache-adopt ") || strings.Contains(line, "key-cleanup") || strings.Contains(line, "promote-check") {
			continue
		}
		if !strings.Contains(text, line) {
			t.Errorf("the line %q is lost", line)
		}
	}
	// The moved item is the last entry of its section.
	inFlight := strings.Index(text, "## In flight")
	queued := strings.Index(text, "## Queued")
	moved := strings.Index(text, "- [ ] cache-adopt - ")
	if !(inFlight < moved && moved < queued) {
		t.Errorf("cache-adopt is not in the in-flight section:\n%s", text)
	}
	if strings.Index(text, "(status) Mobile ladder") > moved {
		t.Error("cache-adopt is not after the last line of its section")
	}

	// The exported text is a backlog that imports again to the same states.
	again, againData, againWhen := sourceBytes(t, []byte(text))
	h2 := newHome(t)
	im2 := h2.importPath(again, againData, againWhen, "UTC")
	for legacyID, state := range map[string]string{"cache-adopt": "in-flight", "promote-check": "done", "key-cleanup": "queued"} {
		if got := im2.items[legacyID]["state"]; got != state {
			t.Errorf("%s imports again as %v; want %s", legacyID, got, state)
		}
	}
	if got := im2.items["key-cleanup"]["priority"]; got != int64(3) {
		t.Errorf("key-cleanup imports again with priority %v", got)
	}
}

// An entry whose title ends in an unfinished tag cannot be rendered
// canonically, but is emitted from its exact lines for as long as it is
// unmodified.
func TestExportFromAMigrationEmitsAnUnmodifiedEntryThatCannotBeRenderedCanonically(t *testing.T) {
	h := newHome(t)
	text := "# Backlog\n\n## Queued\n- [ ] open-a1 - fix (repo:\n- [ ] open-b2 - fine\n\n## Done\n"
	path, data, when := sourceBytes(t, []byte(text))
	im := h.importPath(path, data, when, "UTC")
	out := h.exportMigration(im.migration["migration_id"].(string), nil)
	if out["exact"] != true || out["text"] != text {
		t.Errorf("the export of an unmodified import is not its source (exact %v):\n%s", out["exact"], out["text"])
	}
}

// One legacy identifier that two imports gave to two items is never written
// for both in one rendering: an export that would emit it for both is refused
// naming it, rather than a file that would read back as one item, and the
// export of one migration, which emits it for one, is not.
func TestExportRefusesALegacyIdentifierOfTwoItemsInOneRendering(t *testing.T) {
	h := newHome(t)
	pathA, dataA, whenA := sourceBytes(t, []byte("# Backlog\n\n## Queued\n- [ ] dup-a1 - first\n\n## Done\n"))
	imA := h.importPath(pathA, dataA, whenA, "UTC")
	pathB, dataB, whenB := sourceBytes(t, []byte("# Backlog\n\n## Queued\n- [ ] dup-a1 - second\n\n## Done\n"))
	h.importPath(pathB, dataB, whenB, "UTC")
	id := imA.migration["migration_id"].(string)
	ok(t, h.do("update", map[string]any{"task_id": imA.taskID("dup-a1"), "patch": map[string]any{"priority": int64(3)}}))

	out := h.exportMigration(id, nil)
	if text := out["text"].(string); out["exact"] != false || !strings.Contains(text, "- [ ] dup-a1 - first (priority: 3)") || strings.Contains(text, "second") {
		t.Errorf("the export of the migration does not render its own item alone (exact %v):\n%s", out["exact"], text)
	}
	resp := h.do("export-legacy", map[string]any{"migration_id": id, "zone": "UTC", "normalize": true})
	refused(t, resp, envelope.Invalid, "pool.legacy_malformed")
	if resp.Error.Detail["legacy_id"] != "dup-a1" {
		t.Errorf("the refusal names %v", resp.Error.Detail)
	}
}

// An item whose new state moves it behind a later entry of its identifier, a
// duplicate or an excluded entry, would not be read back as the item: the
// export is refused naming the identifier.
func TestExportFromAMigrationRefusesAnItemMovedBehindAnotherEntryOfItsIdentifier(t *testing.T) {
	for name, text := range map[string]string{
		"a duplicate":       "# Backlog\n\n## Queued\n- [ ] twin-a1 - current\n- [ ] twin-a1 - stale\n\n## Done\n",
		"an excluded entry": "# Backlog\n\n## Queued\n- [ ] twin-a1 - current\n\n## Done\n- [x] twin-a1 - PERSISTENT SECONDMATE owns the project (done 2026-10-01)\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			path, data, when := sourceBytes(t, []byte(text))
			im := h.importPath(path, data, when, "UTC")
			ok(t, h.do("transition", map[string]any{"task_id": im.taskID("twin-a1"), "to": "done", "at": "2026-10-10T12:00:00.000Z"}))
			resp := h.do("export-legacy", map[string]any{"migration_id": im.migration["migration_id"].(string), "zone": "UTC"})
			refused(t, resp, envelope.Invalid, "pool.legacy_malformed")
			if resp.Error.Detail["legacy_id"] != "twin-a1" {
				t.Errorf("the refusal names %v", resp.Error.Detail)
			}
		})
	}
}

// A hold released since the import makes the item modified, because the legacy
// form carries the hold in the entry's own tags: it is re-rendered without the
// hold, never emitted from the original lines. A hold placed since the import
// does the same.
func TestExportFromAMigrationRendersAnItemWhoseHoldsChanged(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("every-tag.md", "UTC")
	id := im.migration["migration_id"].(string)
	ok(t, h.do("release", map[string]any{"hold_id": im.holds["tagged-b2"]["hold_id"]}))
	ok(t, h.do("hold", map[string]any{"task_id": im.taskID("tagged-c3"), "kind": "parked", "reason": "waiting on the vendor"}))

	out := h.exportMigration(id, nil)
	text := out["text"].(string)
	if out["exact"] != false {
		t.Error("the export of an import with a released hold is exact")
	}
	forms := map[string]string{}
	for _, v := range out["items"].([]any) {
		row := v.(map[string]any)
		forms[row["legacy_id"].(string)] = row["form"].(string)
	}
	for legacyID, form := range forms {
		want := "verbatim"
		if legacyID == "tagged-b2" || legacyID == "tagged-c3" {
			want = "rendered"
		}
		if form != want {
			t.Errorf("%s was emitted %s; want %s", legacyID, form, want)
		}
	}
	b, err := legacy.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	b2, c3, e5 := b.Find("tagged-b2"), b.Find("tagged-c3"), b.Find("tagged-e5")
	if b2 == nil || b2.Hold != nil || strings.Contains(text, "waiting for the review slot") {
		t.Errorf("tagged-b2 is written back with its released hold: %+v", b2)
	}
	if c3 == nil || c3.Hold == nil || c3.Hold.Reason != "waiting on the vendor" {
		t.Errorf("tagged-c3 is written without its new hold: %+v", c3)
	}
	if e5 == nil || e5.Hold == nil || e5.Hold.Kind != "captain" {
		t.Errorf("tagged-e5 lost its hold: %+v", e5)
	}
	// Both stay in the section they were in.
	for _, legacyID := range []string{"tagged-b2", "tagged-c3"} {
		if got := sectionOf(t, text, legacyID); !strings.HasPrefix(got, "## Queued") {
			t.Errorf("%s is under %q", legacyID, got)
		}
	}
}

// With normalize a migration_id adds nothing: the selector applies, and the
// items it matches are rendered canonically exactly as they are without a
// migration, not in the file's layout. Without normalize the migration is
// exported whole and the selector is ignored.
func TestExportWithNormalizeIgnoresTheMigration(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("backlog.md", "UTC")
	id := im.migration["migration_id"].(string)
	selector := map[string]any{"selector": map[string]any{"states": []any{"done"}}}

	plain := ok(t, h.do("export-legacy", map[string]any{"zone": "UTC", "selector": selector["selector"]}))
	normalized := h.exportMigration(id, map[string]any{"normalize": true, "selector": selector["selector"]})
	if normalized["text"] != plain["text"] || normalized["digest"] != plain["digest"] || normalized["exact"] != false {
		t.Errorf("the export with normalize differs from the export without a migration\n got %q\nwant %q", normalized["text"], plain["text"])
	}
	rows := normalized["items"].([]any)
	if len(rows) != 4 {
		t.Errorf("the selector matched %d items", len(rows))
	}
	for _, v := range rows {
		if row := v.(map[string]any); row["form"] != "rendered" {
			t.Errorf("%v", row)
		}
	}
	// Nothing of the file's layout: no preamble, free line or excluded entry.
	if text := normalized["text"].(string); strings.Contains(text, "keeps-gadget-h7") || strings.Contains(text, "Release domain") || strings.Contains(text, "cache-adopt ") {
		t.Errorf("the export keeps the file's layout:\n%s", text)
	}

	// Without normalize the whole file is emitted and the selector ignored.
	whole := h.exportMigration(id, selector)
	if whole["text"] != string(im.data) || whole["exact"] != true {
		t.Errorf("the export of the migration is not the whole file (exact %v)", whole["exact"])
	}
}
