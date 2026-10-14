package calls

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The search path of these tests is the one the test binary was started with.
// contract/exec reads it once, when it is loaded, and finds every program by
// it; the package exports no way to change it and the tests reach for none.
// The run that starts them is therefore what makes the tests possible: it
// puts the directory of the seven executables and the directory of the
// test-only callees on the search path, and tells the tests where they are.
// `make test-integration` builds the callees into dist/callee/ and starts the
// tests with dist/, the directory of the seven built executables, and
// dist/callee/ first on the search path, as a real installation has its bin/
// directory on it. The proof of the distribution starts them with the
// artefact's bin/ and a directory of the callees instead, and names both in
// the two variables below. The tests start nothing that is not found there by
// name, and fail, saying so, when the search path they were given does not
// hold the directories in force.

// The two variables that name the directories in force. An unset or empty
// variable leaves the directory of `make test-integration`; a set one must be
// the absolute path of an existing directory.
const (
	executablesEnv = "HIVE_TEST_EXECUTABLES_DIR"
	calleesEnv     = "HIVE_TEST_CALLEES_DIR"
)

// callDirs are the two directories the run places on the search path: the
// directory of the seven executables, and the callee's. They are the ones the
// variables name, and <repo>/dist and <repo>/dist/callee where one is unset.
func callDirs(t testing.TB) (dist, calleeDir string) {
	t.Helper()
	dist = dirFrom(t, executablesEnv, filepath.Join(repoRoot(t), "dist"))
	calleeDir = dirFrom(t, calleesEnv, filepath.Join(repoRoot(t), "dist", "callee"))
	return dist, calleeDir
}

// dirFrom is the directory the variable names, or def when it is unset or
// empty. A value that is not the absolute path of an existing directory fails
// the test.
func dirFrom(t testing.TB, name, def string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	if !filepath.IsAbs(v) {
		t.Fatalf("%s is %q: it must be the absolute path of an existing directory", name, v)
	}
	if fi, err := os.Stat(v); err != nil || !fi.IsDir() {
		t.Fatalf("%s is %q: it must be the absolute path of an existing directory", name, v)
	}
	return v
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

// isTheExecutable reports whether name is the executable exe: the same file
// or a link to it and, on Windows, where a link is a hard link or a
// byte-identical copy, a regular file with exactly its bytes.
func isTheExecutable(name, exe string) bool {
	if sameFile(name, exe) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	fa, err := os.Stat(name)
	if err != nil || !fa.Mode().IsRegular() {
		return false
	}
	a, errA := os.ReadFile(name)
	b, errB := os.ReadFile(exe)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// requireOnPath fails unless the directory of the executables and the callee's
// are both on the search path, so that a run that was not started with them
// on it fails loudly here and says what to run. They need
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
			return fmt.Errorf("%s is not on the search path %q; run `make build`, then `make test-integration`, which starts the tests with it on the path, or name the directories in %s and %s", dir, os.Getenv("PATH"), executablesEnv, calleesEnv)
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
			if !isTheExecutable(path, want) {
				return fmt.Errorf("%s is found on the search path as %s, not as the built %s", name, path, want)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not found on the search path; expected the built %s", name, want)
}

// A name is the executable where it is the same file or a link to it, and on
// Windows also where it is a byte-identical copy; a file with other bytes is
// never the executable.
func TestIsTheExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hive-pool"+suffix())
	copied := filepath.Join(dir, "hive-admit"+suffix())
	other := filepath.Join(dir, "hive-word"+suffix())
	for name, data := range map[string]string{exe: "the executable", copied: "the executable", other: "another program"} {
		if err := os.WriteFile(name, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if !isTheExecutable(exe, exe) {
		t.Error("an executable is not itself")
	}
	if got, want := isTheExecutable(copied, exe), runtime.GOOS == "windows"; got != want {
		t.Errorf("a byte-identical copy is the executable: %v, want %v on %s", got, want, runtime.GOOS)
	}
	if isTheExecutable(other, exe) {
		t.Error("a file with other bytes is the executable")
	}
	if isTheExecutable(filepath.Join(dir, "gone"+suffix()), exe) {
		t.Error("a file that is not there is the executable")
	}
}
