// Package harness holds what the tests of test-distribution use to run
// programs: the go command, found on the search path the test was started
// with, and the restricted search path of the integration run. Every process
// is started through contract/exec, the one process-execution path.
package harness

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/exec"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// goConfig are the variables of the Go toolchain's own configuration that a
// run of go is given, copied by name from the test's environment where they
// are set. Nothing else of the environment reaches it.
var goConfig = []string{
	"GOROOT", "GOPATH", "GOMODCACHE", "GOCACHE", "GOENV", "GOFLAGS", "GOTOOLCHAIN", "GOPROXY",
	"GONOSUMDB", "GONOSUMCHECK", "GOSUMDB", "GOTMPDIR", "GOWORK", "GO111MODULE",
	"GOOS", "GOARCH", "CGO_ENABLED",
}

// goRun runs go in dir and returns what it printed and how it ended.
func goRun(dir string, args ...string) (string, exec.Result, error) {
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Project, Name: "go"},
		Args:    args, Dir: dir, Config: goConfig,
		Stdin: exec.Null(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		return "", exec.Result{}, err
	}
	defer run.Close()
	errs := make(chan string, 1)
	go func() { b, _ := io.ReadAll(run.Stderr()); errs <- string(b) }()
	out, _ := io.ReadAll(run.Stdout())
	diag := <-errs
	return string(out) + diag, run.Wait(), nil
}

// Go runs the go command from the repository root, through contract/exec,
// with the toolchain variables the test set in its environment. It returns
// what go printed. Nothing contacts a network: the module has no dependency.
func Go(t testing.TB, args ...string) (string, error) {
	t.Helper()
	root, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return GoIn(t, root, args...)
}

// GoIn is Go run from a directory.
func GoIn(t testing.TB, dir string, args ...string) (string, error) {
	t.Helper()
	out, res, err := goRun(dir, args...)
	if err != nil {
		return "", err
	}
	if !res.Passed() {
		return out, fmt.Errorf("go %s: %s\n%s", strings.Join(args, " "), res, strings.TrimSpace(out))
	}
	return out, nil
}

var exitStatus = regexp.MustCompile(`(?m)^exit status (\d+)$`)

// GoProgram runs a main package with `go run` from dir and returns what it
// printed and the program's exit code. A failure of go itself, such as a
// package that does not build, is the error.
func GoProgram(t testing.TB, dir string, args ...string) (output string, code int, err error) {
	t.Helper()
	out, res, err := goRun(dir, append([]string{"run"}, args...)...)
	if err != nil {
		return "", 0, err
	}
	if res.Passed() {
		return out, 0, nil
	}
	// go run reports the program's own failure as "exit status N" and exits 1.
	if m := exitStatus.FindStringSubmatch(out); res.Outcome == exec.Exited && m != nil {
		n, _ := strconv.Atoi(m[1])
		return exitStatus.ReplaceAllString(out, ""), n, nil
	}
	return out, 0, fmt.Errorf("go run %s: %s\n%s", strings.Join(args, " "), res, strings.TrimSpace(out))
}

// Build builds the main package pkg, a path relative to the repository root,
// into out for the platform, without the C toolchain, and with paths trimmed
// unless trimpath is false.
func Build(t testing.TB, pkg, out, goos, goarch string, trimpath bool) {
	t.Helper()
	t.Setenv("GOOS", goos)
	t.Setenv("GOARCH", goarch)
	t.Setenv("CGO_ENABLED", "0")
	args := []string{"build", "-buildvcs=false"}
	if trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, "-o", filepath.ToSlash(out), "./"+filepath.ToSlash(pkg))
	if _, err := Go(t, args...); err != nil {
		t.Fatal(err)
	}
}
