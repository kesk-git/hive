package exec

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

// The management information base of one process's record: the kernel, its
// processes, the one with an id.
const (
	ctlKern     = 1
	kernProc    = 14
	kernProcPID = 1
)

// identify reads the identity of the process with the id: its id with the
// start time sysctl reports, in microseconds since the epoch. The kernel's
// process record, read with kern.proc.pid, begins with the process's start
// time as a timeval, of 64-bit seconds and 32-bit microseconds.
func identify(pid int) (ProcessID, error) {
	mib := [4]int32{ctlKern, kernProc, kernProcPID, int32(pid)}
	var buf [1024]byte
	size := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, 0)
	if errno != 0 {
		return ProcessID{}, errno
	}
	if size < 16 {
		return ProcessID{}, fmt.Errorf("exec: the process record of %d is not readable", pid)
	}
	sec := binary.LittleEndian.Uint64(buf[0:8])
	usec := binary.LittleEndian.Uint32(buf[8:12])
	return ProcessID{PID: pid, Start: sec*1_000_000 + uint64(usec)}, nil
}
