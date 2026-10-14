package exec

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests run the test binary itself as the recorded fake of a program: it
// is copied into a directory under the name of the program, and when it is
// started with --fake as its first argument it acts as the mode the next
// argument names. It is an executable the test builds, never a script, so
// that the lookup rules find it on all three platforms.
const fakeFlag = "--fake"

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == fakeFlag {
		runFake(os.Args[2:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFake is the fake program. Its modes:
//
//	args        print the arguments after the mode, as a JSON list
//	argv0       print the program's own name as started
//	env         print the environment, one variable a line
//	exit N      exit with code N
//	cwd         print the working directory
//	echo        copy standard input to standard output, untouched
//	bytes       write fixed bytes with line endings of both kinds, then exit
//	hold        print ready and wait to be ended
//	holdterm    print ready, ignore requests to end and wait
//	slowterm    print ready, and on a request to end exit 0 after a pause
//	tree        start a grandchild that holds ignoring requests to end, print
//	            its id once it is ready, and hold ignoring them too
//	selfkill    end itself with a signal it does not handle
//	ids         print the process id, group and session
//	fds         print the descriptors open beyond the standard three
//	tty         report the terminal, then wait for Control-C
//	needstty    exit 1 unless standard input is a terminal
func runFake(args []string) {
	out := os.Stdout
	mode := ""
	if len(args) > 0 {
		mode, args = args[0], args[1:]
	}
	switch mode {
	case "args":
		b, _ := json.Marshal(args)
		fmt.Fprintln(out, string(b))
	case "argv0":
		fmt.Fprintln(out, os.Args[0])
	case "env":
		env := os.Environ()
		sort.Strings(env)
		for _, kv := range env {
			fmt.Fprintln(out, kv)
		}
	case "exit":
		n, _ := strconv.Atoi(args[0])
		os.Exit(n)
	case "cwd":
		wd, _ := os.Getwd()
		fmt.Fprintln(out, wd)
	case "echo":
		io.Copy(out, os.Stdin)
	case "bytes":
		out.Write([]byte("a\r\nb\nc\x00d\r\n"))
	case "hold":
		fmt.Fprintln(out, "ready")
		time.Sleep(time.Hour)
	case "holdterm":
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprintln(out, "ready")
		time.Sleep(time.Hour)
	case "slowterm":
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		fmt.Fprintln(out, "ready")
		<-c
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	case "tree":
		fakeTree(out, args)
	case "selfkill":
		selfKill()
	case "ids":
		fmt.Fprintln(out, fakeIDs())
	case "fds":
		fmt.Fprintln(out, strings.Join(openDescriptors(), " "))
	case "tty":
		fakeTTY(out)
	case "needstty":
		if !isTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, "stdin is not a terminal")
			os.Exit(1)
		}
		fmt.Fprintln(out, "terminal")
	case "starter":
		fakeStarter(args)
	case "writehandle":
		fakeWriteHandle(out, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown fake mode %q\n", mode)
		os.Exit(99)
	}
}

// fakeTree starts a grandchild of the same program that holds ignoring
// requests to end, waits until it is ready, prints its process id, and holds
// ignoring them too, so that only the end ends the tree. With "leave" the
// grandchild leaves the tree, as a daemon does.
func fakeTree(out *os.File, args []string) {
	signal.Ignore(syscall.SIGTERM)
	cmd := exec.Command(os.Args[0], fakeFlag, "holdterm")
	leaveTree(cmd, len(args) > 0 && args[0] == "leave")
	ready, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err == nil {
		_, err = bufio.NewReader(ready).ReadString('\n')
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	fmt.Fprintf(out, "child %d\n", cmd.Process.Pid)
	time.Sleep(time.Hour)
}

// fakeStarter starts a resident of the fake program through this package's
// path, writes its process id to the file the first argument names, and
// exits, so that what it started has outlived the command that started it.
func fakeStarter(args []string) {
	if len(args) > 1 {
		waitForFile(args[1])
	}
	run, err := Start(Spec{
		Program: Program{Kind: Project, Name: strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")},
		Args:    []string{fakeFlag, "hold"},
		Dir:     filepath.Dir(os.Args[0]),
		Start:   ResidentStart,
	})
	if err != nil {
		os.WriteFile(args[0], []byte("error: "+err.Error()), 0o600)
		os.Exit(3)
	}
	os.WriteFile(args[0], []byte(strconv.Itoa(run.ID.PID)), 0o600)
	run.Close()
}

// waitForFile waits until the file exists.
func waitForFile(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// fakeDir copies the test binary into a new directory under each name, with
// the platform's suffix for an executable, and makes that directory the
// search path of Hive's process and of the environment children are built
// from.
func fakeDir(t testing.TB, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		file := name
		if runtime.GOOS == "windows" && filepath.Ext(name) != ".exe" {
			file += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	withPath(t, dir)
	return dir
}

// withPath makes dir the search path, the one Hive's process was started
// with, and the PATH a child's environment copies.
func withPath(t testing.TB, dir string) {
	t.Helper()
	old := startPath
	startPath = dir
	t.Setenv("PATH", dir)
	t.Cleanup(func() { startPath = old })
}

// fake starts the fake program in the mode, in a directory of its own, and
// returns the run.
func fake(t testing.TB, spec Spec, mode string, args ...string) *Run {
	t.Helper()
	run, err := Start(withFake(t, spec, mode, args...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Stop(Grace{}, nil); run.Close() })
	return run
}

// withFake completes a spec to run the fake program, which must be in the
// search path under the name "fakeprog".
func withFake(t testing.TB, spec Spec, mode string, args ...string) Spec {
	t.Helper()
	spec.Program = Program{Kind: Project, Name: "fakeprog"}
	spec.Args = append([]string{fakeFlag, mode}, args...)
	if spec.Dir == "" {
		spec.Dir = t.TempDir()
	}
	return spec
}

// output starts the fake with its output as a pipe, reads it whole and waits.
func output(t testing.TB, spec Spec, mode string, args ...string) (string, Result) {
	t.Helper()
	spec.Stdout = Pipe()
	run := fake(t, spec, mode, args...)
	data, err := io.ReadAll(run.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	return string(data), run.Wait()
}

// readLine reads one line from r within a bound.
func readLine(t testing.TB, r io.Reader) string {
	t.Helper()
	type res struct {
		s   string
		err error
	}
	c := make(chan res, 1)
	go func() {
		s, err := bufio.NewReader(r).ReadString('\n')
		c <- res{s, err}
	}()
	select {
	case v := <-c:
		if v.err != nil && v.s == "" {
			t.Fatal(v.err)
		}
		return strings.TrimRight(v.s, "\r\n")
	case <-time.After(20 * time.Second):
		t.Fatal("no line within 20 seconds")
	}
	return ""
}

func hexOf(s string) string { return hex.EncodeToString([]byte(s)) }

// waitGone waits for the process to be gone.
func waitGone(t testing.TB, id ProcessID) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if !alive(id) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", id.PID)
}
