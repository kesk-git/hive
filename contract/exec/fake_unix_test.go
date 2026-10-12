//go:build unix

package exec

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"

	"syscall"
	"time"
	"unsafe"
)

// leaveTree makes the command start in a session of its own, as a daemon does.
func leaveTree(cmd *exec.Cmd, leave bool) {
	if leave {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
}

// selfKill ends the process with SIGKILL, which it cannot handle.
func selfKill() {
	syscall.Kill(os.Getpid(), syscall.SIGKILL)
	time.Sleep(time.Hour)
}

// fakeIDs reports the process, its group and its session.
func fakeIDs() string {
	sid, _ := getsid(0)
	return fmt.Sprintf("pid=%d pgid=%d sid=%d", os.Getpid(), syscall.Getpgrp(), sid)
}

// openDescriptors lists the descriptors open beyond the standard three.
func openDescriptors() []string {
	var open []string
	for fd := 3; fd < 256; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil {
			open = append(open, fmt.Sprint(fd))
		}
	}
	return open
}

func isTerminal(f *os.File) bool {
	var ws [4]uint16
	return ioctlWinsize(f, &ws) == nil
}

func ioctlWinsize(f *os.File, ws *[4]uint16) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(ws)))
	if errno != 0 {
		return errno
	}
	return nil
}

// fakeTTY reports whether it has a terminal, its size and that it is the
// leader of a session with that terminal as its controlling terminal, then
// prints the size again at each change and reports a Control-C as an
// interrupt.
func fakeTTY(out *os.File) {
	sig := make(chan os.Signal, 4)
	notify(sig) // before the first line, which is what the test waits for
	var ws [4]uint16
	err := ioctlWinsize(os.Stdin, &ws)
	sid, _ := getsid(0)
	tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if tty != nil {
		tty.Close()
	}
	fmt.Fprintf(out, "tty=%v rows=%d cols=%d leader=%v controlling=%v\r\n", err == nil, ws[0], ws[1], sid == os.Getpid(), ttyErr == nil)
	waitTerminal(out, sig)
}

func waitTerminal(out *os.File, sig chan os.Signal) {
	for s := range sig {
		switch s {
		case syscall.SIGINT:
			fmt.Fprint(out, "interrupt\r\n")
			os.Exit(3)
		case syscall.SIGWINCH:
			var ws [4]uint16
			ioctlWinsize(os.Stdin, &ws)
			fmt.Fprintf(out, "rows=%d cols=%d\r\n", ws[0], ws[1])
		case syscall.SIGHUP:
			os.Exit(4)
		}
	}
}

func notify(c chan os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGWINCH, syscall.SIGHUP)
}

// getsid is the session of the process, which the syscall package names on
// macOS only.
func getsid(pid int) (int, error) {
	r, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

// fakeWriteHandle has nothing to do on macOS and Linux: the descriptors a
// child inherits are listed by fds.
func fakeWriteHandle(out *os.File, args []string) {}
