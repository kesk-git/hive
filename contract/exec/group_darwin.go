package exec

import "syscall"

// siginfo is the siginfo_t that waitid fills for a child on macOS.
type siginfo struct {
	Signo  int32
	Errno  int32
	Code   int32
	Pid    int32
	Uid    uint32
	Status int32
	_      [128]byte
}

// groupLives says whether a process of the group is left that has not ended:
// macOS does not count an ended, unreaped process as one a signal reaches.
func groupLives(pgid int) (bool, error) {
	err := syscall.Kill(-pgid, 0)
	if gone(err) {
		return false, nil
	}
	return err == nil, err
}
