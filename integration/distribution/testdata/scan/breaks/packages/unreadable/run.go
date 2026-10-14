package unreadable

import (
	"os"

	"example.invalid/fix/contract/exec"
)

var run = exec.Spec{Program: exec.Program{Kind: exec.Core, Name: os.Getenv("PROGRAM")}}
