package flushseam

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const seamPath = "github.com/kesk-git/hive/contract/internal/flushseam"

// Only test files turn the real flush off: of the files a delivered
// executable is built from, only the store imports the package, and none
// writes Skip or takes its address.
func TestOnlyTestFilesSetTheSeam(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	found := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		name := ""
		for _, imp := range file.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == seamPath {
				name = "flushseam"
				if imp.Name != nil {
					name = imp.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		found++
		if !strings.HasSuffix(filepath.ToSlash(filepath.Dir(p)), "/contract/store") {
			t.Errorf("%s: a delivered file other than the store's imports the seam", p)
		}
		isSkip := func(e ast.Expr) bool {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			x, ok := sel.X.(*ast.Ident)
			return ok && x.Name == name && sel.Sel.Name == "Skip"
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if isSkip(l) {
						t.Errorf("%s: a delivered file assigns the seam", p)
					}
				}
			case *ast.IncDecStmt:
				if isSkip(n.X) {
					t.Errorf("%s: a delivered file changes the seam", p)
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND && isSkip(n.X) {
					t.Errorf("%s: a delivered file takes the address of the seam", p)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Error("no delivered file reads the seam")
	}
}
