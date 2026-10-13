package calls

import (
	"os"
	"path/filepath"
	"testing"
	_ "unsafe" // for the linkname below
)

// startPath is the search path of the one process-execution path, which
// contract/exec reads once, when it is loaded, from the environment the test
// binary was started with: a later change to the environment does not move
// it, and the package exports no way to place a directory first on it. A
// test of this package must put the delivered executables and the callee it
// builds first on the search path (spec/test-targets.md, rule 3), as the
// tests inside contract/exec do with the same variable, so the variable is
// reached by name and every program is still started by contract/exec, found
// by its lookup rules.
//
//go:linkname startPath github.com/kesk-git/hive/contract/exec.startPath
var startPath string

// placeFirst puts dir first on the search path contract/exec finds programs
// by, ahead of what the test binary was started with, and returns the
// function that puts it back.
func placeFirst(dir string) (restore func()) {
	old := startPath
	startPath = dir + string(os.PathListSeparator) + old
	return func() { startPath = old }
}

// requireOnPath fails the test unless contract/exec's search path begins with
// dir, so that a change in how the variable is named fails loudly here and
// never leaves the tests running a program from somewhere else.
func requireOnPath(t testing.TB, dir string) {
	t.Helper()
	first := filepath.SplitList(startPath)
	if len(first) == 0 || first[0] != dir {
		t.Fatalf("the search path of contract/exec does not begin with %s: %q", dir, startPath)
	}
}
