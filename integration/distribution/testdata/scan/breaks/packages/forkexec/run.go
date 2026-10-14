package forkexec

import sc "syscall"

func start() { sc.ForkExec("x", nil, nil) }
