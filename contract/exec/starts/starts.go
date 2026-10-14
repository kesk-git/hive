// Package starts is the one definition of what starts a process in Go source:
// the rule the architecture check of contract/exec and the distribution proof
// both apply, so that "no other package starts a process" means the same
// thing in both.
package starts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// The ways Go code starts a process, by any means: the standard library's
// os/exec, or anything that wraps it; os.StartProcess; a process-creation
// function of syscall, or the system call numbers that create one; and a
// process-creation call of a platform's system library, reached by its name
// through syscall's lazy libraries, a dynamic loader, cgo or any other call
// that takes the name. A package is known by the last element of its path.
var (
	startingImports = []string{"os/exec", "golang.org/x/sys/execabs", "C"}
	startingCalls   = map[string][]string{
		"os":      {"StartProcess"},
		"syscall": {"StartProcess", "ForkExec", "Exec", "CreateProcess", "CreateProcessAsUser", "SYS_FORK", "SYS_VFORK", "SYS_EXECVE", "SYS_EXECVEAT", "SYS_CLONE", "SYS_CLONE3", "SYS_POSIX_SPAWN"},
		"windows": {"CreateProcess", "CreateProcessAsUser", "CreateProcessWithLogonW", "CreateProcessWithTokenW", "ShellExecute", "ShellExecuteEx"},
		"unix":    {"ForkExec", "Exec", "Execve", "Execveat", "PosixSpawn", "Fork", "Vfork", "SYS_FORK", "SYS_VFORK", "SYS_EXECVE", "SYS_CLONE", "SYS_CLONE3"},
	}
	startingNames = []string{
		"CreateProcessA", "CreateProcessW", "CreateProcessAsUserA", "CreateProcessAsUserW", "CreateProcessWithLogonW",
		"CreateProcessWithTokenW", "ShellExecuteA", "ShellExecuteW", "ShellExecuteExA", "ShellExecuteExW", "WinExec",
		"posix_spawn", "posix_spawnp", "execve", "execv", "execvp", "execl", "execlp", "execle", "fexecve", "execveat",
	}
	// Names that are also words, and so count only where a call takes them to
	// look up a symbol of a library.
	lookedUpNames = []string{"fork", "vfork", "system", "popen", "clone"}
	lookups       = []string{"Dlsym", "NewProc", "NewLazyProc", "FindProc", "MustFindProc", "GetProcAddress", "RegisterLibFunc", "RegisterFunc", "Symbol", "Lookup"}
)

// EvalString reads a string literal, or the concatenation of literals.
func EvalString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return EvalString(e.X)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, okA := EvalString(e.X)
			b, okB := EvalString(e.Y)
			return a + b, okA && okB
		}
	}
	return "", false
}

// ImportPaths maps the name each import of a file is known by to its path,
// and lists the paths imported with a dot.
func ImportPaths(file *ast.File) (local map[string]string, dotted []string) {
	local = map[string]string{}
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch name {
		case ".":
			dotted = append(dotted, p)
		case "_":
		default:
			local[name] = p
		}
	}
	return local, dotted
}

// starting says whether a selector of a package starts a process. The
// functions of golang.org/x/sys/windows come in variants, which count by
// their prefix.
func starting(pkg, name string) bool {
	for _, call := range startingCalls[pkg] {
		if name == call || pkg == "windows" && strings.HasPrefix(name, call) {
			return true
		}
	}
	return false
}

// Find reads one Go source file and says how it starts a process, in lines of
// the form "name:line: how". A file that starts none gives none.
func Find(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	note := func(pos token.Pos, format string, args ...any) {
		found = append(found, fmt.Sprintf("%s:%d: %s", name, fset.Position(pos).Line, fmt.Sprintf(format, args...)))
	}
	local, dotted := ImportPaths(file)
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); slices.Contains(startingImports, p) {
			note(imp.Pos(), "imports %s", p)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok {
				if p := local[id.Name]; p != "" && starting(path.Base(p), n.Sel.Name) {
					note(n.Pos(), "uses %s.%s", p, n.Sel.Name)
				}
			}
		case *ast.Ident:
			// A name imported with a dot stands alone.
			for _, p := range dotted {
				if starting(path.Base(p), n.Name) {
					note(n.Pos(), "uses %s.%s through a dot import", p, n.Name)
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
				if s, ok := EvalString(arg); ok && slices.Contains(lookedUpNames, s) {
					note(arg.Pos(), "looks up the call %s", s)
				}
			}
		case *ast.BasicLit:
			if s, ok := EvalString(n); ok && slices.Contains(startingNames, s) {
				note(n.Pos(), "names the call %s", s)
			}
		case *ast.BinaryExpr:
			// A name built of parts is read whole.
			if s, ok := EvalString(n); ok && slices.Contains(startingNames, s) {
				note(n.Pos(), "names the call %s", s)
			}
		}
		return true
	})
	return found, nil
}
