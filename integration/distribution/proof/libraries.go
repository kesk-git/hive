package proof

import (
	"encoding/json"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// keyring is the first named exception of the toolchain
// (spec/distribution.md, The toolchain): it is built into the Windows and
// Linux executables only. macOS keeps its keychain route through security.
const keyring = "github.com/zalando/go-keyring"

// record is the part of an import record these checks read.
type record struct {
	ImportID string `json:"import_id"`
	Kind     string `json:"kind"`
	Upstream struct {
		Module *struct {
			Path string `json:"path"`
		} `json:"module"`
	} `json:"upstream"`
	LocalChanges string `json:"local_changes"`
	Audit        struct {
		Statement string `json:"statement"`
	} `json:"audit"`
}

// importRecords reads the records under imports/, by import_id.
func importRecords(root string) (map[string]record, error) {
	files, err := filepath.Glob(filepath.Join(root, "imports", "*.json"))
	if err != nil {
		return nil, err
	}
	out := map[string]record{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var r record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[r.ImportID] = r
	}
	return out, nil
}

// pins says whether the record is the dependency record of the module.
func (r record) pins(module string) bool {
	return r.Kind == "dependency" && r.Upstream.Module != nil && r.Upstream.Module.Path == module
}

// listed is the library of a declaration that an import path belongs to: the
// module itself or a package of it. A package with no declaration lists none.
func (d *declaration) listed(importPath string) (library, bool) {
	if d == nil {
		return library{}, false
	}
	for _, l := range d.Libraries {
		if importPath == l.Module || strings.HasPrefix(importPath, l.Module+"/") {
			return l, true
		}
	}
	return library{}, false
}

// thirdParty says whether an import path is a module that is neither the
// standard library nor the repository's own.
func thirdParty(importPath, module string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return strings.Contains(first, ".") && importPath != module && !strings.HasPrefix(importPath, module+"/")
}

// Libraries is the part of How it is proved item 3 that concerns the two
// named exceptions of the toolchain, and the row of test-distribution that a
// module of them is linked only where it is declared. A third-party module
// that a source file of the repository imports passes only where the
// declaration of the package the file is in lists the module under libraries,
// the import record that entry names is the dependency record of that module,
// and the record names each start of a process the module holds and the
// condition that keeps Hive from reaching it; the record's local changes or
// audit must speak of the process, which is as much as a program can read of a
// statement that a reviewer weighs. A module another package declares, or one
// only a record covers, is not declared where it is linked. And a macOS
// executable that holds go-keyring fails: no package that builds for macOS
// imports it.
func Libraries(root string) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	records, err := importRecords(root)
	if err != nil {
		return nil, err
	}
	decls, err := declarations(root)
	if err != nil {
		return nil, err
	}
	var found []string
	dirs := map[string]bool{}
	err = walkFiles(root, func(rel string) error {
		if !sourceFile(rel) {
			return nil
		}
		dirs[path.Dir(rel)] = true
		src, err := readFile(root, rel)
		if err != nil {
			return err
		}
		imports, err := sourceImports(rel, src)
		if err != nil {
			return err
		}
		own := owner(path.Dir(rel))
		for _, p := range imports {
			if !thirdParty(p, module) {
				continue
			}
			lib, ok := decls[own].listed(p)
			if !ok {
				if decls[own] == nil {
					found = append(found, fmt.Sprintf("%s imports %s, and is in no package whose declaration lists it under libraries", rel, p))
				} else {
					found = append(found, fmt.Sprintf("%s imports %s, which the declaration of %s does not list under libraries", rel, p, own))
				}
				continue
			}
			rec, ok := records[lib.ImportID]
			switch {
			case !ok || !rec.pins(lib.Module):
				found = append(found, fmt.Sprintf("%s imports %s, which has no dependency record: the declaration of %s names the import record %q, which is not the dependency record of %s", rel, p, own, lib.ImportID, lib.Module))
			case !strings.Contains(strings.ToLower(rec.LocalChanges+" "+rec.Audit.Statement), "process"):
				found = append(found, fmt.Sprintf("%s imports %s, and its record %s does not name the starts of a process it holds and the condition that keeps Hive from reaching them", rel, p, rec.ImportID))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var list []string
	for d := range dirs {
		list = append(list, d)
	}
	sort.Strings(list)
	for _, d := range list {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "darwin", "amd64", false
		pkg, err := ctx.ImportDir(filepath.Join(root, filepath.FromSlash(d)), 0)
		if err != nil {
			if _, none := err.(*build.NoGoError); none {
				continue
			}
			return nil, fmt.Errorf("%s: %w", d, err)
		}
		for _, p := range pkg.Imports {
			if p == keyring || strings.HasPrefix(p, keyring+"/") {
				found = append(found, fmt.Sprintf("%s builds for macOS and imports %s, which no macOS executable holds", d, p))
			}
		}
	}
	return found, nil
}

// sourceImports lists the import paths of a Go source file.
func sourceImports(name string, src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var out []string
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		out = append(out, p)
	}
	return out, nil
}
