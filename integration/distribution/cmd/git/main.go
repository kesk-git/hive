// Command git is the git of the restricted search path of test-distribution
// (spec/distribution.md, How it is proved item 6): the one program of the
// directory that stands on that path for the real Git. The tests build it from
// this source, with the directory of the real Git, found before the search
// path is restricted, linked in, and place it alone in a directory of its own
// under the name git. It starts the real Git where it is installed, through
// contract/exec, with the arguments, the working directory and the environment
// it was started with, relays the three standard streams and exits with the
// real Git's code. The real Git is never linked, copied or moved, so its
// installation finds its own parts on every platform, and nothing else of its
// directory is on the search path of the program that runs git.
//
// contract/exec finds a program by a bare name on the search path its process
// was started with, read once when the process loads it, and a process started
// on the restricted path has no directory of the real Git on that path. So the
// real Git is reached in two starts of this program. Started on a path that
// does not begin with the real Git's directory, it puts that directory first
// and starts git by name, which finds this program again: its own directory is
// the one directory of the restricted path that holds a git. Started on a path
// that begins with the real Git's directory, it takes that directory off again
// and starts git by name, which is now found there; the real Git is given the
// search path of the first start, unchanged.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kesk-git/hive/contract/exec"
)

// realDir is the directory of the real Git, set by the linker when the tests
// build this program.
var realDir string

func main() {
	if realDir == "" {
		fail(errors.New("it was built without the directory of the real Git"))
	}
	sep := string(os.PathListSeparator)
	path := os.Getenv("PATH")
	rest, second := strings.CutPrefix(path, realDir+sep)
	if second {
		os.Setenv("PATH", rest)
	} else {
		os.Setenv("PATH", realDir+sep+path)
	}
	dir, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	// contract/exec builds the environment of a run from nothing, by name, so
	// every variable this program was started with is named.
	var names []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			names = append(names, kv[:i])
		}
	}
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Core, Name: "git"},
		Args:    os.Args[1:],
		Dir:     dir,
		Config:  names,
		Stdin:   exec.Pipe(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		fail(err)
	}
	if second && filepath.Dir(run.Path) != realDir {
		run.Close()
		fail(fmt.Errorf("the real Git is no longer in %s: git is found as %s", realDir, run.Path))
	}
	go func() {
		io.Copy(run.Stdin(), os.Stdin)
		run.Stdin().Close()
	}()
	var wg sync.WaitGroup
	for _, c := range []struct {
		from io.Reader
		to   io.Writer
	}{{run.Stdout(), os.Stdout}, {run.Stderr(), os.Stderr}} {
		wg.Add(1)
		go func() { defer wg.Done(); io.Copy(c.to, c.from) }()
	}
	wg.Wait()
	res := run.Wait()
	run.Close()
	if res.Outcome != exec.Exited {
		fail(fmt.Errorf("the real Git %s", res))
	}
	os.Exit(int(res.Code))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "git of the restricted search path:", err)
	os.Exit(1)
}
