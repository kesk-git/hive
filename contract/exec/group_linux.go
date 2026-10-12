package exec

import (
	"os"
	"strconv"
	"strings"
)

// siginfo is the siginfo_t that waitid fills for a child on Linux: the union
// after the first three fields is aligned for a pointer.
type siginfo struct {
	Signo  int32
	Errno  int32
	Code   int32
	_      [0]uintptr
	Pid    int32
	Uid    uint32
	Status int32
	_      [128]byte
}

// groupLives says whether a process of the group is left that has not ended.
// Linux counts an ended, unreaped process as one a signal reaches, so the
// processes are read from /proc: the state, field 3 of /proc/<pid>/stat, is
// neither zombie nor dead, and the group, field 5, is the one asked for.
func groupLives(pgid int) (bool, error) {
	d, err := os.Open("/proc")
	if err != nil {
		return false, err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return false, err
	}
	want := strconv.Itoa(pgid)
	for _, name := range names {
		if name[0] < '0' || name[0] > '9' {
			continue
		}
		data, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue
		}
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(s[i+1:])
		if len(fields) > 2 && fields[2] == want && fields[0] != "Z" && fields[0] != "X" {
			return true, nil
		}
	}
	return false, nil
}
