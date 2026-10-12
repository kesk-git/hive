package declare

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
)

// SchemaID is the schema every declaration follows.
const SchemaID = "urn:hive:schema:package:v1"

// The places declarations sit, relative to the repository root.
const (
	FileName    = "hive-package.json"
	PackagesDir = "packages"
	LibraryDir  = "contract"
)

// The kinds of a declaration.
const (
	KindPackage = "package"
	KindLibrary = "library"
)

// ToolProject is the name of an operation's executes member for a project's
// own validation command, which a package declares as executes.project.
const ToolProject = "project-validation"

// Package is one declaration: a package of the seven, or the contract
// library.
type Package struct {
	// Dir is the declaration's directory, relative to the repository root
	// with forward slashes: packages/pool or contract.
	Dir string

	Name         string
	Kind         string
	Title        string
	Owns         string
	PlanStage    int
	Executable   string
	Commands     []Command
	HomePaths    []string
	KindPrefixes []string
	ReceiptKinds []ReceiptKind
	TestTarget   string
	TestScope    string
	Calls        []string
	Executes     Executes
	Platform     []Utility
	Libraries    []Library
	Imports      []string
}

// Command is one command a package owns, with the operations the design
// names; capabilities is every command's and is not listed.
type Command struct {
	Name       string
	Purpose    string
	Operations []string
}

// ReceiptKind is a receipt kind a package has registered.
type ReceiptKind struct {
	Kind       string
	Asserts    string
	BodySchema string
}

// Executes are the external executables a package may run.
type Executes struct {
	Core         []string
	Transitional []string
	Optional     []string
	Project      bool
}

// Utility is a platform utility a package runs, with its reason.
type Utility struct {
	Name   string
	Reason string
}

// Library is a third-party module compiled into a package's executable.
type Library struct {
	Module   string
	ImportID string
}

// Parse reads one declaration. It follows the schema or it is refused; the
// error names where it does not.
func Parse(data []byte) (*Package, error) {
	if err := envelope.Validate(SchemaID, data); err != nil {
		return nil, err
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("declare: a declaration is a JSON object")
	}
	r := reader{m}
	p := &Package{
		Name:         r.str("name"),
		Kind:         r.str("kind"),
		Title:        r.str("title"),
		Owns:         r.str("owns"),
		PlanStage:    int(r.num("plan_stage")),
		Executable:   r.str("executable"),
		HomePaths:    r.strs("home_paths"),
		KindPrefixes: r.strs("kind_prefixes"),
		TestTarget:   r.str("test_target"),
		TestScope:    r.str("test_scope"),
		Calls:        r.strs("calls"),
		Imports:      r.strs("imports"),
	}
	for _, c := range r.objs("commands") {
		cr := reader{c}
		p.Commands = append(p.Commands, Command{cr.str("name"), cr.str("purpose"), cr.strs("operations")})
	}
	for _, c := range r.objs("receipt_kinds") {
		cr := reader{c}
		p.ReceiptKinds = append(p.ReceiptKinds, ReceiptKind{cr.str("kind"), cr.str("asserts"), cr.str("body_schema")})
	}
	ex := reader{r.obj("executes")}
	p.Executes = Executes{ex.strs("core"), ex.strs("transitional"), ex.strs("optional"), ex.boolean("project")}
	for _, u := range r.objs("platform") {
		ur := reader{u}
		p.Platform = append(p.Platform, Utility{ur.str("name"), ur.str("reason")})
	}
	for _, l := range r.objs("libraries") {
		lr := reader{l}
		p.Libraries = append(p.Libraries, Library{lr.str("module"), lr.str("import_id")})
	}
	return p, nil
}

// reader reads members of a decoded object whose shape the schema fixed.
type reader struct{ m map[string]any }

func (r reader) str(k string) string { s, _ := r.m[k].(string); return s }

func (r reader) num(k string) int64 { n, _ := r.m[k].(int64); return n }

func (r reader) boolean(k string) bool { b, _ := r.m[k].(bool); return b }

func (r reader) obj(k string) map[string]any { o, _ := r.m[k].(map[string]any); return o }

func (r reader) strs(k string) []string {
	out := []string{}
	list, _ := r.m[k].([]any)
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (r reader) objs(k string) []map[string]any {
	var out []map[string]any
	list, _ := r.m[k].([]any)
	for _, v := range list {
		if o, ok := v.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

// Set is every declaration of a repository: the shared library's and the
// packages', sorted by name.
type Set struct {
	Library  *Package
	Packages []*Package
}

// All returns the library's declaration, then the packages'.
func (s *Set) All() []*Package {
	var all []*Package
	if s.Library != nil {
		all = append(all, s.Library)
	}
	return append(all, s.Packages...)
}

// Lookup returns the declaration named name, or nil.
func (s *Set) Lookup(name string) *Package {
	for _, p := range s.All() {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Owner returns the package that declares the command, or nil.
func (s *Set) Owner(command string) *Package {
	for _, p := range s.Packages {
		for _, c := range p.Commands {
			if c.Name == command {
				return p
			}
		}
	}
	return nil
}

// Problem is one way the declarations disagree with the specification:
// which rule, where, and what.
type Problem struct {
	Rule    string
	Where   string
	Message string
}

func (p Problem) String() string { return p.Rule + ": " + p.Where + ": " + p.Message }

// Problems is a list of problems, and an error when it is not empty.
type Problems []Problem

func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

// Err returns the list as an error, or nil when it is empty.
func (ps Problems) Err() error {
	if len(ps) == 0 {
		return nil
	}
	return ps
}

// Read reads the declarations of the repository whose root is fsys:
// contract/hive-package.json and packages/<name>/hive-package.json for every
// directory of packages/. The set holds every declaration that was read;
// the problems say which could not be, or sit in the wrong place.
func Read(fsys fs.FS) (*Set, Problems) {
	var problems Problems
	set := &Set{}
	read := func(dir, kind string) *Package {
		file := path.Join(dir, FileName)
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			problems = append(problems, Problem{"declaration", file, err.Error()})
			return nil
		}
		p, err := Parse(data)
		if err != nil {
			problems = append(problems, Problem{"declaration", file, err.Error()})
			return nil
		}
		p.Dir = dir
		if p.Kind != kind {
			problems = append(problems, Problem{"declaration", file, fmt.Sprintf("it is of kind %s; a declaration in %s is of kind %s", p.Kind, dir, kind)})
		}
		return p
	}
	set.Library = read(LibraryDir, KindLibrary)
	entries, err := fs.ReadDir(fsys, PackagesDir)
	if err != nil {
		problems = append(problems, Problem{"declaration", PackagesDir, err.Error()})
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := read(path.Join(PackagesDir, e.Name()), KindPackage)
		if p == nil {
			continue
		}
		if p.Name != e.Name() {
			problems = append(problems, Problem{"declaration", p.Dir, fmt.Sprintf("the directory %s holds the declaration of %s", e.Name(), p.Name)})
		}
		set.Packages = append(set.Packages, p)
	}
	sort.Slice(set.Packages, func(i, j int) bool { return set.Packages[i].Name < set.Packages[j].Name })
	return set, problems
}

// has reports whether list holds s.
func has(list []string, s string) bool { return slices.Contains(list, s) }

// segments splits a name at the characters that separate its words.
func segments(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '.' || r == '/' || r == '_' })
}
