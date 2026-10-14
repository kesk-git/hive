package proof

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/exec/starts"
)

// declaration is the part of a package declaration these checks read.
type declaration struct {
	Name     string `json:"name"`
	Commands []struct {
		Name string `json:"name"`
	} `json:"commands"`
	Calls    []string `json:"calls"`
	Executes struct {
		Core         []string `json:"core"`
		Transitional []string `json:"transitional"`
		Optional     []string `json:"optional"`
		Project      bool     `json:"project"`
	} `json:"executes"`
	Platform []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"platform"`
}

// declarations reads the declaration of each package and of the contract
// library: packages/<name>/hive-package.json and contract/hive-package.json.
func declarations(root string) (map[string]*declaration, error) {
	files, err := filepath.Glob(filepath.Join(root, "packages", "*", "hive-package.json"))
	if err != nil {
		return nil, err
	}
	files = append(files, filepath.Join(root, "contract", "hive-package.json"))
	out := map[string]*declaration{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var d declaration
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[d.Name] = &d
	}
	return out, nil
}

// owner is the declaration a source directory belongs to: the package under
// packages/, or the contract library, or none.
func owner(dir string) string {
	parts := strings.Split(dir, "/")
	switch {
	case parts[0] == "packages" && len(parts) > 1:
		return parts[1]
	case parts[0] == "contract":
		return "contract"
	}
	return ""
}

// modulePath reads the module's own path from go.mod.
func modulePath(root string) (string, error) {
	data, err := readFile(root, "go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
			return f[1], nil
		}
	}
	return "", fmt.Errorf("go.mod names no module")
}

// goPackage is the parsed source of one directory.
type goPackage struct {
	dir   string
	fset  *token.FileSet
	files map[string]*ast.File
	// consts are the package-level string constants, by name.
	consts map[string]string
}

// parseTree parses every Go source file the scan reads, by directory.
func parseTree(root string) (map[string]*goPackage, error) {
	pkgs := map[string]*goPackage{}
	err := walkFiles(root, func(rel string) error {
		if !sourceFile(rel) {
			return nil
		}
		src, err := readFile(root, rel)
		if err != nil {
			return err
		}
		dir := path.Dir(rel)
		p := pkgs[dir]
		if p == nil {
			p = &goPackage{dir: dir, fset: token.NewFileSet(), files: map[string]*ast.File{}, consts: map[string]string{}}
			pkgs[dir] = p
		}
		f, err := parser.ParseFile(p.fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		p.files[rel] = f
		return nil
	})
	for _, p := range pkgs {
		for _, f := range p.files {
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.CONST {
					continue
				}
				for _, s := range g.Specs {
					v := s.(*ast.ValueSpec)
					for i, n := range v.Names {
						if i < len(v.Values) {
							if s, ok := starts.EvalString(v.Values[i]); ok {
								p.consts[n.Name] = s
							}
						}
					}
				}
			}
		}
	}
	return pkgs, err
}

// resolver reads the values of names in the source of one file.
type resolver struct {
	module string
	pkgs   map[string]*goPackage
	pkg    *goPackage
	local  map[string]string // import name to path
}

// str reads an expression that names a string: a literal, a concatenation, a
// constant of the same package or a constant of another package of the module.
func (r resolver) str(e ast.Expr) (string, bool) {
	if s, ok := starts.EvalString(e); ok {
		return s, true
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return r.str(e.X)
	case *ast.Ident:
		s, ok := r.pkg.consts[e.Name]
		return s, ok
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if p := r.local[id.Name]; strings.HasPrefix(p, r.module+"/") {
				if other := r.pkgs[strings.TrimPrefix(p, r.module+"/")]; other != nil {
					s, ok := other.consts[e.Sel.Name]
					return s, ok
				}
			}
		}
	}
	return "", false
}

// The kinds a run of contract/exec names its program by, and the field of
// the declaration that lists the programs of each.
var kinds = []string{"Core", "Transitional", "Optional", "Project", "Hive", "Platform"}

// kind reads the kind of a program: exec.Core and the like.
func (r resolver) kind(e ast.Expr, execName string) (string, bool) {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == execName && slices.Contains(kinds, sel.Sel.Name) {
			return sel.Sel.Name, true
		}
	}
	if s, ok := starts.EvalString(e); ok {
		for _, k := range kinds {
			if strings.EqualFold(s, k) {
				return k, true
			}
		}
	}
	return "", false
}

// allowed says whether a package's declaration lists the program of the kind
// and name: Core, Transitional and Optional by the names under executes, a
// project's command where executes.project is set, another Hive command where
// the package declares the call, and a platform utility under platform.
func allowed(d *declaration, all map[string]*declaration, kind, name string) bool {
	switch kind {
	case "Core":
		return slices.Contains(d.Executes.Core, name)
	case "Transitional":
		return slices.Contains(d.Executes.Transitional, name)
	case "Optional":
		return slices.Contains(d.Executes.Optional, name)
	case "Project":
		return d.Executes.Project
	case "Hive":
		for _, called := range d.Calls {
			if c := all[called]; c != nil {
				for _, cmd := range c.Commands {
					if cmd.Name == name {
						return true
					}
				}
			}
		}
		return false
	case "Platform":
		for _, p := range d.Platform {
			if p.Name == name {
				return true
			}
		}
	}
	return false
}

// Programs is How it is proved item 4: no package starts a program its
// declaration does not list, found by the kinds passed to contract/exec. It
// reads every program named in a source file of a package or of the contract
// library outside contract/exec: the kind and the name of each exec.Program
// literal, and refuses a program whose kind or name it cannot read, since an
// unreadable name cannot be shown listed.
func Programs(root string) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	decls, err := declarations(root)
	if err != nil {
		return nil, err
	}
	pkgs, err := parseTree(root)
	if err != nil {
		return nil, err
	}
	execImport := module + "/" + execPackage
	var dirs []string
	for d := range pkgs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var found []string
	for _, dir := range dirs {
		pkg := pkgs[dir]
		own := owner(dir)
		if own == "" || dir == execPackage || strings.HasPrefix(dir, execPackage+"/") {
			continue
		}
		var names []string
		for n := range pkg.files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			file := pkg.files[name]
			local, _ := starts.ImportPaths(file)
			execName := ""
			for n, p := range local {
				if p == execImport {
					execName = n
				}
			}
			if execName == "" {
				continue
			}
			r := resolver{module: module, pkgs: pkgs, pkg: pkg, local: local}
			at := func(p token.Pos) string { return fmt.Sprintf("%s:%d", name, pkg.fset.Position(p).Line) }
			d := decls[own]
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Name != execName {
					return true
				}
				switch sel.Sel.Name {
				case "Spec":
					// The program of a run is a literal, or a name bound to one.
					for _, el := range lit.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Program" {
							switch kv.Value.(type) {
							case *ast.CompositeLit, *ast.Ident:
							default:
								found = append(found, at(kv.Pos())+": the program of this run is not a literal or a name the scan can follow")
							}
						}
					}
				case "Program":
					var kindExpr, nameExpr ast.Expr
					for i, el := range lit.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							key, _ := kv.Key.(*ast.Ident)
							switch {
							case key != nil && key.Name == "Kind":
								kindExpr = kv.Value
							case key != nil && key.Name == "Name":
								nameExpr = kv.Value
							}
						} else if i == 0 {
							kindExpr = el
						} else if i == 1 {
							nameExpr = el
						}
					}
					var k, nm string
					var okK, okN bool
					if kindExpr != nil {
						k, okK = r.kind(kindExpr, execName)
					}
					if nameExpr != nil {
						nm, okN = r.str(nameExpr)
					}
					switch {
					case !okK:
						found = append(found, at(lit.Pos())+": the kind of this program cannot be read")
					case !okN && k != "Project":
						found = append(found, at(lit.Pos())+fmt.Sprintf(": the name of this %s program cannot be read", k))
					case d == nil:
						found = append(found, at(lit.Pos())+fmt.Sprintf(": starts a %s program in %s, which has no declaration", k, dir))
					case !allowed(d, decls, k, nm):
						found = append(found, at(lit.Pos())+fmt.Sprintf(": starts the %s program %q, which the declaration of %s does not list", strings.ToLower(k), nm, own))
					}
				}
				return true
			})
		}
	}
	return found, nil
}

// five are the programs of the five dependencies.
var five = []string{"git", "gh", "claude", "codex", "no-mistakes"}

var bareWord = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// PlatformUtilities is How it is proved item 5: no platform list is non-empty
// without a reason, and none names a program that is one of the five.
func PlatformUtilities(root string) ([]string, error) {
	decls, err := declarations(root)
	if err != nil {
		return nil, err
	}
	var names []string
	for n := range decls {
		names = append(names, n)
	}
	sort.Strings(names)
	var found []string
	for _, n := range names {
		for _, u := range decls[n].Platform {
			switch {
			case strings.TrimSpace(u.Reason) == "":
				found = append(found, fmt.Sprintf("%s lists the platform utility %q without a reason", n, u.Name))
			case slices.Contains(five, strings.ToLower(strings.TrimSuffix(u.Name, ".exe"))):
				found = append(found, fmt.Sprintf("%s lists %q under platform, and that is one of the five", n, u.Name))
			case !bareWord.MatchString(u.Name):
				found = append(found, fmt.Sprintf("%s lists %q under platform, which is not a program's bare name", n, u.Name))
			}
		}
	}
	return found, nil
}
