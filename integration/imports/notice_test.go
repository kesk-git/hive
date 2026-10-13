package imports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The repository holds no import record, so its root NOTICE is the fixed
// header and the one sentence that says no record exists yet. It catches a
// repository without a NOTICE, which the artefact would then ship without.
func TestRootNoticeSaysNoRecordExistsYet(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "imports"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "README.md" {
			t.Skipf("imports/ holds %s; this test covers the tree with no record", e.Name())
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
	if strings.Count(text, "No import record exists yet.") != 1 {
		t.Errorf("NOTICE does not say once that no import record exists yet:\n%s", text)
	}
}
