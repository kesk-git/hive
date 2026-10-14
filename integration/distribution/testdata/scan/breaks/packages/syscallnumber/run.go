package syscallnumber

import "syscall"

func start() { syscall.Syscall(syscall.SYS_FORK, 0, 0, 0) }
