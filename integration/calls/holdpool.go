//go:build ignore

// Command holdpool is the pass-through hive-pool of word_pool_test.go, which
// the test builds from this file (spec/test-targets.md, rule 3) and places
// first on the search path of one hive-word only. It forwards every call to
// the real hive-pool unchanged: the arguments, the request on standard input,
// the response on standard output, standard error and the exit code. The one
// thing it does is hold the handoff of the one request it is given, which is
// every call whose operation is not read, as hive-word sends them: it writes
// the file named arrived into its own directory, holding the operation, and
// forwards the call once a file named release is there.
//
// It finds the real hive-pool by running itself once more: the first run,
// which has its own directory on its search path, takes that directory off the
// path it gives its child, so that the child, which contract/exec finds on the
// path it was started with, finds the real hive-pool and forwards to it.
package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kesk-git/hive/contract/exec"
)

// The files the test and the fake meet at, in the fake's own directory.
const (
	arrived = "arrived"
	release = "release"
	// patience is how long a held call waits for its release before it gives
	// up and ends with an error.
	patience = time.Minute
)

func main() { os.Exit(run()) }

func run() int {
	self, err := os.Executable()
	if err != nil {
		return 99
	}
	own := filepath.Dir(self)
	request, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 99
	}
	if rest, first := withoutDir(own); first {
		if len(os.Args) > 1 && os.Args[1] != "read" && !hold(own, os.Args[1]) {
			return 99
		}
		if os.Setenv("PATH", rest) != nil {
			return 99
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return 99
	}
	r, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Hive, Name: "hive-pool"},
		Args:    os.Args[1:],
		Dir:     dir,
		Stdin:   exec.Pipe(),
		Stdout:  exec.Pipe(),
		Stderr:  exec.Pipe(),
	})
	if err != nil {
		return 99
	}
	defer r.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(os.Stdout, r.Stdout()) }()
	go func() { defer wg.Done(); io.Copy(os.Stderr, r.Stderr()) }()
	go func() {
		r.Stdin().Write(request)
		r.Stdin().Close()
	}()
	wg.Wait()
	res := r.Wait()
	if res.Outcome != exec.Exited {
		return 98
	}
	return int(res.Code)
}

// withoutDir is the search path this run was started with, less the
// directory, and whether the directory was on it.
func withoutDir(dir string) (string, bool) {
	own, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	var kept []string
	found := false
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if fi, err := os.Stat(entry); err == nil && os.SameFile(fi, own) {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	return strings.Join(kept, string(os.PathListSeparator)), found
}

// hold says the call has arrived and waits for its release.
func hold(dir, operation string) bool {
	pending := filepath.Join(dir, arrived+".tmp")
	if os.WriteFile(pending, []byte(operation), 0o600) != nil || os.Rename(pending, filepath.Join(dir, arrived)) != nil {
		return false
	}
	for end := time.Now().Add(patience); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, release)); err == nil {
			return true
		}
	}
	return false
}
