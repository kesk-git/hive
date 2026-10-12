package exec

import "syscall"

const processQueryLimitedInformation = 0x1000

// identify reads the identity of the process with the id: its id with the
// creation time GetProcessTimes reports.
func identify(pid int) (ProcessID, error) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return ProcessID{}, err
	}
	defer syscall.CloseHandle(h)
	start, err := processStart(h)
	if err != nil {
		return ProcessID{}, err
	}
	return ProcessID{PID: pid, Start: start}, nil
}
