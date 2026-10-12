package exec

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
