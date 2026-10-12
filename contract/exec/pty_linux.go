package exec

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY opens the pseudo-terminal controller, /dev/ptmx, read-write and
// without making it a controlling terminal; unlocks it with TIOCSPTLCK and
// reads the number of its terminal side with TIOCGPTN. It returns the
// controller and the terminal side opened the same way.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("exec: opening /dev/ptmx: %w", err)
	}
	fail := func(step string, err error) (*os.File, *os.File, error) {
		master.Close()
		return nil, nil, fmt.Errorf("exec: %s: %w", step, err)
	}
	var unlock int32
	if err := ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		return fail("unlocking the terminal", err)
	}
	var n uint32
	if err := ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		return fail("reading the terminal's number", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fail("opening the terminal side", err)
	}
	return master, slave, nil
}
