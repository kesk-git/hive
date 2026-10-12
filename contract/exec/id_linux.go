package exec

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// identify reads the identity of the process with the id: its id with the
// start time /proc reports, in clock ticks since boot (field 22 of
// /proc/<pid>/stat), read in process.
func identify(pid int) (ProcessID, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ProcessID{}, err
	}
	// The command name, field 2, is in parentheses and may hold anything;
	// the rest starts after the last closing one.
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return ProcessID{}, fmt.Errorf("exec: /proc/%d/stat is not readable", pid)
	}
	fields := strings.Fields(s[i+1:])
	// fields[0] is field 3, the state; the start time is field 22.
	if len(fields) < 20 {
		return ProcessID{}, fmt.Errorf("exec: /proc/%d/stat is short", pid)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcessID{}, err
	}
	return ProcessID{PID: pid, Start: start}, nil
}
