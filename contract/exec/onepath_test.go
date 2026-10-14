package exec

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/exec/starts"
)

// Architecture check, not behavioral evidence: the scan below finds every way
// of starting a process in the source it is given, and passes code that
// starts none. That the path starts processes as specified is proved by
// running it, in the tests of this package.
func TestArchitectureScanFindsEveryWayOfStartingAProcess(t *testing.T) {
	bad := map[string]string{
		"os/exec":            `package p; import "os/exec"; var _ = exec.Command`,
		"os/exec renamed":    `package p; import run "os/exec"; var _ = run.Command`,
		"os.StartProcess":    `package p; import "os"; func f() { os.StartProcess("x", nil, nil) }`,
		"syscall.ForkExec":   `package p; import "syscall"; func f() { syscall.ForkExec("x", nil, nil) }`,
		"syscall.Exec":       `package p; import "syscall"; func f() { syscall.Exec("x", nil, nil) }`,
		"CreateProcess":      `package p; import "syscall"; func f() { syscall.CreateProcess(nil, nil, nil, nil, false, 0, nil, nil, nil, nil) }`,
		"the system call":    `package p; import "syscall"; func f() { syscall.Syscall(syscall.SYS_FORK, 0, 0, 0) }`,
		"a lazy library":     `package p; import "syscall"; var _ = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateProcessW")`,
		"a loader":           `package p; func f(sym func(string) uintptr) { sym("posix_spawn") }`,
		"x/sys/windows":      `package p; import "golang.org/x/sys/windows"; func f() { windows.CreateProcessAsUser() }`,
		"ShellExecute":       `package p; import "golang.org/x/sys/windows"; func f() { windows.ShellExecute() }`,
		"x/sys/unix":         `package p; import "golang.org/x/sys/unix"; func f() { unix.Exec("x", nil, nil) }`,
		"os/exec execabs":    `package p; import "golang.org/x/sys/execabs"; var _ = execabs.Command`,
		"an aliased syscall": `package p; import sc "syscall"; func f() { sc.StartProcess("x", nil, nil) }`,
		"cgo":                `package p; import "C"`,
		"a dot import":       `package p; import . "os"; func f() { StartProcess("x", nil, nil) }`,
		"a spliced name":     `package p; import "syscall"; var _ = syscall.NewLazyDLL("k").NewProc("Create" + "ProcessW")`,
		"a registered func":  `package p; func f(reg func(string) uintptr) { reg("posix_" + "spawn") }`,
		"a system call":      `package p; import "golang.org/x/sys/unix"; var _ = unix.SYS_EXECVE`,
	}
	for name, src := range bad {
		found, err := starts.Find(name+".go", []byte(src))
		if err != nil || len(found) == 0 {
			t.Errorf("%s: not found (%v)", name, err)
		}
	}
	good := map[string]string{
		"os":                `package p; import "os"; func f() { os.Getenv("PATH"); os.Exit(1) }`,
		"syscall":           `package p; import "syscall"; func f() { syscall.Getpid(); syscall.Kill(1, 0) }`,
		"a word":            `package p; var _ = "fork the repository"; var _ = "system"+"call"`,
		"a local exec":      `package p; type exec struct{}; func (exec) Command() {}; func f(e exec) { e.Command() }`,
		"a local os":        `package p; func f(os struct{ StartProcess func() }) { os.StartProcess() }`,
		"the other exec":    `package p; import "github.com/kesk-git/hive/contract/exec"; var _ = exec.Start`,
		"net/http":          `package p; import "net/http"; var _ = http.Get`,
		"a different Start": `package p; import "syscall"; var _ = syscall.SYS_GETSID`,
	}
	for name, src := range good {
		found, err := starts.Find(name+".go", []byte(src))
		if err != nil || len(found) != 0 {
			t.Errorf("%s: %v %v", name, found, err)
		}
	}
}

// Architecture check, not behavioral evidence (spec/test-targets.md,
// test-contract): no other package starts a process. No Go file of the
// repository outside contract/exec that is part of a delivered executable
// imports os/exec, calls os.StartProcess or a process-creation function of
// syscall, or reaches a platform's process-creation call by name. Test files
// are not part of an executable and are not read.
func TestArchitectureNoOtherPackageStartsAProcess(t *testing.T) {
	root := filepath.Join("..", "..")
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".git" || rel == "dist" || rel == "contract/exec":
				return filepath.SkipDir
			case d.Name() == "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := starts.Find(rel, src)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Errorf("%s", v)
	}
}
