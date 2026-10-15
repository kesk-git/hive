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

// writeTree makes the files of a directory from slash paths and contents.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func digestOf(t *testing.T, data string) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"f": data})
	return Tree(t, dir)["f"]
}

// A lock file is listed with the placeholder whatever its content; a file
// elsewhere is digested, even one named like a lock file; a file nested
// below locks/ is digested like any other.
func TestTreeListsALockFileWithoutReadingIt(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"locks/pool.lock":     "",
		"locks/dispatch.lock": "pid 4242\n",
		"locks/other":         "not named like a lock",
		"locks/deep/x.lock":   "nested",
		"pool/pool.lock":      "elsewhere",
		"pool/locks/y.lock":   "a locks directory that is not top-level",
		"pool.lock":           "top level",
		"locks.lock":          "not in locks/",
		"pool/records/a.json": "{}\n",
	})
	got := Tree(t, dir)
	for _, k := range []string{"locks/pool.lock", "locks/dispatch.lock", "locks/other"} {
		if got[k] != LockPlaceholder {
			t.Errorf("%s = %q, want the placeholder", k, got[k])
		}
	}
	for k, content := range map[string]string{
		"locks/deep/x.lock":   "nested",
		"pool/pool.lock":      "elsewhere",
		"pool/locks/y.lock":   "a locks directory that is not top-level",
		"pool.lock":           "top level",
		"locks.lock":          "not in locks/",
		"pool/records/a.json": "{}\n",
	} {
		if want := digestOf(t, content); got[k] != want || want == "" || want == LockPlaceholder {
			t.Errorf("%s = %q, want the digest %q", k, got[k], want)
		}
	}
	if len(got) != 9 {
		t.Errorf("Tree = %v", got)
	}
}

// The placeholder is no digest, and the content of a lock file does not
// change what Tree reports.
func TestTreeLockPlaceholderIsNoDigest(t *testing.T) {
	if len(LockPlaceholder) == 64 {
		t.Errorf("placeholder %q is as long as a digest", LockPlaceholder)
	}
	if digestOf(t, "") == LockPlaceholder || digestOf(t, LockPlaceholder) == LockPlaceholder {
		t.Error("a digest equals the placeholder")
	}
	a, b := t.TempDir(), t.TempDir()
	writeTree(t, a, map[string]string{"locks/pool.lock": "one"})
	writeTree(t, b, map[string]string{"locks/pool.lock": "two, longer"})
	if x, y := Tree(t, a)["locks/pool.lock"], Tree(t, b)["locks/pool.lock"]; x != y {
		t.Errorf("lock files differ in the tree: %q, %q", x, y)
	}
}
