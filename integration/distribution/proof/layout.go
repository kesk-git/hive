package proof

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
)

// Want is what the artefact of one platform must hold.
type Want struct {
	// Root is the repository root: the LICENSE and NOTICE the artefact
	// carries are the ones committed there.
	Root string
	// GOOS is the platform whose rules apply. A fixture of any platform's
	// artefact is checked by its own rules on any host.
	GOOS string
	// Executables are the seven executables, by base name without suffix.
	Executables []string
	// Links maps each of the twenty-one link names to its executable.
	Links map[string]string
}

func (w Want) suffix() string {
	if w.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// manifestMembers are the members of manifest.json, no more and no fewer.
var manifestMembers = []string{"hive_version", "contract_version", "source_revision", "go_release", "os", "arch", "files", "links"}

// Layout checks the contents of an artefact directory against
// spec/distribution.md, The artefact, and How it is proved item 1: the
// directory holds bin/, LICENSE, NOTICE and manifest.json and nothing else;
// bin/ holds the seven executables and the twenty-one links under the names
// of the platform; each link is, on macOS and Linux, a relative symbolic link
// to its executable in the same directory that resolves, and on Windows a
// hard link to it or a byte-identical copy; every name on Windows ends in
// .exe; and manifest.json agrees with the files and links.
func Layout(dir string, w Want) []string {
	var c collector
	c.top(dir, w)
	c.bin(filepath.Join(dir, "bin"), w)
	c.manifest(dir, w)
	return c.problems
}

type collector struct{ problems []string }

func (c *collector) addf(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// names lists a directory's entries by name.
func names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// sameNames reports the difference between the names a directory holds and
// the names it must hold.
func (c *collector) sameNames(where string, got, want []string) {
	for _, n := range want {
		if !slices.Contains(got, n) {
			c.addf("%s lacks %s", where, n)
		}
	}
	for _, n := range got {
		if !slices.Contains(want, n) {
			c.addf("%s holds %s, which the artefact does not", where, n)
		}
	}
}

func (c *collector) top(dir string, w Want) {
	got, err := names(dir)
	if err != nil {
		c.addf("the artefact directory cannot be read: %v", err)
		return
	}
	c.sameNames("the artefact", got, []string{"bin", "LICENSE", "NOTICE", "manifest.json"})
	for _, n := range []string{"LICENSE", "NOTICE", "manifest.json"} {
		c.regular(dir, n)
	}
	for _, n := range []string{"LICENSE", "NOTICE"} {
		if !slices.Contains(got, n) || !isRegular(filepath.Join(dir, n)) {
			continue
		}
		have, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			c.addf("%s of the artefact cannot be read: %v", n, err)
			continue
		}
		want, err := os.ReadFile(filepath.Join(w.Root, n))
		if err != nil {
			c.addf("the repository's %s cannot be read: %v", n, err)
			continue
		}
		if !bytes.Equal(have, want) {
			c.addf("%s of the artefact differs from the committed %s", n, n)
		}
	}
	if fi, err := os.Lstat(filepath.Join(dir, "bin")); err == nil && !fi.IsDir() {
		c.addf("bin is not a directory")
	}
}

// isRegular says whether a path is a regular file itself, not a link to one.
func isRegular(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// regular reports a member of the artefact that is there but is not a regular
// file inside it: a directory, or a link, which may lead out of the artefact.
// A member that is not there is reported by the comparison of names.
func (c *collector) regular(dir, name string) {
	fi, err := os.Lstat(filepath.Join(dir, name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		c.addf("%s of the artefact cannot be inspected: %v", name, err)
	case !fi.Mode().IsRegular():
		c.addf("%s of the artefact is not a regular file (%v)", name, fi.Mode().Type())
	}
}

func (c *collector) bin(bin string, w Want) {
	got, err := names(bin)
	if err != nil {
		c.addf("bin/ cannot be read: %v", err)
		return
	}
	sfx := w.suffix()
	var want []string
	for _, e := range w.Executables {
		want = append(want, e+sfx)
	}
	for l := range w.Links {
		want = append(want, l+sfx)
	}
	c.sameNames("bin/", got, want)
	if w.GOOS == "windows" {
		for _, n := range got {
			if !strings.HasSuffix(strings.ToLower(n), ".exe") {
				c.addf("bin/%s lacks the .exe suffix every name on Windows carries", n)
			}
		}
	}
	for _, e := range w.Executables {
		c.executable(filepath.Join(bin, e+sfx), w)
	}
	for l, e := range w.Links {
		c.link(bin, l+sfx, e+sfx, w)
	}
}

func (c *collector) executable(path string, w Want) {
	fi, err := os.Lstat(path)
	if err != nil {
		return // reported as missing
	}
	name := filepath.Base(path)
	if !fi.Mode().IsRegular() {
		c.addf("bin/%s is not a regular file (%v)", name, fi.Mode().Type())
		return
	}
	if fi.Size() == 0 {
		c.addf("bin/%s is empty", name)
	}
	if w.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		c.addf("bin/%s has no execute bit", name)
	}
}

func (c *collector) link(bin, name, exe string, w Want) {
	path := filepath.Join(bin, name)
	fi, err := os.Lstat(path)
	if err != nil {
		return // reported as missing
	}
	target := filepath.Join(bin, exe)
	if w.GOOS == "windows" {
		// A hard link to the executable, or a byte-identical copy of it.
		if !fi.Mode().IsRegular() {
			c.addf("bin/%s is not a hard link or a copy of %s: it is %v", name, exe, fi.Mode().Type())
			return
		}
		efi, err := os.Stat(target)
		if err != nil {
			return
		}
		if os.SameFile(fi, efi) {
			return
		}
		a, errA := os.ReadFile(path)
		b, errB := os.ReadFile(target)
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			c.addf("bin/%s is neither a hard link to %s nor a byte-identical copy of it", name, exe)
		}
		return
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		c.addf("bin/%s is not a symbolic link (%v)", name, fi.Mode().Type())
		return
	}
	dest, err := os.Readlink(path)
	if err != nil {
		c.addf("bin/%s: its link cannot be read: %v", name, err)
		return
	}
	switch {
	case filepath.IsAbs(dest):
		c.addf("bin/%s is an absolute link to %s", name, dest)
	case dest != exe:
		c.addf("bin/%s links to %q, not to %s in the same directory", name, dest, exe)
	}
	lfi, err := os.Stat(path)
	if err != nil {
		c.addf("bin/%s is a dangling link: %v", name, err)
		return
	}
	if efi, err := os.Stat(target); err == nil && !os.SameFile(lfi, efi) {
		c.addf("bin/%s does not resolve to %s", name, exe)
	}
}

type fileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type linkEntry struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}

