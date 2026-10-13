package proof

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// The ways Go code starts a process, by any means, for How it is proved item
// 3: an import of the standard library's os/exec, or of anything that wraps
// it; a call of os.StartProcess; a process-creation function of syscall, or
// the system call numbers that create a process; and a process-creation call
// of a platform's own system library, reached by its name through a lazy
// library, a dynamic loader or cgo.

// startingFile lists the process-creation calls of the standard library and of
// a platform's system libraries, one a line: "call <package path> <name>" for a
// selector of a package, and "name <name>" for the name of a library's call,
// which counts wherever it appears as a string in a source file. They are data
// of this package, kept in a file so that the source of this package does not
// name them: the scan reads every source file of the repository, this
// package's own among them.
//
//go:embed starting.txt
var startingFile string

// startingCalls maps a package path to the names of its selectors that start
// a process, and startingNames lists the names of a library's calls, both
// read from startingFile.
var startingCalls, startingNames = readStarting(startingFile)

func readStarting(text string) (map[string][]string, []string) {
	calls := map[string][]string{}
	var names []string
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "call":
			calls[f[1]] = append(calls[f[1]], f[2])
		case len(f) == 2 && f[0] == "name":
			names = append(names, f[1])
		}
	}
	return calls, names
}

var (
	startingImports = []string{"os/exec", "golang.org/x/sys/execabs", "C"}

	// lookedUpNames are names that are also words, and count only where a call
	// takes them to look up a symbol of a library.
	lookedUpNames = []string{"fork", "vfork", "system", "popen", "clone"}
	lookups       = []string{"Dlsym", "NewProc", "NewLazyProc", "FindProc", "MustFindProc", "GetProcAddress", "RegisterLibFunc", "RegisterFunc", "Symbol", "Lookup"}
)

// evalString reads a string literal, or the concatenation of literals.
func evalString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return evalString(e.X)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, okA := evalString(e.X)
			b, okB := evalString(e.Y)
			return a + b, okA && okB
		}
	}
	return "", false
}

// importPaths maps the name each import of a file is known by to its path,
// and lists the paths imported with a dot.
func importPaths(file *ast.File) (local map[string]string, dotted []string) {
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

// StartsAProcess reads one Go source file and says how it starts a process,
// in lines of the form "name:line: how". A file that starts none gives none.
func StartsAProcess(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	note := func(pos token.Pos, format string, args ...any) {
		found = append(found, fmt.Sprintf("%s:%d: %s", name, fset.Position(pos).Line, fmt.Sprintf(format, args...)))
	}
	local, dotted := importPaths(file)
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); slices.Contains(startingImports, p) {
			note(imp.Pos(), "imports %s", p)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok {
				if p := local[id.Name]; slices.Contains(startingCalls[p], n.Sel.Name) {
					note(n.Pos(), "uses %s.%s", p, n.Sel.Name)
				}
			}
		case *ast.Ident:
			// A name imported with a dot stands alone.
			for _, p := range dotted {
				if slices.Contains(startingCalls[p], n.Name) && n.Obj == nil {
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
				if s, ok := evalString(arg); ok && slices.Contains(lookedUpNames, s) {
					note(arg.Pos(), "looks up the call %s", s)
				}
			}
		case *ast.BasicLit:
			if s, ok := evalString(n); ok && slices.Contains(startingNames, s) {
				note(n.Pos(), "names the call %s", s)
			}
		case *ast.BinaryExpr:
			// A name built of parts is read whole.
			if s, ok := evalString(n); ok && slices.Contains(startingNames, s) {
				note(n.Pos(), "names the call %s", s)
			}
		}
		return true
	})
	return found, nil
}

// Starts is How it is proved item 3: no Go source file of the module outside
// contract/exec starts a process by any means. Test files are not part of an
// executable and are not read, and neither are the directories no scan reads.
func Starts(root string) ([]string, error) {
	var found []string
	err := walkFiles(root, func(rel string) error {
		if !sourceFile(rel) || rel == execPackage || strings.HasPrefix(rel, execPackage+"/") {
			return nil
		}
		src, err := readFile(root, rel)
		if err != nil {
			return err
		}
		f, err := StartsAProcess(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		found = append(found, f...)
		return nil
	})
	return found, err
}
