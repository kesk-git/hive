package legacy_test

import (
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
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
