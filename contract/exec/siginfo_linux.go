package exec

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
