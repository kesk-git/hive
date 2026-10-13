// Package artefact assembles the artefact of spec/distribution.md, The
// artefact, from the seven executables `make build` left in dist/. It builds
// nothing. It writes dist/hive-<version>-<os>-<arch>/ with bin/ (the seven
// executables and the twenty-one links of spec/commands.md), LICENSE, NOTICE
// and manifest.json, and replaces the directory when it is run again.
//
// Nothing is stamped into an executable. The identity of the artefact is read
// from the executables: the implementation version each reports, the source
// revision each reports in its capabilities, and the Go release, platform and
// version-control state the toolchain embedded in each. An artefact is
// refused, and nothing is written, where those disagree or where an identity
// is unknown or marks a modified tree.
package artefact

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/integration/imports"
)

// Executable is what one built executable says of itself.
type Executable struct {
	// Name is the executable's name without suffix: hive-pool.
	Name string
	// Version is the implementation version `--version` prints.
	Version string
	// CapabilitiesVersion is the implementation version its capabilities
	// declare, which must agree with Version.
	CapabilitiesVersion string
	// ContractVersion is the contract version its capabilities report.
	ContractVersion string
	// SourceRevision is the source revision its capabilities report; empty
	// where the build information held none.
	SourceRevision string
	// BuildRevision is the vcs.revision of its embedded build information.
	BuildRevision string
	// Modified is the vcs.modified setting of its embedded build information:
	// the tree it was built from held changes the revision does not.
	Modified bool
	// GoRelease, OS and Arch are the toolchain release and the platform its
	// embedded build information names.
	GoRelease string
	OS        string
	Arch      string
}

// Inspector reads what a built executable says of itself.
type Inspector interface {
	Inspect(name, path string) (Executable, error)
}

// Options are the inputs of an assembly.
type Options struct {
	// Root is the repository root: its declarations, LICENSE and NOTICE.
	Root string
	// Dist is the directory `make build` filled and the artefact goes in.
	Dist string
	// GOOS and GOARCH are the platform of the artefact: the host's, unless a
	// fixture says otherwise.
	GOOS, GOARCH string
	Inspect      Inspector
}

// Manifest is the identity of an assembled artefact.
type Manifest struct {
	HiveVersion     string
	ContractVersion string
	SourceRevision  string
	GoRelease       string
	OS, Arch        string
}

func (o *Options) defaults() {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
}

// Assemble writes the artefact and returns its directory.
func Assemble(o Options) (string, error) {
	o.defaults()
	sfx := ""
	if o.GOOS == "windows" {
		sfx = ".exe"
	}
	set, problems := declare.Read(os.DirFS(o.Root))
	if len(problems) > 0 {
		return "", fmt.Errorf("the package declarations are not sound: %v", problems)
	}
	var execs []string
	links := map[string]string{}
	for _, p := range set.Packages {
		execs = append(execs, p.Executable)
		for _, c := range p.Commands {
			if c.Name != p.Executable {
				links[c.Name] = p.Executable
			}
		}
	}
	sort.Strings(execs)
	if len(execs) != 7 || len(links) != 21 {
		return "", fmt.Errorf("the declarations name %d executables and %d links; the artefact has seven and twenty-one", len(execs), len(links))
	}

	// What is to be assembled: the NOTICE must be the one the records produce.
	license, err := os.ReadFile(filepath.Join(o.Root, "LICENSE"))
	if err != nil {
		return "", fmt.Errorf("the root LICENSE cannot be read: %w", err)
	}
	notice, err := os.ReadFile(filepath.Join(o.Root, imports.NoticeFile))
	if err != nil {
		return "", fmt.Errorf("the root NOTICE cannot be read: %w", err)
	}
	records, recordProblems := imports.LoadRecords(os.DirFS(o.Root))
	if len(recordProblems) > 0 {
		return "", fmt.Errorf("the import records are not sound: %v", recordProblems)
	}
	if !bytes.Equal(notice, imports.Notice(records)) {
		return "", errors.New("the committed NOTICE differs from the notices the import records produce; regenerate it with `go run ./integration/imports/cmd/notice`")
	}

	// The seven executables, each as it is in dist/, and what each says of itself.
	var infos []Executable
	sources := map[string]string{}
	for _, e := range execs {
		src := filepath.Join(o.Dist, e+sfx)
		if _, err := os.Stat(src); err != nil {
			if alt := filepath.Join(o.Dist, e); sfx != "" && fileExists(alt) {
				src = alt
			} else {
				return "", fmt.Errorf("%s is not in %s: run `make build` first (%v)", e+sfx, o.Dist, err)
			}
		}
		sources[e] = src
		info, err := o.Inspect.Inspect(e, src)
		if err != nil {
			return "", fmt.Errorf("%s: %w", e, err)
		}
		infos = append(infos, info)
	}
	m, err := identify(infos, o.GOOS, o.GOARCH)
	if err != nil {
		return "", err
	}

	name := fmt.Sprintf("hive-%s-%s-%s", m.HiveVersion, o.GOOS, o.GOARCH)
	final := filepath.Join(o.Dist, name)
	stage := final + ".assembling"
	if err := os.RemoveAll(stage); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(stage, "bin"), 0o755); err != nil {
		return "", err
	}
	if err := fill(stage, sources, sfx, links, license, notice, o.GOOS); err != nil {
		os.RemoveAll(stage)
		return "", err
	}
	if err := writeManifest(stage, m, sfx, links); err != nil {
		os.RemoveAll(stage)
		return "", err
	}
	if err := os.RemoveAll(final); err != nil {
		return "", err
	}
	if err := os.Rename(stage, final); err != nil {
		return "", err
	}
	return final, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// identify reads the identity of the artefact from the seven executables, or
