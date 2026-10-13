// Command testintegration is the recipe of `make test-integration`
// (spec/test-targets.md), written in Go so that the same target runs from the
// repository root on macOS, Windows and Linux with nothing but the Go
// toolchain (spec/distribution.md, The toolchain). It is no part of Hive and
// is run by `go run` from the root.
//
// It builds the test-only callee of integration/testcmd/ into dist/callee/,
// under both its command names, and starts the tests under integration/calls/
// with dist/, where `make build` left the seven executables, and dist/callee/
// ahead of the rest of the search path, in the platform's own list syntax.
// contract/exec reads that path once, when the test binary loads, so no test
// changes it. It does not build the seven executables: the tests fail, saying
// so, when they are absent or are not found first. Every program it starts,
// the Go toolchain itself, is started through contract/exec.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kesk-git/hive/contract/exec"
)

// target is the name the refusals are given under.
const target = "test-integration"

// callees are the command names the callee is built under.
var callees = []string{"hive-callee", "hive-callee-small"}

// goConfig are the Go toolchain's own configuration variables, passed by name
// to every run of it where they are set; the rest of a run's environment is
// built from nothing.
var goConfig = []string{
	"CGO_ENABLED", "GOCACHE", "GOENV", "GOEXPERIMENT", "GOFLAGS", "GOINSECURE", "GOMODCACHE",
	"GONOPROXY", "GONOSUMDB", "GOPATH", "GOPRIVATE", "GOPROXY", "GOROOT", "GOSUMDB",
	"GOTMPDIR", "GOTOOLCHAIN", "XDG_CACHE_HOME", "XDG_CONFIG_HOME",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if !holdsTest(filepath.Join(root, "integration", "calls")) {
		return errors.New("nothing to run: integration/calls holds no test")
	}
	if err := os.Setenv("CGO_ENABLED", "0"); err != nil {
		return err
	}

	dist := filepath.Join(root, "dist")
	calleeDir := filepath.Join(dist, "callee")
	if err := os.RemoveAll(calleeDir); err != nil {
		return err
	}
	if err := os.MkdirAll(calleeDir, 0o755); err != nil {
		return err
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	for _, name := range callees {
		if err := goTool(root, "build", "-trimpath", "-o", filepath.Join(calleeDir, name+suffix), "./integration/testcmd"); err != nil {
			return err
		}
	}

	sep := string(os.PathListSeparator)
	if err := os.Setenv("PATH", dist+sep+calleeDir+sep+os.Getenv("PATH")); err != nil {
		return err
	}
	return goTool(root, "test", "-count=1", "./integration/calls/...")
}

// moduleRoot is the root of the repository: the nearest directory at or above
// the working directory that holds go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod at or above the working directory")
		}
		dir = parent
	}
}

// holdsTest says whether a Go test file is anywhere under dir.
func holdsTest(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), "_test.go") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// goTool runs the Go toolchain with the arguments in dir, its output passed
// through as it comes, and fails unless it exits 0. An interrupt stops it.
func goTool(dir string, args ...string) error {
	r, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Project, Name: "go"},
		Args:    args,
		Dir:     dir,
		Config:  goConfig,
		Stdout:  exec.Pipe(),
		Stderr:  exec.Pipe(),
	})
	if err != nil {
		return err
	}
	defer r.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-interrupt:
			r.Stop(exec.Grace{Request: 5 * time.Second}, nil)
		case <-r.Done():
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(os.Stdout, r.Stdout()) }()
	go func() { defer wg.Done(); io.Copy(os.Stderr, r.Stderr()) }()
	wg.Wait()
	<-stopped
	if res := r.Wait(); !res.Passed() {
		return fmt.Errorf("go %s %s", strings.Join(args, " "), res)
	}
	return nil
}
