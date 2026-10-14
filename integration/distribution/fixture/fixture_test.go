package fixture_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/fixture"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// The fixture root carries the import records its NOTICE is produced from. In
// a repository that holds records, with the NOTICE they produce, the root is
// still one an assembly accepts, and the artefact carries that NOTICE. It
// catches a root that copies the NOTICE of records it leaves behind, which
// the assembler refuses as a NOTICE the records do not produce. The
// repository here is a temporary one: the declarations and the licence of
// this repository, and the two records of check-imports' own fixture with the
// NOTICE written for them by hand.
func TestRootCarriesTheRecordsItsNoticeIsProducedFrom(t *testing.T) {
	real, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	copyFrom := func(base, file string) {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(base, file)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declarations, err := filepath.Glob(filepath.Join(real, "packages", "*", "hive-package.json"))
	if err != nil || len(declarations) == 0 {
		t.Fatalf("no package declaration under packages/: %v", err)
	}
	for _, f := range append(declarations, filepath.Join(real, "contract", "hive-package.json"), filepath.Join(real, "LICENSE")) {
		copyFrom(real, f)
	}
	valid := filepath.Join(real, "integration", "imports", "testdata", "valid")
	records, err := filepath.Glob(filepath.Join(valid, "imports", "*.json"))
	if err != nil || len(records) == 0 {
		t.Fatalf("no record under %s: %v", valid, err)
	}
	for _, f := range append(records, filepath.Join(valid, "imports", "README.md"), filepath.Join(valid, "NOTICE")) {
		copyFrom(valid, f)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.invalid/hive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	dir, want, root, _ := fixture.Artefact(t, "windows")
	for _, f := range records {
		if _, err := os.Stat(filepath.Join(root, "imports", filepath.Base(f))); err != nil {
			t.Errorf("the fixture root lacks the record %s: %v", filepath.Base(f), err)
		}
	}
	produced, err := os.ReadFile(filepath.Join(valid, "NOTICE"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "NOTICE")); err != nil || !bytes.Equal(got, produced) {
		t.Errorf("the artefact's NOTICE is not the one the records produce: %v\n%s", err, got)
	}
	for _, p := range proof.Layout(dir, want) {
		t.Error(p)
	}
}
