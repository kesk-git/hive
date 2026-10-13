package calls

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The search path of these tests is the one the test binary was started with.
// contract/exec reads it once, when it is loaded, and finds every program by
// it; the package exports no way to change it and the tests reach for none.
// `make test-integration` is therefore what makes the tests possible: it builds
// the test-only callee into dist/callee/ and starts the tests with dist/, the
// directory of the seven built executables, and dist/callee/ first on the
// search path, as a real installation has its bin/ directory on it. The tests
// start nothing that is not found there by name, and fail, saying so, when
// the search path they were given is not that one.

// callDirs are the two directories `make test-integration` places ahead on the
// search path: the directory of the seven executables, and the callee's.
func callDirs(t testing.TB) (dist, calleeDir string) {
	t.Helper()
	dist = filepath.Join(repoRoot(t), "dist")
	return dist, filepath.Join(dist, "callee")
}

// searchDirs are the entries of the search path this process was started
// with, in order, in the platform's own list syntax.
func searchDirs() []string {
	return filepath.SplitList(os.Getenv("PATH"))
}

// sameFile reports whether the two names are one file or directory, whatever
// links or case lead to it.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// requireOnPath fails unless the directory of the built executables and the
// callee's are both on the search path, so that a run that was not started by
// `make test-integration` fails loudly here and says what to run. They need
// not be the first two entries: the Go tool puts the directory of a toolchain
// it switched to ahead of the path it was given. That nothing ahead of them
// shadows a program is what requireFoundIn checks, name by name.
func requireOnPath(dist, calleeDir string) error {
	for _, dir := range []string{dist, calleeDir} {
		found := false
		for _, entry := range searchDirs() {
			if entry != "" && filepath.IsAbs(entry) && sameFile(entry, dir) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is not on the search path %q; run `make build`, then `make test-integration`, which starts the tests with it on the path", dir, os.Getenv("PATH"))
		}
	}
	return nil
}

// requireFoundIn fails unless the program found by name on the
// search path, by the first match in the order of the list as contract/exec
// looks, is the file want, so that a program of the same name earlier on the
// path, or one that is not the built one, is never what a test starts.
func requireFoundIn(name, want string) error {
	for _, dir := range searchDirs() {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, name+suffix())
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			if !sameFile(path, want) {
				return fmt.Errorf("%s is found on the search path as %s, not as the built %s", name, path, want)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not found on the search path; expected the built %s", name, want)
}
