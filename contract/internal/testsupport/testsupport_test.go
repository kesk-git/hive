package testsupport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/ids"
)

func TestIDIsAValidIdentifierThatCountsN(t *testing.T) {
	seen := map[string]bool{}
	for n := 0; n < 40; n++ {
		s := ID(ids.Task, n)
		if err := ids.Check(s, ids.Task); err != nil {
			t.Errorf("%d: %v", n, err)
		}
		if seen[s] {
			t.Errorf("%d: %s repeats", n, s)
		}
		seen[s] = true
	}
}

func TestTreeAndParsed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"a/x.json": `{"n":1}` + "\n", "y.json": "{}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := Tree(t, dir)
	if len(got) != 2 || got["a/x.json"] == "" || got["y.json"] == got["a/x.json"] {
		t.Errorf("Tree = %v", got)
	}
	if m := Parsed(t, []byte(`{"n":1}`)); m["n"] != int64(1) {
		t.Errorf("Parsed = %v", m)
	}
}
