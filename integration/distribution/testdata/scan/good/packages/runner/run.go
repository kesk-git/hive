package runner

import "example.invalid/fix/contract/exec"

const gitName = "git"

// Run starts Git, a command of a called package, a platform utility and the
// project's own command, each as the declaration lists.
func Run(project string) []exec.Spec {
	git := exec.Program{Kind: exec.Core, Name: gitName}
	return []exec.Spec{
		{Program: git},
		{Program: exec.Program{Kind: exec.Hive, Name: "hive-other"}},
		{Program: exec.Program{Kind: exec.Platform, Name: "security"}},
		{Program: exec.Program{Kind: exec.Project, Name: project}},
	}
}