func (c *collector) manifest(dir string, w Want) {
	if !isRegular(filepath.Join(dir, "manifest.json")) {
		return // reported as missing or as not a regular file
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		c.addf("manifest.json cannot be read: %v", err)
		return
	}
	if err := canon.CheckFile(data); err != nil {
		c.addf("manifest.json: %v", err)
	}
	if !canon.IsCanonical(bytes.TrimSuffix(data, []byte("\n"))) {
		c.addf("manifest.json is not in the repository's canonical form")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		c.addf("manifest.json does not parse: %v", err)
		return
	}
	var got []string
	for k := range members {
		got = append(got, k)
	}
	sort.Strings(got)
	want := slices.Clone(manifestMembers)
	sort.Strings(want)
	c.sameNames("manifest.json", got, want)

	var m struct {
		HiveVersion string      `json:"hive_version"`
		OS          string      `json:"os"`
		Arch        string      `json:"arch"`
		Files       []fileEntry `json:"files"`
		Links       []linkEntry `json:"links"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		c.addf("manifest.json has members of the wrong type: %v", err)
		return
	}
	if name, want := filepath.Base(dir), fmt.Sprintf("hive-%s-%s-%s", m.HiveVersion, m.OS, m.Arch); name != want {
		c.addf("the directory is named %s but manifest.json says %s", name, want)
	}
	if m.OS != w.GOOS {
		c.addf("manifest.json names the platform %s, not %s", m.OS, w.GOOS)
	}
	c.manifestFiles(dir, m.Files)
	c.manifestLinks(w, m.Links)
}

// manifestFiles compares the files of the manifest with the regular files of
// the artefact, except manifest.json itself.
func (c *collector) manifestFiles(dir string, listed []fileEntry) {
	have := map[string]fileEntry{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		have[rel] = fileEntry{Path: rel, Size: int64(len(data)), Digest: canon.FileDigest(data)}
		return nil
	})
	if err != nil {
		c.addf("the files of the artefact cannot be inspected: %v", err)
	}
	seen := map[string]bool{}
	for i, f := range listed {
		if seen[f.Path] {
			c.addf("manifest.json lists %s twice", f.Path)
		}
		seen[f.Path] = true
		if i > 0 && listed[i-1].Path >= f.Path {
			c.addf("manifest.json lists %s out of order", f.Path)
		}
		actual, ok := have[f.Path]
		switch {
		case !ok:
			c.addf("manifest.json lists %s, which the artefact does not hold as a regular file", f.Path)
		case actual.Size != f.Size:
			c.addf("manifest.json gives %s a size of %d; it is %d", f.Path, f.Size, actual.Size)
		case actual.Digest != f.Digest:
			c.addf("manifest.json gives %s the digest %s; it is %s", f.Path, f.Digest, actual.Digest)
		}
	}
	for p := range have {
		if !seen[p] {
			c.addf("manifest.json does not list %s", p)
		}
	}
}

func (c *collector) manifestLinks(w Want, listed []linkEntry) {
	sfx := w.suffix()
	want := map[string]string{}
	for l, e := range w.Links {
		want["bin/"+l+sfx] = e + sfx
	}
	seen := map[string]bool{}
	for i, l := range listed {
		if seen[l.Path] {
			c.addf("manifest.json lists the link %s twice", l.Path)
		}
		seen[l.Path] = true
		if i > 0 && listed[i-1].Path >= l.Path {
			c.addf("manifest.json lists the link %s out of order", l.Path)
		}
		target, ok := want[l.Path]
		switch {
		case !ok:
			c.addf("manifest.json lists the link %s, which is not one of the twenty-one", l.Path)
		case target != l.Target:
			c.addf("manifest.json gives the link %s the target %s, not %s", l.Path, l.Target, target)
		}
	}
	for p := range want {
		if !seen[p] {
			c.addf("manifest.json does not list the link %s", p)
		}
	}
}
