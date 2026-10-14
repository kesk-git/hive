package lazydll

import "syscall"

var create = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateProcessW")
