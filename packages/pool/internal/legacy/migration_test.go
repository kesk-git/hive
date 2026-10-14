package legacy_test

import (
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/legacy"
)

// The codec and the read adapter of the pool.migration record
// (spec/pool.md, Legacy import and export): the pieces the record keeps lay the
// file out again, and a record whose pieces are not the source text kept beside
// them is refused rather than exported as if it were whole.

// stored is a copy of the migration record as it is stored.
func stored(t testing.TB, im *imported) map[string]any {
	t.Helper()
	data, err := canon.Encode(im.migration)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func TestMigrationLayoutRendersItsSourceBack(t *testing.T) {
	for _, fixture := range []string{"backlog.md", "every-tag.md", "crlf-suffix-unrecognized.md", "crlf-no-final-newline.md", "done-archive.md"} {
		t.Run(fixture, func(t *testing.T) {
			im := newHome(t).importFixture(fixture, "UTC")
			m, err := legacy.DecodeMigration(im.migration["migration_id"].(string), 1, stored(t, im))
			if err != nil {
				t.Fatal(err)
			}
			l, err := m.Layout()
			if err != nil {
				t.Fatal(err)
			}
			if l.Text() != string(im.data) {
				t.Errorf("the layout renders another text")
			}
			// The blank lines that end an entry's block are blocks of their own.
			for _, s := range l.Sections {
				for _, b := range s.Blocks {
					if b.Entry >= 0 && len(b.Lines) > 1 && strings.TrimSpace(b.Lines[len(b.Lines)-1]) == "" {
						t.Errorf("an entry block ends in a blank line: %q", b.Lines)
					}
				}
			}
		})
	}
}

func TestMigrationLayoutRefusesARecordThatIsNotWhole(t *testing.T) {
	im := newHome(t).importFixture("checkbox-in-flight.md", "UTC")
	id := im.migration["migration_id"].(string)
	for name, mutate := range map[string]func(rec map[string]any){
		"an entry line that is not the source's": func(rec map[string]any) {
			rec["entries"].([]any)[0].(map[string]any)["lines"].([]any)[0] = "- [ ] changed - words"
		},
		"an entry moved to another line": func(rec map[string]any) {
			rec["entries"].([]any)[0].(map[string]any)["first_line"] = int64(1)
		},
		"a free line dropped": func(rec map[string]any) {
			rec["preamble"] = rec["preamble"].([]any)[:1]
		},
		"a section dropped": func(rec map[string]any) {
			rec["sections"] = rec["sections"].([]any)[1:]
		},
		"a header that is not the source's": func(rec map[string]any) {
			rec["sections"].([]any)[0].(map[string]any)["header"] = "## Another"
		},
		"a source text that is another": func(rec map[string]any) {
			src := rec["source"].(map[string]any)
			src["text"] = src["text"].(string) + "\nmore"
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := stored(t, im)
			mutate(rec)
			m, err := legacy.DecodeMigration(id, 1, rec)
			if err == nil {
				_, err = m.Layout()
			}
			if err == nil {
				t.Error("the record was laid out")
			}
		})
	}
}

// importText imports the text for real.
func (h *home) importText(text, zone string) *imported {
	h.t.Helper()
	path, data, when := sourceBytes(h.t, []byte(text))
	return h.importPath(path, data, when, zone)
}

// The migration is read through hive-pool read, whole, by identity and by
// query, and an identity that names none is not found.
func TestMigrationIsRead(t *testing.T) {
	h := newHome(t)
	im := h.importFixture("checkbox-in-flight.md", "UTC")
	id := im.migration["migration_id"].(string)
	body := ok(t, h.do("read", map[string]any{"kind": "migration", "selector": map[string]any{"id": id}}))
	records := body["records"].([]any)
	if len(records) != 1 || records[0].(map[string]any)["migration_id"] != id || records[0].(map[string]any)["source"].(map[string]any)["text"] != string(im.data) {
		t.Fatalf("the read gave %v", records)
	}
	h.importFixture("backlog.md", "UTC")
	body = ok(t, h.do("read", map[string]any{"kind": "migration", "selector": map[string]any{}}))
	if got := len(body["records"].([]any)); got != 2 {
		t.Errorf("a query lists %d migrations", got)
	}
	body = ok(t, h.do("read", map[string]any{"kind": "migration", "selector": map[string]any{"limit": int64(1)}}))
	if got := len(body["records"].([]any)); got != 1 {
		t.Errorf("a limit of one lists %d migrations", got)
	}
	refused(t, h.do("read", map[string]any{"kind": "migration", "selector": map[string]any{"id": "migr_" + strings.Repeat("0", 26)}}), envelope.NotFound, envelope.CodeNotFound)
}

// A date that is not a calendar date is kept in the entry's lines and warned
// of; the item is created now, and a done entry is completed with its time
// unknown, since no time is invented for it.
func TestMigrationOfADateThatIsNotACalendarDate(t *testing.T) {
	text := "# Backlog\n\n## Queued\n- [ ] t-1 - Title (since 2026-02-30) (hold: later) (hold-kind: parked) (hold-until: 2026-13-01)\n\n## Done\n- [x] t-2 - Closed (done 2026-00-10)\n"
	h := newHome(t)
	im := h.importText(text, "UTC")
	if im.items["t-1"]["created_at"] != "2026-10-14T09:00:00.000Z" {
		t.Errorf("created_at %v", im.items["t-1"]["created_at"])
	}
	if _, has := im.holds["t-1"]["until"]; has {
		t.Errorf("a hold with a date that is none has until %v", im.holds["t-1"]["until"])
	}
	if t2 := im.items["t-2"]; t2["completed"] != true || t2["closed_at_unknown"] != true || t2["closed_at"] != nil {
		t.Errorf("t-2 is %v", t2)
	}
	for _, id := range []string{"t-1", "t-2"} {
		if ws := im.entry(id)["warnings"]; ws == nil {
			t.Errorf("%s: nothing is warned of", id)
		}
	}
	// The export is still the file.
	if out := h.exportMigration(im.migration["migration_id"].(string), nil); out["text"] != text || out["exact"] != true {
		t.Errorf("the export is %q", out["text"])
	}
}
