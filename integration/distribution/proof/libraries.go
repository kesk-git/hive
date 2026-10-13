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

// dependencyRecords reads the records of kind dependency under imports/, by
// the module path they pin.
func dependencyRecords(root string) (map[string]record, error) {
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
		if r.Kind == "dependency" && r.Upstream.Module != nil {
			out[r.Upstream.Module.Path] = r
		}
	}
	return out, nil
}

// thirdParty says whether an import path is a module that is neither the
// standard library nor the repository's own.
func thirdParty(importPath, module string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return strings.Contains(first, ".") && importPath != module && !strings.HasPrefix(importPath, module+"/")
}

// Libraries is the part of How it is proved item 3 that concerns the two
// named exceptions of the toolchain. A third-party module that a source file
// of the repository imports passes only where its import record names each
// start of a process it holds and the condition that keeps Hive from reaching
// it; the record's local changes or audit must speak of the process, which is
// as much as a program can read of a statement that a reviewer weighs. And a
// macOS executable that holds go-keyring fails: no package that builds for
// macOS imports it.
func Libraries(root string) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	records, err := dependencyRecords(root)
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
		for _, p := range imports {
			if !thirdParty(p, module) {
				continue
			}
			var rec *record
			for mod, r := range records {
				if p == mod || strings.HasPrefix(p, mod+"/") {
					r := r
					rec = &r
				}
			}
			switch {
			case rec == nil:
				found = append(found, fmt.Sprintf("%s imports %s, which has no dependency record", rel, p))
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
