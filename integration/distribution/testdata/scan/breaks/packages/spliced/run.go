package spliced

import "syscall"

var create = syscall.NewLazyDLL("kernel32.dll").NewProc("Create" + "ProcessW")
