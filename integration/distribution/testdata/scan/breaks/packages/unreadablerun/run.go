package unreadablerun

import "example.invalid/fix/contract/exec"

func pick() exec.Program { return exec.Program{Kind: exec.Core, Name: "git"} }

var run = exec.Spec{Program: pick()}
