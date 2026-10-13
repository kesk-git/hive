package fixture

import (
	"fmt"
	"io"
	"path/filepath"
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

// Go runs the go command, found on the search path the test was started with,
// from the repository root, through contract/exec, with the toolchain
// variables the test set in its environment. It returns what go printed.
// Nothing contacts a network: the module has no dependency.
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
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Project, Name: "go"},
		Args:    args, Dir: dir, Config: goConfig,
		Stdin: exec.Null(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		return "", err
	}
	defer run.Close()
	errs := make(chan string, 1)
	go func() { b, _ := io.ReadAll(run.Stderr()); errs <- string(b) }()
	out, _ := io.ReadAll(run.Stdout())
	diag := <-errs
	res := run.Wait()
	text := string(out) + diag
	if !res.Passed() {
		return text, fmt.Errorf("go %s: %s\n%s", strings.Join(args, " "), res, strings.TrimSpace(text))
	}
	return text, nil
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
