//go:build unix

package exec

import (
	"os"
	"strconv"
	"syscall"
)

// closeInherited marks every descriptor Hive holds beyond the standard three
// close-on-exec, so that none reaches a child: Go opens its own descriptors
// close-on-exec, but one Hive was started with, a pipe or a file its starter
// left open, is not, and the child would inherit it. A descriptor the start
// passes on is duplicated onto the child's own, whatever its flag.
func closeInherited() {
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
				syscall.CloseOnExec(fd)
			}
		}
		return
	}
	var r syscall.Rlimit
	max := 1024
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &r); err == nil && r.Cur < 1<<16 {
		max = int(r.Cur)
	}
	for fd := 3; fd < max; fd++ {
		syscall.CloseOnExec(fd)
	}
}
