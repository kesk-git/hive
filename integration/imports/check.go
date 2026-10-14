package imports

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
)

// Check runs the six checks of spec/imports.md, What check-imports proves, on
// the repository tree fsys is rooted at, and returns every problem it finds:
//
//  1. Every file under imports/ other than README.md is a valid record whose
//     name matches its import_id.
//  2. Every path a record names exists, and every digest it gives matches.
//  3. Every file under any upstream/ directory is named by a record.
//  4. Every module in go.mod beyond the standard library has a dependency
//     record, and every package declaration's libraries and imports lists
//     name existing records.
//  5. If any record exists, the root LICENSE exists.
//  6. NOTICE equals the notices the records produce.
func Check(fsys fs.FS) []Problem {
	records, problems := LoadRecords(fsys)
	problems = append(problems, checkPaths(fsys, records)...)
	problems = append(problems, checkUpstream(fsys, records)...)
	problems = append(problems, checkModules(fsys, records)...)
	problems = append(problems, checkLicence(fsys)...)
	problems = append(problems, checkNotice(fsys, records)...)
	sort.SliceStable(problems, func(i, j int) bool { return problems[i].Rule < problems[j].Rule })
	return problems
}

func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}

// checkPaths is the second check: every path a record names exists, and every
// digest and size it gives matches the file.
func checkPaths(fsys fs.FS, records []Record) []Problem {
	var problems []Problem
	for _, r := range records {
		where := path.Join(Dir, r.ImportID+".json")
		bad := func(format string, args ...any) {
			problems = append(problems, Problem{2, where, fmt.Sprintf(format, args...)})
		}
		for _, f := range r.Licence.Files {
			data, err := fs.ReadFile(fsys, f.Path)
			switch {
			case err != nil:
				bad("names the licence file %s, which cannot be read: %v", f.Path, err)
			case canon.FileDigest(data) != f.Digest:
				bad("gives the licence file %s the digest %s; it is %s", f.Path, f.Digest, canon.FileDigest(data))
			case int64(len(data)) != f.Bytes:
				bad("gives the licence file %s a size of %d bytes; it is %d", f.Path, f.Bytes, len(data))
			}
		}
		for _, p := range r.Portions {
			if !exists(fsys, p.HivePath) {
				bad("names the portion %s, which does not exist", p.HivePath)
			}
		}
		for _, p := range r.RetainedTests {
			if !exists(fsys, p.HivePath) {
				bad("names the retained test %s, which does not exist", p.HivePath)
			}
		}
	}
	return problems
}

// walk calls fn for every file of the tree outside the three directories no
// check reads, each named by its path from the root of the tree: Git's own,
// the build output, and this checker's own fixture trees.
func walk(fsys fs.FS, fn func(name string)) error {
	return fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch name {
			case ".git", "dist", "integration/imports/testdata":
				return fs.SkipDir
			}
			return nil
		}
		fn(name)
		return nil
	})
}

// checkUpstream is the third check: every file under any upstream/ directory
// is named by a record, as one of its licence or notice texts, as a portion
// or as a retained test.
func checkUpstream(fsys fs.FS, records []Record) []Problem {
	named := map[string]bool{}
	for _, r := range records {
		for _, f := range r.Licence.Files {
			named[f.Path] = true
		}
		for _, p := range r.Portions {
			named[p.HivePath] = true
		}
		for _, p := range r.RetainedTests {
			named[p.HivePath] = true
		}
	}
	var problems []Problem
	err := walk(fsys, func(name string) {
		if slices.Contains(strings.Split(path.Dir(name), "/"), "upstream") && !named[name] {
			problems = append(problems, Problem{3, name, "is under an upstream/ directory and no record names it"})
		}
	})
	if err != nil {
		problems = append(problems, Problem{3, ".", "the tree cannot be walked: " + err.Error()})
	}
	return problems
}

type module struct{ path, version string }

// requires reads the modules go.mod requires: the lines of a require
// directive, alone or in a block, without a trailing comment.
func requires(gomod []byte) []module {
	var out []module
	inBlock := false
	for _, line := range strings.Split(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case inBlock && fields[0] == ")":
			inBlock = false
		case inBlock && len(fields) >= 2:
			out = append(out, module{fields[0], fields[1]})
		case fields[0] == "require" && len(fields) == 2 && fields[1] == "(":
			inBlock = true
		case fields[0] == "require" && len(fields) >= 3:
			out = append(out, module{fields[1], fields[2]})
		}
	}
	return out
}

// declared are the lists of a package declaration the fourth check reads.
type declared struct {
	Libraries []struct {
		Module   string `json:"module"`
		ImportID string `json:"import_id"`
	} `json:"libraries"`
	Imports []string `json:"imports"`
}

// checkModules is the fourth check: every module go.mod requires has a
// dependency record that pins it, and every library and import a package
// declaration lists names an existing record.
func checkModules(fsys fs.FS, records []Record) []Problem {
	var problems []Problem
	if gomod, err := fs.ReadFile(fsys, "go.mod"); err != nil {
		problems = append(problems, Problem{4, "go.mod", "cannot be read: " + err.Error()})
	} else {
		for _, m := range requires(gomod) {
			found := false
			for _, r := range records {
				if r.Kind != "dependency" || r.Upstream.Module == nil || r.Upstream.Module.Path != m.path {
					continue
				}
				found = true
				if r.Upstream.Module.Version != m.version {
					problems = append(problems, Problem{4, "go.mod", fmt.Sprintf("pins %s at %s; its record %s pins %s", m.path, m.version, r.ImportID, r.Upstream.Module.Version)})
				}
			}
			if !found {
				problems = append(problems, Problem{4, "go.mod", fmt.Sprintf("requires %s %s, which has no dependency record", m.path, m.version)})
			}
		}
	}
	files, _ := fs.Glob(fsys, "packages/*/hive-package.json")
	files = append(files, "contract/hive-package.json")
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			continue
		}
		var d declared
		if err := json.Unmarshal(data, &d); err != nil {
			problems = append(problems, Problem{4, f, "cannot be read: " + err.Error()})
			continue
		}
		var ids []string
		for _, l := range d.Libraries {
			ids = append(ids, l.ImportID)
		}
		ids = append(ids, d.Imports...)
		for _, id := range ids {
			if !exists(fsys, path.Join(Dir, id+".json")) {
				problems = append(problems, Problem{4, f, fmt.Sprintf("names the import record %q, and %s/%s.json does not exist", id, Dir, id)})
			}
		}
	}
	return problems
}

// checkLicence is the fifth check: with any record, the root LICENSE exists.
func checkLicence(fsys fs.FS) []Problem {
	entries, _ := fs.ReadDir(fsys, Dir)
	for _, e := range entries {
		if e.Name() != "README.md" && !exists(fsys, "LICENSE") {
			return []Problem{{5, "LICENSE", "does not exist, and " + path.Join(Dir, e.Name()) + " is a record"}}
		}
	}
	return nil
}

// checkNotice is the sixth check: NOTICE is, byte for byte, what the records
// produce.
func checkNotice(fsys fs.FS, records []Record) []Problem {
	have, err := fs.ReadFile(fsys, NoticeFile)
	if err != nil {
		return []Problem{{6, NoticeFile, "cannot be read: " + err.Error()}}
	}
	if want := Notice(records); !bytes.Equal(have, want) {
		return []Problem{{6, NoticeFile, "differs from the notices the records produce; regenerate it with `go run ./integration/imports/cmd/notice`"}}
	}
	return nil
}