// says everything that stops it from having one.
func identify(infos []Executable, goos, goarch string) (Manifest, error) {
	var bad []string
	addf := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }
	first := infos[0]
	m := Manifest{
		HiveVersion:     first.Version,
		ContractVersion: first.ContractVersion,
		SourceRevision:  first.SourceRevision,
		GoRelease:       first.GoRelease,
		OS:              goos,
		Arch:            goarch,
	}
	if m.HiveVersion == "" {
		addf("%s reports no version", first.Name)
	}
	for _, e := range infos {
		if e.Version != first.Version {
			addf("%s reports the version %q, %s reports %q: the seven executables of one artefact are built from one revision", e.Name, e.Version, first.Name, first.Version)
		}
		if e.CapabilitiesVersion != e.Version {
			addf("%s reports the version %q with --version and %q in its capabilities", e.Name, e.Version, e.CapabilitiesVersion)
		}
		if e.ContractVersion != first.ContractVersion {
			addf("%s reports the contract version %q, %s reports %q", e.Name, e.ContractVersion, first.Name, first.ContractVersion)
		}
		switch {
		case e.SourceRevision == "":
			addf("%s reports no source revision: an unknown identity does not qualify, and a build outside a Git checkout, as from a source archive, carries none", e.Name)
		case e.SourceRevision != first.SourceRevision:
			addf("%s reports the source revision %s, %s reports %s", e.Name, e.SourceRevision, first.Name, first.SourceRevision)
		}
		if e.BuildRevision != e.SourceRevision {
			addf("%s reports the source revision %q and its build information names %q", e.Name, e.SourceRevision, e.BuildRevision)
		}
		if e.Modified {
			addf("%s was built from a tree with changes the revision %s does not hold (vcs.modified is true): a release is built from a committed tree, and a deliberate development build is a separate, explicit status", e.Name, e.SourceRevision)
		}
		if e.GoRelease != first.GoRelease {
			addf("%s was built with %s, %s with %s", e.Name, e.GoRelease, first.Name, first.GoRelease)
		}
		if e.OS != goos || e.Arch != goarch {
			addf("%s was built for %s-%s, not for this artefact's %s-%s", e.Name, e.OS, e.Arch, goos, goarch)
		}
	}
	if len(bad) > 0 {
		return Manifest{}, errors.New("no artefact is assembled:\n  " + strings.Join(bad, "\n  "))
	}
	return m, nil
}

// hardLink makes a hard link; a test replaces it with one that fails, to show
// the copy a platform without hard links gets.
var hardLink = os.Link

// fill writes bin/, LICENSE and NOTICE.
func fill(stage string, sources map[string]string, sfx string, links map[string]string, license, notice []byte, goos string) error {
	bin := filepath.Join(stage, "bin")
	for e, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(bin, e+sfx), data, 0o755); err != nil {
			return err
		}
	}
	for l, e := range links {
		dst := filepath.Join(bin, l+sfx)
		if goos == "windows" {
			// A hard link, where one can be made, and otherwise a whole copy.
			if err := hardLink(filepath.Join(bin, e+sfx), dst); err != nil {
				data, rerr := os.ReadFile(filepath.Join(bin, e+sfx))
				if rerr != nil {
					return rerr
				}
				if werr := os.WriteFile(dst, data, 0o755); werr != nil {
					return werr
				}
			}
			continue
		}
		if err := os.Symlink(e, dst); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "LICENSE"), license, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, imports.NoticeFile), notice, 0o644)
}

// writeManifest writes manifest.json in the repository's canonical form: the
// identity, every regular file except manifest.json with its size and digest,
// and the twenty-one links with their targets, each list sorted by path.
func writeManifest(stage string, m Manifest, sfx string, links map[string]string) error {
	var files []any
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(stage, p)
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"path": rel, "size": int64(len(data)), "digest": canon.FileDigest(data)})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].(map[string]any)["path"].(string) < files[j].(map[string]any)["path"].(string)
	})
	var list []any
	for l, e := range links {
		list = append(list, map[string]any{"path": path.Join("bin", l+sfx), "target": e + sfx})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].(map[string]any)["path"].(string) < list[j].(map[string]any)["path"].(string)
	})
	body, err := canon.Encode(map[string]any{
		"hive_version":     m.HiveVersion,
		"contract_version": m.ContractVersion,
		"source_revision":  m.SourceRevision,
		"go_release":       m.GoRelease,
		"os":               m.OS,
		"arch":             m.Arch,
		"files":            files,
		"links":            list,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, "manifest.json"), append(body, '\n'), 0o644)
}
