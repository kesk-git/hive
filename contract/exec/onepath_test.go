package exec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The ways Go code starts a process, by any means: the standard library's
// os/exec; os.StartProcess; a process-creation function of syscall, or the
// system call numbers that create one; and a process-creation call of a
// platform's system library, reached by its name through syscall's lazy
// libraries, a dynamic loader or any other call that takes the name.
var (
	startingImports = []string{"os/exec", "golang.org/x/sys/execabs"}
	startingCalls   = map[string][]string{
		"os":      {"StartProcess"},
		"syscall": {"StartProcess", "ForkExec", "Exec", "CreateProcess", "CreateProcessAsUser", "SYS_FORK", "SYS_VFORK", "SYS_EXECVE", "SYS_CLONE", "SYS_CLONE3", "SYS_POSIX_SPAWN"},
		"windows": {"CreateProcess", "CreateProcessAsUser", "CreateProcessWithLogonW", "CreateProcessWithTokenW", "ShellExecute", "ShellExecuteEx"},
		"unix":    {"ForkExec", "Exec", "Execve", "PosixSpawn", "Fork", "Vfork"},
	}
	startingNames = []string{
		"CreateProcessA", "CreateProcessW", "CreateProcessAsUserA", "CreateProcessAsUserW", "CreateProcessWithLogonW",
		"CreateProcessWithTokenW", "ShellExecuteA", "ShellExecuteW", "ShellExecuteExA", "ShellExecuteExW", "WinExec",
		"posix_spawn", "posix_spawnp", "execve", "execv", "execvp", "execl", "execlp", "execle", "fexecve",
	}
	// Names that are also words, and so count only where a call takes them to
	// look up a symbol of a library.
	lookedUpNames = []string{"fork", "vfork", "system", "popen"}
	lookups       = []string{"Dlsym", "NewProc", "NewLazyProc", "FindProc", "MustFindProc", "GetProcAddress", "RegisterLibFunc", "Symbol", "Lookup"}
)

// startsAProcess reads one Go source file and says how it starts a process, in
// lines of the form "name:line: how".
func startsAProcess(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	note := func(pos token.Pos, how string) {
		found = append(found, name+":"+strconv.Itoa(fset.Position(pos).Line)+": "+how)
	}
	local := map[string]string{} // the name an import is known by, to its path
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if slices.Contains(startingImports, path) {
			note(imp.Pos(), "imports "+path)
		}
		base := filepath.Base(path)
		if imp.Name != nil {
			base = imp.Name.Name
		}
		local[base] = path
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			if !ok {
				return true
			}
			path := local[id.Name]
			if path == "" {
				return true
			}
			pkg := filepath.Base(path)
			for _, call := range startingCalls[pkg] {
				if n.Sel.Name == call || pkg == "windows" && strings.HasPrefix(n.Sel.Name, call) {
					note(n.Pos(), "uses "+path+"."+n.Sel.Name)
				}
			}
		case *ast.CallExpr:
			var callee string
			switch f := n.Fun.(type) {
			case *ast.SelectorExpr:
				callee = f.Sel.Name
			case *ast.Ident:
				callee = f.Name
			}
			if !slices.Contains(lookups, callee) {
				return true
			}
			for _, arg := range n.Args {
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && slices.Contains(lookedUpNames, s) {
						note(lit.Pos(), "looks up the call "+s)
					}
				}
			}
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(n.Value); err == nil && slices.Contains(startingNames, s) {
				note(n.Pos(), "names the call "+s)
			}
		}
		return true
	})
	return found, nil
}

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
	}
	for name, src := range bad {
		found, err := startsAProcess(name+".go", []byte(src))
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
		found, err := startsAProcess(name+".go", []byte(src))
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
		found, err := startsAProcess(rel, src)
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
