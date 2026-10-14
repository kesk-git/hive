package proof

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skipped are the directories no scan reads: Git's own, the build output and
// test data, which holds the fixtures that show a check catches its break.
func skipped(name string) bool {
	return name == ".git" || name == "dist" || name == "testdata"
}

// walkFiles calls fn with the path of every file of the tree, relative to the
// root with forward slashes, outside the directories no scan reads.
func walkFiles(root string, fn func(rel string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipped(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(rel)
	})
}

// sourceFile is a Go source file that is part of a delivered executable's
// code: not a test file.
func sourceFile(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

// readFile reads a file of the tree.
func readFile(root, rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
}

// execPackage is the one package that starts processes.
const execPackage = "contract/exec"
