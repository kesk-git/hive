package exec

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	testKernel32           = syscall.NewLazyDLL("kernel32.dll")
	procRaiseException     = testKernel32.NewProc("RaiseException")
	procGetConsoleMode     = testKernel32.NewProc("GetConsoleMode")
	procGetConsoleScreen   = testKernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetErrorMode       = testKernel32.NewProc("SetErrorMode")
	errorModeNoFaultDialog = uintptr(0x0002 | 0x8000)
)

// leaveTree makes the command start outside the job of its starter, as a
// descendant that asks to leave does.
func leaveTree(cmd *exec.Cmd, leave bool) {
	if leave {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createBreakawayFromJob}
	}
}

// selfKill ends the process with an exception status it does not handle,
// which Windows reports as the status of the process.
func selfKill() {
	procSetErrorMode.Call(errorModeNoFaultDialog)
	procRaiseException.Call(0xC0000999, 1, 0, 0)
	time.Sleep(time.Hour)
}

func fakeIDs() string { return fmt.Sprintf("pid=%d", os.Getpid()) }

// heldIdentities has nothing to report on Windows: the handle a child
// inherits is tested by writing to it (see writehandle).
func heldIdentities(want []string) []string { return nil }

func isTerminal(f *os.File) bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// consoleSize is the size of the console buffer's window.
func consoleSize(f *os.File) (rows, cols int, ok bool) {
	var info struct {
		SizeX, SizeY             int16
		CursorX, CursorY         int16
		Attributes               uint16
		Left, Top, Right, Bottom int16
		MaxX, MaxY               int16
	}
	r, _, _ := procGetConsoleScreen.Call(f.Fd(), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0, false
	}
	return int(info.Bottom-info.Top) + 1, int(info.Right-info.Left) + 1, true
}

// fakeTTY reports whether it has a console and its size, then reports each
// change of size and a Control-C as an interrupt.
func fakeTTY(out *os.File) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt) // before the first line, which is what the test waits for
	rows, cols, ok := consoleSize(os.Stdout)
	fmt.Fprintf(out, "tty=%v rows=%d cols=%d leader=true controlling=%v\r\n", isTerminal(os.Stdin), rows, cols, ok)
	tick := time.NewTicker(50 * time.Millisecond)
	for {
		select {
		case <-sig:
			fmt.Fprint(out, "interrupt\r\n")
			os.Exit(3)
		case <-tick.C:
			if r, c, ok := consoleSize(os.Stdout); ok && (r != rows || c != cols) {
				rows, cols = r, c
				fmt.Fprintf(out, "rows=%d cols=%d\r\n", rows, cols)
			}
		}
	}
}

// fakeWriteHandle tries to write to the handle whose value the argument
// names, as a leak of a handle Hive holds would let it, and reports whether
// the write was accepted.
func fakeWriteHandle(out *os.File, args []string) {
	n, _ := strconv.ParseUint(args[0], 10, 64)
	var written uint32
	err := syscall.WriteFile(syscall.Handle(n), []byte("leaked"), &written, nil)
	fmt.Fprintf(out, "written=%d err=%v\n", written, err)
}

// fakeTermHandles reports whether each of its three standard handles is a
// console, writes a line to standard error, and waits to be ended.
func fakeTermHandles(out *os.File, args []string) {
	fmt.Fprintf(out, "stdin=%v stdout=%v stderr=%v\r\n", isTerminal(os.Stdin), isTerminal(os.Stdout), isTerminal(os.Stderr))
	fmt.Fprint(os.Stderr, "written-to-stderr\r\n")
	time.Sleep(time.Hour)
}
