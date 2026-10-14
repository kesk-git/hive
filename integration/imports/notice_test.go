package imports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The root NOTICE is UTF-8 text that ends in one line feed, and it says that
// no import record exists yet exactly while imports/ holds none: once, with no
// record, and never beside one. It catches a repository without a NOTICE,
// which the artefact would then ship without, and a sentence left behind by
// the first record.
func TestRootNoticeSaysWhetherARecordExists(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "imports"))
	if err != nil {
		t.Fatal(err)
	}
	records := 0
	for _, e := range entries {
		if e.Name() != "README.md" {
			records++
		}
	}
	notice, err := os.ReadFile(filepath.Join("..", "..", "NOTICE"))
	if err != nil {
		t.Fatalf("the root NOTICE is missing: %v", err)
	}
	text := string(notice)
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
		t.Errorf("NOTICE is not UTF-8 text ending in one line feed")
	}
	want := 0
	if records == 0 {
		want = 1
	}
	if got := strings.Count(text, "No import record exists yet."); got != want {
		t.Errorf("imports/ holds %d records and NOTICE says %d times that no import record exists yet, want %d:\n%s", records, got, want, text)
	}
}
