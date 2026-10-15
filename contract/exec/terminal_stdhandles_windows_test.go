package exec

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func init() { extraFakes["termhandles"] = fakeTermHandles }

var procSetStdHandle = testKernel32.NewProc("SetStdHandle")

// setStdHandle sets one of the process's standard handles, which the child of
// a start that sets none is given.
func setStdHandle(id int, h syscall.Handle) error {
	r, _, err := procSetStdHandle.Call(uintptr(id), uintptr(h))
	if r == 0 {
		return err
	}
	return nil
}

// fakeTermHandles reports whether each of its three standard handles is a
// console, writes a line to standard error, and waits to be ended.
func fakeTermHandles(out *os.File, args []string) {
	fmt.Fprintf(out, "stdin=%v stdout=%v stderr=%v\r\n", isTerminal(os.Stdin), isTerminal(os.Stdout), isTerminal(os.Stderr))
	fmt.Fprint(os.Stderr, "written-to-stderr\r\n")
	time.Sleep(time.Hour)
}

// parentPipes makes the standard handles of this process pipes, as they are
// for a parent started by a program that captures its output, and gives the
// reading ends of the pipes that standard output and standard error write to.
type parentPipes struct {
	t     *testing.T
	ids   [3]int
	saved [3]syscall.Handle
	ends  [3][2]syscall.Handle // read and write end of each pipe
	once  sync.Once
}

func pipeStandardHandles(t *testing.T) *parentPipes {
	t.Helper()
	p := &parentPipes{t: t, ids: [3]int{syscall.STD_INPUT_HANDLE, syscall.STD_OUTPUT_HANDLE, syscall.STD_ERROR_HANDLE}}
	sa := &syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), InheritHandle: 1}
	for i, id := range p.ids {
		saved, err := syscall.GetStdHandle(id)
		if err != nil {
			t.Fatal(err)
		}
		p.saved[i] = saved
		if err := syscall.CreatePipe(&p.ends[i][0], &p.ends[i][1], sa, 0); err != nil {
			t.Fatal(err)
		}
		std := p.ends[i][1] // the parent writes to its output and error
		if i == 0 {
			std = p.ends[i][0] // and reads its input
		}
		if err := setStdHandle(id, std); err != nil {
			t.Fatal(err)
		}
		if got, _ := syscall.GetFileType(std); got != syscall.FILE_TYPE_PIPE {
			t.Fatalf("standard handle %d is not a pipe (type %d)", i, got)
		}
	}
	t.Cleanup(func() { p.restore() })
	return p
}

// restore gives the process its own standard handles back and closes the
// pipes, but for the reading ends it is going to read.
func (p *parentPipes) restore() {
	p.once.Do(func() {
		for i, id := range p.ids {
			setStdHandle(id, p.saved[i])
			syscall.CloseHandle(p.ends[i][1])
		}
		syscall.CloseHandle(p.ends[0][0])
	})
}

// expectEmpty restores the standard handles and requires that nothing was
// written to the pipes of standard output and standard error while they were
// the parent's: what the child wrote went to its terminal.
func (p *parentPipes) expectEmpty() {
	p.t.Helper()
	p.restore()
	for _, i := range []int{1, 2} {
		name := map[int]string{1: "output", 2: "error"}[i]
		f := os.NewFile(uintptr(p.ends[i][0]), name)
		got := make(chan []byte, 1)
		go func() { data, _ := io.ReadAll(f); got <- data }()
		select {
		case data := <-got:
			if len(data) != 0 {
				p.t.Errorf("the child wrote %q to the parent's standard %s pipe", data, name)
			}
		case <-time.After(10 * time.Second):
			p.t.Errorf("the parent's standard %s pipe did not end", name)
		}
		f.Close()
	}
}

// A terminal start from a parent whose own standard handles are pipes gives
// the child the pseudoconsole as its standard handles: they are consoles,
// what it writes arrives through the pseudoconsole and not in the parent's
// pipes, and a resize and an interrupt reach it.
func TestTerminalStartFromAParentWithPipedStandardHandles(t *testing.T) {
	fakeDir(t, "fakeprog")
	pipes := pipeStandardHandles(t)

	handles := fake(t, Spec{Start: TerminalStart}, "termhandles")
	got := readUntil(t, handles.Terminal(), "written-to-stderr")
	if !strings.Contains(got, "stdin=true stdout=true stderr=true") {
		t.Errorf("the child's standard handles are not all terminals: %q", got)
	}
	if res, err := handles.Stop(Grace{}, nil); err != nil || res.Outcome == "" {
		t.Errorf("%+v %v", res, err)
	}

	run := fake(t, Spec{Start: TerminalStart, Window: Window{Rows: 30, Cols: 100}}, "tty")
	term := run.Terminal()
	got = readUntil(t, term, "controlling=")
	for _, want := range []string{"tty=true", "rows=30", "cols=100", "controlling=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not hold %s", got, want)
		}
	}
	if err := term.Resize(Window{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, "rows=40 cols=120")
	if _, err := term.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, "interrupt")
	if res := run.Wait(); res.Outcome != Exited || res.Code != 3 {
		t.Errorf("%v", res)
	}

	pipes.expectEmpty()
}
