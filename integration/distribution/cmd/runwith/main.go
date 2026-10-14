// Command runwith starts one program of a given search path and home through
// contract/exec, relays its output and exits with its code. The tests start it
// with `go run`, which is what lets a test of a process whose search path is
// not its own start a program that only that path finds: contract/exec reads
// its search path once, when a process loads it, so a process cannot start a
// program by a name another path holds, and `go run` is the one program of the
// test's own path that starts another. `go run` puts the toolchain's directory
// first on the path it passes on, and this command takes it out again: the
// program is started with exactly the path it is given, and with a home of the
// caller's.
//
// A program is given nothing of this process's environment but the search
// path, the home and, where the run names them, the two directories the
// integration tests take from the run: -executables sets
// HIVE_TEST_EXECUTABLES_DIR, the directory of the seven executables, and
// -callees sets HIVE_TEST_CALLEES_DIR, the directory of the test-only callees.
// No other variable can be named.
//
// Usage: runwith -path <search path> -home <directory> [-dir <directory>] [-executables <directory>] [-callees <directory>] <program> [argument ...]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/kesk-git/hive/contract/exec"
)

// The two variables the integration tests take their directories from.
const (
	executablesVar = "HIVE_TEST_EXECUTABLES_DIR"
	calleesVar     = "HIVE_TEST_CALLEES_DIR"
)

func main() {
	path := flag.String("path", "", "the search path of the program")
	home := flag.String("home", "", "the home of the program")
	dir := flag.String("dir", "", "the working directory of the program; this process's own when empty")
	executables := flag.String("executables", "", "the directory of the seven executables, which the program is told in "+executablesVar)
	callees := flag.String("callees", "", "the directory of the test-only callees, which the program is told in "+calleesVar)
	flag.Parse()
	if flag.NArg() == 0 || *path == "" || *home == "" {
		fmt.Fprintln(os.Stderr, "usage: runwith -path <search path> -home <directory> [-dir <directory>] [-executables <directory>] [-callees <directory>] <program> [argument ...]")
		os.Exit(2)
	}
	if *dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		*dir = wd
	}
	// What the program is started with: contract/exec builds its environment
	// from this process's, by name.
	tmp := filepath.Join(*home, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		fail(err)
	}
	vars := map[string]string{"PATH": *path, "HOME": *home, "TMPDIR": tmp}
	if runtime.GOOS == "windows" {
		vars = map[string]string{"PATH": *path, "USERPROFILE": *home, "TEMP": tmp, "TMP": tmp, "APPDATA": filepath.Join(*home, "AppData")}
	}
	for k, v := range vars {
		os.Setenv(k, v)
	}
	// A directory the run names is set here and passed on by the name of its
	// variable, as the program's own configuration; contract/exec passes
	// nothing else.
	var config []string
	for _, v := range []struct{ name, dir string }{{executablesVar, *executables}, {calleesVar, *callees}} {
		if v.dir != "" {
			os.Setenv(v.name, v.dir)
			config = append(config, v.name)
		}
	}
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Project, Name: flag.Arg(0)},
		Args:    flag.Args()[1:],
		Dir:     *dir,
		Config:  config,
		Stdin:   exec.Null(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		fail(err)
	}
	defer run.Close()
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
	if res.Outcome != exec.Exited {
		fmt.Fprintf(os.Stderr, "runwith: %s %s\n", flag.Arg(0), res)
		os.Exit(1)
	}
	os.Exit(int(res.Code))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "runwith:", err)
	os.Exit(1)
}
