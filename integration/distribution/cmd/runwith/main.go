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
// A program is given nothing of this process's environment but what is named:
// each -env name=value, which may be repeated, sets one variable the program
// is started with, besides the search path and the home.
//
// Usage: runwith -path <search path> -home <directory> [-dir <directory>] [-env name=value ...] <program> [argument ...]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kesk-git/hive/contract/exec"
)

// envFlag collects the repeated -env name=value flags.
type envFlag []string

func (e *envFlag) String() string { return strings.Join(*e, " ") }

func (e *envFlag) Set(v string) error {
	if name, _, ok := strings.Cut(v, "="); !ok || name == "" {
		return fmt.Errorf("%q is not name=value", v)
	}
	*e = append(*e, v)
	return nil
}

func main() {
	var extra envFlag
	flag.Var(&extra, "env", "a variable the program is started with, as name=value; may be repeated")
	path := flag.String("path", "", "the search path of the program")
	home := flag.String("home", "", "the home of the program")
	dir := flag.String("dir", "", "the working directory of the program; this process's own when empty")
	flag.Parse()
	if flag.NArg() == 0 || *path == "" || *home == "" {
		fmt.Fprintln(os.Stderr, "usage: runwith -path <search path> -home <directory> [-dir <directory>] [-env name=value ...] <program> [argument ...]")
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
	// The named variables are set here and passed on by name, as the program's
	// own configuration; contract/exec passes nothing else.
	var config []string
	for _, kv := range extra {
		name, value, _ := strings.Cut(kv, "=")
		os.Setenv(name, value)
		config = append(config, name)
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
