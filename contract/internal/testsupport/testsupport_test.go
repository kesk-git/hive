package testsupport

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// Only test files use the helpers: no file a delivered executable is built
// from imports the support packages. (That the support packages start no
// process is held by the scan of contract/exec, which reads every file but
// the test files of the repository.)
func TestOnlyTestFilesImportTheSupport(t *testing.T) {
	const support = "github.com/kesk-git/hive/contract/internal/testsupport"
	root := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(p), "/contract/internal/testsupport/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == support || strings.HasPrefix(path, support+"/") {
				t.Errorf("%s: a file that is not a test imports %s", p, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
