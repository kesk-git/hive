// Package exec is the one package that starts processes.
package exec

import (
	"os"
	"os/exec"
	"syscall"
)

var _ = exec.Command
var _ = os.StartProcess
var _ = syscall.ForkExec

// Kinds of program.
const (
	Core         = "core"
	Transitional = "transitional"
	Hive         = "hive"
	Platform     = "platform"
)

// Program is a program by kind and name.
type Program struct{ Kind, Name string }

// Spec is one run.
type Spec struct{ Program Program }
