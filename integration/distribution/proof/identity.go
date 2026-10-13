package proof

import (
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// constString reads the value of a package-level string constant of a Go
// source file, or "" where the file declares none by that name.
func constString(file *ast.File, name string) string {
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v := s.(*ast.ValueSpec)
			for i, n := range v.Names {
				if n.Name != name || i >= len(v.Values) {
					continue
				}
				if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						return s
					}
				}
			}
		}
	}
	return ""
}

// SourceVersions are the implementation versions the source of the seven
// packages declares, by package, read from the Version constant of each
// package's entry package without running anything.
func SourceVersions(root string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(root, "packages", "*", "internal", "entry", "*.go"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range files {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if v := constString(file, "Version"); v != "" {
			pkg := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(f))))
			out[pkg] = v
		}
	}
	return out, nil
}

// SourceContractVersion is the contract version the library's envelope
// package declares that built code meets, read from its source.
func SourceContractVersion(root string) (string, error) {
	f := filepath.Join(root, "contract", "envelope", "codec.go")
	file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
	if err != nil {
		return "", err
	}
	if v := constString(file, "ContractVersion"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s declares no ContractVersion", f)
}

// Stated is the identity manifest.json states.
type Stated struct {
	HiveVersion     string `json:"hive_version"`
	ContractVersion string `json:"contract_version"`
	SourceRevision  string `json:"source_revision"`
	GoRelease       string `json:"go_release"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
}

// CheckSource compares the identity stated with the source the executables
// are built from: the version each package declares and the contract version
// the library declares that built code meets.
func CheckSource(st Stated, versions map[string]string, contract string, packages int) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if st.SourceRevision == "" {
		addf("manifest.json states no source revision: an unknown identity does not qualify")
	}
	var seen []string
	for pkg, v := range versions {
		seen = append(seen, pkg+"="+v)
		if v != st.HiveVersion {
			addf("manifest.json states the version %s; the source of %s declares %s", st.HiveVersion, pkg, v)
		}
	}
	sort.Strings(seen)
	if len(versions) != packages {
		addf("the source declares a version for %d packages (%v), not %d", len(versions), seen, packages)
	}
	if contract != st.ContractVersion {
		addf("manifest.json states the contract version %s; the library declares that built code meets %s", st.ContractVersion, contract)
	}
	return problems
}

// CheckBuilt compares the identity stated with the build information of one
// executable: the Go release, the platform and the revision, and that the
// tree it was built from held no change.
func CheckBuilt(name string, info *buildinfo.BuildInfo, st Stated) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	s := settings(info)
	if info.GoVersion != st.GoRelease {
		addf("%s was built with %s; manifest.json states %s", name, info.GoVersion, st.GoRelease)
	}
	if s["GOOS"] != st.OS || s["GOARCH"] != st.Arch {
		addf("%s was built for %s-%s; manifest.json states %s-%s", name, s["GOOS"], s["GOARCH"], st.OS, st.Arch)
	}
	if s["vcs.revision"] != st.SourceRevision {
		addf("%s carries the source revision %q; manifest.json states %q", name, s["vcs.revision"], st.SourceRevision)
	}
	if s["vcs.modified"] == "true" {
		addf("%s was built from a modified tree (vcs.modified is true), which is not a release", name)
	}
	return problems
}

// Identity checks the identity manifest.json states against what the
// executables themselves carry, without running any of them: the Go release,
// the platform and the source revision from the build information the
// toolchain embedded in each, and the versions from the source they were
// built from. An executable whose build information marks a modified tree, or
// holds no revision, is not a release and fails
// (spec/distribution.md, Packaged build identity).
func Identity(dir string, w Want) []string {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return []string{fmt.Sprintf("manifest.json cannot be read: %v", err)}
	}
	var st Stated
	if err := json.Unmarshal(data, &st); err != nil {
		return []string{fmt.Sprintf("manifest.json does not parse: %v", err)}
	}
	versions, err := SourceVersions(w.Root)
	if err != nil {
		return []string{err.Error()}
	}
	contract, err := SourceContractVersion(w.Root)
	if err != nil {
		return []string{err.Error()}
	}
	problems := CheckSource(st, versions, contract, len(w.Executables))
	for _, e := range w.Executables {
		info, err := buildinfo.ReadFile(filepath.Join(dir, "bin", e+w.suffix()))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: its build information cannot be read: %v", e, err))
			continue
		}
		problems = append(problems, CheckBuilt(e, info, st)...)
	}
	return problems
}
