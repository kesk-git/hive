package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/proof"
)

// Suffix is the suffix of an executable's name on the host.
func Suffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// FakeNames are the programs a Hive executable runs that the artefact does not
// hold and that are not Git: gh, claude, codex and no-mistakes, and on macOS
// security, the platform utility that stands for the declared one and not for
// a sixth dependency (spec/distribution.md, How it is proved item 6).
func FakeNames(goos string) []string {
	names := []string{"gh", "claude", "codex", "no-mistakes"}
	if goos == "darwin" {
		names = append(names, "security")
	}
	return names
}

// BuildFakes builds the recorded fake from its Go source and places a copy of
// the executable under the name of each program in dir. A fake is an
// executable, never a script, so the lookup rules find it on every platform.
func BuildFakes(t testing.TB, dir string) {
	t.Helper()
	built := filepath.Join(t.TempDir(), "fake"+Suffix())
	Build(t, "integration/distribution/testdata/fake", built, runtime.GOOS, runtime.GOARCH, true)
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range FakeNames(runtime.GOOS) {
		if err := os.WriteFile(filepath.Join(dir, name+Suffix()), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// Callees are the names of the test-only callees of integration/testcmd/ that
// the integration tests start.
var Callees = []string{"hive-callee", "hive-callee-small"}

// BuildCallees builds the test-only callee of ./integration/testcmd, without
// the C toolchain and with paths trimmed, into dir once under each of the
// names of Callees, as `make test-integration` does. A callee is an
// executable the test builds, never a script.
func BuildCallees(t testing.TB, dir string) {
	t.Helper()
	for _, name := range Callees {
		Build(t, "integration/testcmd", filepath.Join(dir, name+Suffix()), runtime.GOOS, runtime.GOARCH, true)
	}
}

// GitDir is the directory of the real Git, found once on the search path the
// test was started with.
func GitDir() (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, "git"+Suffix()))
		if err == nil && fi.Mode().IsRegular() && (runtime.GOOS == "windows" || fi.Mode().Perm()&0o111 != 0) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("git is not on the search path; the integration tests run the real Git")
}

// Restricted is the world of the integration run: a search path of the
// artefact's bin/, the directory of the recorded fakes, which also holds the
// test-only callees, and a directory of its own that holds only the real Git,
// and a home of the run's own.
type Restricted struct {
	Bin, Fakes, Git string
	Home            string
}

// NewRestricted makes the world of an integration run for the artefact in dir,
// with the fakes built, the real Git linked alone into a directory of its own,
// so that no other program of Git's directory is reachable, and a home created
// under a temporary directory.
func NewRestricted(t testing.TB, artefact string) Restricted {
	t.Helper()
	gitDir, err := GitDir()
	if err != nil {
		t.Fatal(err)
	}
	git := t.TempDir()
	if err := os.Symlink(filepath.Join(gitDir, "git"+Suffix()), filepath.Join(git, "git"+Suffix())); err != nil {
		t.Fatalf("the real Git cannot be exposed alone: %v", err)
	}
	r := Restricted{Bin: filepath.Join(artefact, "bin"), Fakes: t.TempDir(), Git: git, Home: t.TempDir()}
	BuildFakes(t, r.Fakes)
	BuildCallees(t, r.Fakes)
	return r
}

// Path is the search path of the run: exactly these three directories.
func (r Restricted) Path() string {
	return strings.Join([]string{r.Bin, r.Fakes, r.Git}, string(os.PathListSeparator))
}

// Run starts the program, by its bare name, with the arguments, from dir,
// under the search path and the home of the run, and returns what it printed
// and its exit code. Only the programs on the search path can be found.
func (r Restricted) Run(t testing.TB, dir, program string, args ...string) (string, int, error) {
	t.Helper()
	return LaunchWith(t, r.Path(), r.Home, nil, dir, program, args...)
}

// Environment is what the integration tests are told besides the search path
// and the home: the directory of the seven executables, which is the
// artefact's bin/, and the directory of the test-only callees, which is the
// directory of the fakes. The tests find the executables and the callees in
// these two directories, not in dist/.
func (r Restricted) Environment() []string {
	return []string{"HIVE_TEST_EXECUTABLES_DIR=" + r.Bin, "HIVE_TEST_CALLEES_DIR=" + r.Fakes}
}

// Launch starts the program, by its bare name, from dir, with exactly the
// search path and the home given, and returns what it printed and its exit
// code. contract/exec reads its search path once, when a process loads it, so
// a test cannot start by a bare name a program that its own path does not
// hold; Launch starts a launcher of the repository with `go run`, under the
// path, and the launcher starts the program through contract/exec and takes
// the toolchain's directory, which `go run` puts first, off the program's path.
func Launch(t testing.TB, path, home, dir, program string, args ...string) (string, int, error) {
	t.Helper()
	return LaunchWith(t, path, home, nil, dir, program, args...)
}

// LaunchWith is Launch with the variables given, each as name=value, which the
// program is also started with and nothing else of the test's environment.
func LaunchWith(t testing.TB, path, home string, env []string, dir, program string, args ...string) (string, int, error) {
	t.Helper()
	// The launcher is built and started by go, which finds the program on this
	// path; go itself is found on the path the test was started with.
	t.Setenv("PATH", path)
	t.Setenv("CGO_ENABLED", "0")
	root, err := proof.RepoRoot()
	if err != nil {
		return "", 0, err
	}
	launch := []string{
		"github.com/kesk-git/hive/integration/distribution/cmd/runwith",
		"-path", path, "-home", home, "-dir", dir,
	}
	for _, kv := range env {
		launch = append(launch, "-env", kv)
	}
	launch = append(append(launch, program), args...)
	return GoProgram(t, root, launch...)
}

// Integration compiles the integration tests of ./integration/calls, places the
// compiled binary beside the fakes, and runs it under the restricted path. It
// returns what the run printed and whether it passed; a repository with no
// integration test has nothing to run, which is an error.
func (r Restricted) Integration(t testing.TB) (string, error) {
	t.Helper()
	root, err := proof.RepoRoot()
	if err != nil {
		return "", err
	}
	tests, _ := filepath.Glob(filepath.Join(root, "integration", "calls", "*_test.go"))
	if len(tests) == 0 {
		return "", fmt.Errorf("integration/calls holds no test: there is nothing to run")
	}
	name := "integration-calls"
	if _, err := Go(t, "test", "-c", "-o", filepath.ToSlash(filepath.Join(r.Fakes, name+Suffix())), "./integration/calls"); err != nil {
		return "", err
	}
	out, code, err := LaunchWith(t, r.Path(), r.Home, r.Environment(), filepath.Join(root, "integration", "calls"), name, "-test.count=1")
	if err != nil {
		return out, err
	}
	if code != 0 {
		return out, fmt.Errorf("the integration tests failed with the code %d under the restricted search path", code)
	}
	return out, nil
}
