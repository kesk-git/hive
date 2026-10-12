//go:build unix

package exec

import (
	"os"
	"syscall"
	"unsafe"
)

// ioctl makes the request on the descriptor of f without making f blocking,
// which taking its descriptor with Fd would.
func ioctl(f *os.File, request uintptr, arg unsafe.Pointer) error {
	sc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := sc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// winsize is the structure of TIOCSWINSZ.
type winsize struct {
	Rows, Cols, X, Y uint16
}

// setWindow sets the size of the terminal whose controller side is master.
func setWindow(master *os.File, w Window) error {
	ws := winsize{Rows: w.Rows, Cols: w.Cols}
	return ioctl(master, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

// pty is the controller side of a pseudo-terminal, which is the terminal a
// terminal start's program sees on its standard input, output and error.
type pty struct {
	master *os.File
}

func (p *pty) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *pty) Write(b []byte) (int, error) { return p.master.Write(b) }
func (p *pty) Resize(w Window) error       { return setWindow(p.master, w) }
func (p *pty) Close() error                { return p.master.Close() }
