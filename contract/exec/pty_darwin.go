package exec

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY opens the pseudo-terminal controller, /dev/ptmx, read-write and
// without making it a controlling terminal; grants and unlocks it and reads
// the name of its terminal side through the platform's own ioctl requests,
// TIOCPTYGRANT, TIOCPTYUNLK and TIOCPTYGNAME. It returns the controller and
// the terminal side opened the same way.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("exec: opening /dev/ptmx: %w", err)
	}
	fail := func(step string, err error) (*os.File, *os.File, error) {
		master.Close()
		return nil, nil, fmt.Errorf("exec: %s: %w", step, err)
	}
	if err := ioctl(master, syscall.TIOCPTYGRANT, nil); err != nil {
		return fail("granting the terminal", err)
	}
	if err := ioctl(master, syscall.TIOCPTYUNLK, nil); err != nil {
		return fail("unlocking the terminal", err)
	}
	var name [128]byte
	if err := ioctl(master, syscall.TIOCPTYGNAME, unsafe.Pointer(&name[0])); err != nil {
		return fail("reading the terminal's name", err)
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	slave, err = os.OpenFile(string(name[:n]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fail("opening the terminal side", err)
	}
	return master, slave, nil
}
